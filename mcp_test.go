package main

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestMCP(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(newServer(store))
	defer srv.Close()
	t.Setenv("THINKPATH_ADDR", strings.TrimPrefix(srv.URL, "http://"))
	t.Setenv("THINKPATH_PROJECT", "/code/app")

	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"goal","arguments":{"goal":"ship the MCP"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"step","arguments":{"direction":"toward","distance":2,"summary":"wrote mcp.go"}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"step","arguments":{"direction":"backwards","summary":"x"}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"done","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":7,"method":"nope"}`,
	}, "\n")

	var out bytes.Buffer
	if err := runMCP(strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}

	type response struct {
		ID     int `json:"id"`
		Result struct {
			Tools   []mcpTool `json:"tools"`
			IsError bool      `json:"isError"`
		} `json:"result"`
		Error *rpcError `json:"error"`
	}
	var resps []response
	dec := json.NewDecoder(&out)
	for dec.More() {
		var r response
		if err := dec.Decode(&r); err != nil {
			t.Fatal(err)
		}
		resps = append(resps, r)
	}

	// The notification gets no reply, so there's one response per request.
	if len(resps) != 7 {
		t.Fatalf("got %d responses, want 7", len(resps))
	}
	if n := len(resps[1].Result.Tools); n != 5 {
		t.Errorf("tools/list: got %d tools, want 5", n)
	}
	if resps[3].Result.IsError {
		t.Errorf("valid step reported as an error")
	}
	if !resps[4].Result.IsError {
		t.Errorf("bad direction not reported as an error")
	}
	if resps[6].Error == nil || resps[6].Error.Code != -32601 {
		t.Errorf("unknown method: got %+v, want error -32601", resps[6].Error)
	}

	sess, err := store.Current("/code/app")
	if err != nil {
		t.Fatal(err)
	}
	if sess.Goal != "ship the MCP" || !sess.Reached || len(sess.Steps) != 1 {
		t.Errorf("got %q reached=%v with %d steps", sess.Goal, sess.Reached, len(sess.Steps))
	}
}
