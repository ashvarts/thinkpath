package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// The MCP server speaks JSON-RPC 2.0 over stdin/stdout, one message per line,
// which is what the MCP stdio transport expects. It holds no data of its own:
// every tool call goes to the running `thinkpath serve` through the same
// functions the CLI uses, so the board updates exactly as it would from the
// command line.

const mcpProtocolVersion = "2025-06-18"

// mcpInstructions is sent to the client when it connects. Clients usually
// hand it to the model, so it plays the same part as agentGuide does for
// `thinkpath help`, just phrased in terms of tools rather than commands.
const mcpInstructions = `The user tracks this session on a thinkpath board. Keep it up to date:

- When the user states a goal ("the goal is ...") or you agree on one together,
  call the goal tool.
- The goal SHOULD be a tangible artifact: something other people can see or
  use when the session ends, such as a doc, blog post, PR, feature or tool.
  "Understand X" or "explore Y" is not an artifact. If the user's goal isn't
  one, suggest an artifact-shaped version (for example "publish a one-page
  note explaining X") and confirm it with them before calling the goal tool.
- At the end of EVERY reply, call the step tool to record what that prompt did:
  toward    moved the goal forward
  stalled   no movement either way
  sideways  work not needed for the goal
  distance  how far it moved: 1 = small step, 5 = big leap (ignored for stalled)
  ghost     when recent steps look like they're drifting into a known trap,
            name it (the traps tool lists them)
- Judge honestly against the goal shown by the status tool. A sideways step
  recorded as toward makes the board useless.
- When the goal is met, call the done tool.`

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`

	// run does the work for a tools/call. It gets the raw arguments so each
	// tool can decode them into whatever shape it needs.
	run func(args json.RawMessage) (string, error) `json:"-"`
}

func mcpTools() []mcpTool {
	trapNames := make([]string, len(traps))
	for i, t := range traps {
		trapNames[i] = t.Name
	}

	return []mcpTool{
		{
			Name:        "goal",
			Description: "Start a new thinkpath session with this goal. Any open session in the project is closed as not reached.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"goal": map[string]any{"type": "string", "description": "the goal, ideally a tangible artifact"},
				},
				"required": []string{"goal"},
			},
			run: func(args json.RawMessage) (string, error) {
				var in struct {
					Goal string `json:"goal"`
				}
				if err := json.Unmarshal(args, &in); err != nil {
					return "", err
				}
				return startGoal(in.Goal)
			},
		},
		{
			Name:        "step",
			Description: "Record one step in the current session: what the last prompt did with respect to the goal.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"direction": map[string]any{"type": "string", "enum": directions},
					"distance":  map[string]any{"type": "integer", "minimum": 1, "maximum": 5, "description": "1 = small step, 5 = big leap; ignored for stalled"},
					"ghost":     map[string]any{"type": "string", "enum": trapNames, "description": "trap the recent steps are drifting towards"},
					"summary":   map[string]any{"type": "string", "description": "one-line summary of the step"},
				},
				"required": []string{"direction", "summary"},
			},
			run: func(args json.RawMessage) (string, error) {
				// Distance defaults to 1, the same as the CLI flag.
				step := Step{Distance: 1}
				if err := json.Unmarshal(args, &step); err != nil {
					return "", err
				}
				return recordStep(step)
			},
		},
		{
			Name:        "done",
			Description: "End the current session, either with the goal reached or abandoned.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"abandon": map[string]any{"type": "boolean", "description": "true to end the session without reaching the goal"},
				},
			},
			run: func(args json.RawMessage) (string, error) {
				var in struct {
					Abandon bool `json:"abandon"`
				}
				if err := json.Unmarshal(args, &in); err != nil {
					return "", err
				}
				return finish(!in.Abandon)
			},
		},
		{
			Name:        "status",
			Description: "Show the current goal and its steps.",
			InputSchema: map[string]any{"type": "object"},
			run:         func(json.RawMessage) (string, error) { return status() },
		},
		{
			Name:        "traps",
			Description: "List the trap names usable as a step's ghost.",
			InputSchema: map[string]any{"type": "object"},
			run:         func(json.RawMessage) (string, error) { return trapList(), nil },
		},
	}
}

type rpcRequest struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func runMCP(r io.Reader, w io.Writer) error {
	tools := mcpTools()
	enc := json.NewEncoder(w)

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}

		var req rpcRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			enc.Encode(rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{-32700, "parse error"}})
			continue
		}

		// Messages without an id are notifications (such as
		// notifications/initialized), which never get a reply.
		if len(req.ID) == 0 {
			continue
		}

		result, rerr := handleMCP(tools, req)
		resp := rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result, Error: rerr}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return sc.Err()
}

func handleMCP(tools []mcpTool, req rpcRequest) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]string{"name": "thinkpath", "version": "0.1.0"},
			"instructions":    mcpInstructions,
		}, nil

	case "ping":
		return map[string]any{}, nil

	case "tools/list":
		return map[string]any{"tools": tools}, nil

	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &rpcError{-32602, err.Error()}
		}
		if len(p.Arguments) == 0 {
			p.Arguments = json.RawMessage("{}")
		}
		for _, t := range tools {
			if t.Name == p.Name {
				return toolResult(t.run(p.Arguments)), nil
			}
		}
		return nil, &rpcError{-32602, fmt.Sprintf("unknown tool %q", p.Name)}

	default:
		return nil, &rpcError{-32601, fmt.Sprintf("method %q not found", req.Method)}
	}
}

// toolResult wraps a tool's output in the MCP result shape. Tool failures,
// such as the server not running or a bad direction, are reported as a
// result with isError set rather than as a protocol error, so the model gets
// to read the message and correct itself.
func toolResult(out string, err error) map[string]any {
	text, isError := out, false
	if err != nil {
		text, isError = err.Error(), true
	}
	return map[string]any{
		"content": []map[string]string{{"type": "text", "text": text}},
		"isError": isError,
	}
}
