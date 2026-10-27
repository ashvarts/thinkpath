# Runbook: try the thinkpath MCP server in Pi

This runbook adds thinkpath to a local [Pi](https://github.com/earendil-works/pi) agent as an MCP server, checks that Pi can use it, and then removes everything again.

Everything happens in a throwaway folder, `/tmp/mcptest2`: a test build of thinkpath, a separate server on port 7979 with its own data file, and a Pi MCP entry named `thinkpath-test` that applies to that folder only. Your real board (port 7878), its data (`~/.thinkpath/data.json`) and your global Pi config are never touched.

Time needed: about 10 minutes.

## Before you start

You need:

- Go, to build thinkpath.
- Pi with built-in MCP support. Check with `pi mcp --help`; if it prints usage for `add`, `list` and `remove`, you're set.
- A clone of this repository. The commands below assume it's at `~/github.com/ashvarts/thinkpath`; change the path in step 1 if yours is elsewhere.

Create the folder and run every command below from inside it, in the same terminal, so the variables you set stay available:

```sh
mkdir /tmp/mcptest2 && cd /tmp/mcptest2
```

## 1. Build a test copy of thinkpath

```sh
export TP_WORK="$PWD"
go -C ~/github.com/ashvarts/thinkpath build -o "$TP_WORK/thinkpath" .
```

**Check:** `ls` shows a `thinkpath` file.

## 2. Start a throwaway server

```sh
THINKPATH_ADDR=127.0.0.1:7979 THINKPATH_DATA="$TP_WORK/data.json" \
  "$TP_WORK/thinkpath" serve > "$TP_WORK/serve.log" 2>&1 &
echo $! > "$TP_WORK/serve.pid"
```

**Check:** open http://127.0.0.1:7979. The board loads and is empty.

If the board doesn't load, run `cat serve.log`. The usual cause is that port 7979 is already in use. In that case, pick another port and use it everywhere this runbook says 7979.

## 3. Add the server to Pi for this folder

```sh
pi mcp add -l thinkpath-test \
  --env THINKPATH_ADDR=127.0.0.1:7979 \
  --env THINKPATH_PROJECT=/try-pi \
  --description "thinkpath board: record the session goal and each step toward it" \
  -- "${TP_WORK:?run step 1 first}/thinkpath" mcp
```

This writes a `thinkpath-test` entry to `./.pi/mcp.json`. The two `--env` settings point the server at the throwaway server and file every step under a fixed test project called `/try-pi`.

If the command stops with `TP_WORK: run step 1 first`, you're in a different terminal from step 1. Run `export TP_WORK="$PWD"` from `/tmp/mcptest2` and try again.

**Check:** `cat .pi/mcp.json` shows a `"command"` ending in `/tmp/mcptest2/thinkpath`, not just `/thinkpath`.

Pi ignores `./.pi/mcp.json` until you trust the folder, so `pi mcp list` won't show `thinkpath-test` yet. You'll trust the folder in the next step.

## 4. Try it in Pi

Start Pi in this folder:

```sh
pi
```

When Pi asks whether to trust the folder, say yes.

**Check:** exit Pi with Ctrl-D, run `pi mcp list` and make sure the output includes:

```
thinkpath-test: connected, 5 tools ...
  tools: goal, step, done, status, traps
```

If it shows `spawn ... ENOENT`, the command path in `.pi/mcp.json` is wrong; redo step 3. If it shows some other failure, run `./thinkpath mcp < /dev/null`. It should exit right away without printing anything. Any output points to the problem.

Start `pi` again, then send these prompts one at a time, in this order. The goal gets met by the last prompt on purpose: once it's met, Pi closes the session and later steps have nowhere to go. After each one, refresh http://127.0.0.1:7979 and compare the board with the expected result.

| Prompt | Expected on the board |
| --- | --- |
| `We're going to use thinkpath: follow the thinkpath-test MCP server's instructions. The goal is to write a haiku about Go in haiku.md. Don't write it yet.` | A new session for `/try-pi` with the haiku goal, plus a first step. |
| `Before we start, tell me the history of the haiku form in detail.` | One `sideways` step. |
| `Now tell me about the history of Go, too.` | Another `sideways` step, possibly with a ghost line (for example rabbit-hole). |
| `Show me the thinkpath status.` | Pi lists the goal and every step so far. A `stalled` step may appear. |
| `Go ahead and write the haiku.` | One `toward` step, and the session shows as reached: Pi closes it by itself once the goal is met. |

What counts as a pass:

- Pi calls the thinkpath tools by itself after each reply, without being reminded.
- Each tool call shows up on the board.
- No tool call returns an error. Pi shows tool errors inline. "can't reach the server" means step 2's server isn't running or the port is wrong.

Exit Pi with Ctrl-D (or `/quit`).

## 5. Clean up

Stop the throwaway server and delete the folder. The test build, data file, server log, Pi MCP entry and `haiku.md` all live in it:

```sh
kill "$(cat "$TP_WORK/serve.pid")"
cd .. && rm -rf /tmp/mcptest2 && unset TP_WORK
```

**Check:**

- http://127.0.0.1:7979 no longer loads.
- `ls /tmp/mcptest2` says the folder doesn't exist.

Pi saves your choice to trust the folder outside it, so deleting the folder doesn't remove that record. It does no harm, since the folder no longer exists.

## If you got lost halfway

If you closed the terminal, `$TP_WORK` and the server's process ID are gone. Clean up by hand instead:

```sh
lsof -ti tcp:7979 -sTCP:LISTEN | xargs kill   # stops only the server on the test port
rm -rf /tmp/mcptest2
```
