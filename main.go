// thinkpath plots a work session as a path on a board: each prompt moves a
// piece toward the goal, keeps it in place, or drifts it sideways. The same
// binary runs the local server that holds the data and acts as the CLI that
// agents (or you) use to record goals and steps.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const usage = `thinkpath: see whether you're heading toward your goal.

Usage:
  thinkpath serve                       start the server and board (http://127.0.0.1:7878)
  thinkpath server list                 list the running thinkpath servers
  thinkpath goal "<goal>"               start a new session with this goal
  thinkpath step -dir <direction> [-dist 1-5] [-ghost <trap>] "<summary>"
                                        record one step in the current session
  thinkpath done [-abandon]             end the current session (reached, or abandoned)
  thinkpath status                      show the current goal and its steps
  thinkpath traps                       list the trap names usable with -ghost
  thinkpath mcp                         run as an MCP server over stdin/stdout,
                                        exposing the commands above as tools
  thinkpath help                        show this, plus instructions for agents

Directions: toward, stalled, sideways

Sessions belong to a project: the git repository root of the current
directory, or the current directory itself outside a git repository.

Environment:
  THINKPATH_PROJECT  project to use instead of the one worked out from the
                     current directory
  THINKPATH_ADDR     server address (default 127.0.0.1:7878)
  THINKPATH_DATA     data file used by serve (default ~/.thinkpath/data.json)
`

// agentGuide is printed by `thinkpath help` so that the user can point any
// agent at the CLI with a one-line prompt and have it learn the rules itself.
const agentGuide = `
For agents
----------
The user tracks this session on a thinkpath board. Keep it up to date:

- When the user states a goal ("the goal is ...") or you agree on one together,
  run: thinkpath goal "<goal>"
- The goal SHOULD be a tangible artifact: something other people can see or
  use when the session ends, such as a doc, blog post, PR, feature or tool.
  "Understand X" or "explore Y" is not an artifact. If the user's goal isn't
  one, suggest an artifact-shaped version (for example "publish a one-page
  note explaining X") and confirm it with them before running thinkpath goal.
- At the end of EVERY reply, record what that prompt did:
    thinkpath step -dir <toward|stalled|sideways> -dist <1-5> [-ghost <trap>] "<one-line summary>"
  toward    moved the goal forward
  stalled   no movement either way
  sideways  work not needed for the goal
  -dist     how far it moved: 1 = small step, 5 = big leap (ignored for stalled)
  -ghost    when recent steps look like they're drifting into a known trap,
            name it (run "thinkpath traps" for the list)
- Judge honestly against the goal shown by "thinkpath status". A sideways step
  recorded as toward makes the board useless.
- When the goal is met, run: thinkpath done
`

func main() {
	log.SetFlags(0)

	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	cmd, args := os.Args[1], os.Args[2:]

	var err error
	switch cmd {
	case "serve":
		err = runServe()
	case "server":
		err = runServerCmd(args)
	case "goal":
		err = runGoal(args)
	case "step":
		err = runStep(args)
	case "done":
		err = runDone(args)
	case "status":
		err = runStatus()
	case "traps":
		fmt.Print(trapList())
	case "mcp":
		err = runMCP(os.Stdin, os.Stdout)
	case "help", "-h", "--help":
		fmt.Print(usage, agentGuide)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}

	if err != nil {
		log.Fatalf("thinkpath: %v", err)
	}
}

func addr() string {
	if a := os.Getenv("THINKPATH_ADDR"); a != "" {
		return a
	}
	return "127.0.0.1:7878"
}

// project works out which project the CLI is being run for. Agents run
// commands from somewhere inside the project they're working on, so the git
// root is the natural key: it's the same no matter which subdirectory the
// command happens to run from.
func project() (string, error) {
	if p := os.Getenv("THINKPATH_PROJECT"); p != "" {
		return p, nil
	}
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err == nil {
		return strings.TrimSpace(string(out)), nil
	}
	return os.Getwd()
}

func runServe() error {
	path := os.Getenv("THINKPATH_DATA")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		path = filepath.Join(home, ".thinkpath", "data.json")
	}

	store, err := OpenStore(path)
	if err != nil {
		return err
	}

	unregister, err := registerServer(ServerInfo{PID: os.Getpid(), Addr: addr(), Data: path, StartedAt: time.Now()})
	if err != nil {
		return err
	}
	defer unregister()

	// Remove our registry entry on Ctrl-C or kill, since ListenAndServe
	// never returns on its own.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigs
		unregister()
		os.Exit(0)
	}()

	log.Printf("thinkpath: board at http://%s (data in %s)", addr(), path)
	return http.ListenAndServe(addr(), newServer(store))
}

func runServerCmd(args []string) error {
	if len(args) == 1 && args[0] == "list" {
		return runServerList()
	}
	return errors.New("usage: thinkpath server list")
}

func runGoal(args []string) error {
	return printResult(startGoal(strings.Join(args, " ")))
}

func runStep(args []string) error {
	fs := flag.NewFlagSet("step", flag.ContinueOnError)
	dir := fs.String("dir", "", "toward, stalled or sideways")
	dist := fs.Int("dist", 1, "how far the step moved, 1 (small) to 5 (big leap)")
	ghost := fs.String("ghost", "", "trap the recent steps are drifting towards (see `thinkpath traps`)")

	// The flag package stops at the first non-flag argument. Agents won't
	// always put the summary last, so we keep parsing until every flag has
	// been consumed, collecting the leftover words as the summary.
	var words []string
	for {
		if err := fs.Parse(args); err != nil {
			return err
		}
		if fs.NArg() == 0 {
			break
		}
		words = append(words, fs.Arg(0))
		args = fs.Args()[1:]
	}

	return printResult(recordStep(Step{Direction: *dir, Distance: *dist, Ghost: *ghost, Summary: strings.Join(words, " ")}))
}

func runDone(args []string) error {
	fs := flag.NewFlagSet("done", flag.ContinueOnError)
	abandon := fs.Bool("abandon", false, "end the session without reaching the goal")
	if err := fs.Parse(args); err != nil {
		return err
	}

	return printResult(finish(!*abandon))
}

func runStatus() error {
	return printResult(status())
}

func printResult(out string, err error) error {
	if err != nil {
		return err
	}
	fmt.Print(out)
	return nil
}

// The functions below do the actual work behind each command and return the
// text to show for it. Both the CLI and the MCP server use them, so the two
// always behave (and read) the same.

func startGoal(goal string) (string, error) {
	var sess Session
	if err := call("POST", "/api/sessions", map[string]string{"goal": goal}, &sess); err != nil {
		return "", err
	}
	return fmt.Sprintf("session %d started: %s\n", sess.ID, sess.Goal), nil
}

func recordStep(step Step) (string, error) {
	var sess Session
	if err := call("POST", "/api/sessions/current/steps", step, &sess); err != nil {
		return "", err
	}
	return fmt.Sprintf("step %d recorded: %s\n", len(sess.Steps), step.Direction), nil
}

func finish(reached bool) (string, error) {
	var sess Session
	if err := call("POST", "/api/sessions/current/finish", map[string]bool{"reached": reached}, &sess); err != nil {
		return "", err
	}
	if sess.Reached {
		return fmt.Sprintf("session %d: goal reached in %d steps\n", sess.ID, len(sess.Steps)), nil
	}
	return fmt.Sprintf("session %d: abandoned after %d steps\n", sess.ID, len(sess.Steps)), nil
}

func status() (string, error) {
	var sess Session
	if err := call("GET", "/api/sessions/current", nil, &sess); err != nil {
		return "", err
	}
	var b strings.Builder

	state := "in progress"
	if sess.EndedAt != nil {
		state = "abandoned"
		if sess.Reached {
			state = "reached"
		}
	}
	fmt.Fprintf(&b, "session %d (%s)\nproject: %s\ngoal: %s\n", sess.ID, state, sess.Project, sess.Goal)

	for i, st := range sess.Steps {
		fmt.Fprintf(&b, "%3d  %-8s %d  %s", i+1, st.Direction, st.Distance, st.Summary)
		if st.Ghost != "" {
			fmt.Fprintf(&b, "  [ghost: %s]", st.Ghost)
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}

func trapList() string {
	var b strings.Builder
	for _, t := range traps {
		fmt.Fprintf(&b, "%-24s %s\n", t.Name, t.Description)
	}
	return b.String()
}

// call sends a JSON request about the current project to the running server
// and decodes the response into dst. API errors come back as
// {"error": "..."}, which we surface as-is.
func call(method, path string, body, dst any) error {
	proj, err := project()
	if err != nil {
		return err
	}
	path += "?project=" + url.QueryEscape(proj)

	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, "http://"+addr()+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("can't reach the server at %s (is `thinkpath serve` running?): %w", addr(), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var apiErr struct {
			Error string `json:"error"`
		}
		if json.NewDecoder(resp.Body).Decode(&apiErr) == nil && apiErr.Error != "" {
			return errors.New(apiErr.Error)
		}
		return fmt.Errorf("server returned %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}
