#!/usr/bin/env bash
# try-mcp.sh walks a human through the thinkpath MCP server by hand.
#
# It builds thinkpath, starts a throwaway server with its own data file and
# port, sends a series of MCP messages to `thinkpath mcp` (showing each
# request and reply), and leaves the board open for you to inspect. When you
# press Enter (or hit Ctrl-C) it stops the server and deletes everything it
# created. Your real board and ~/.thinkpath data are never touched.
set -euo pipefail

cd "$(dirname "$0")/.."

ADDR="127.0.0.1:${THINKPATH_TEST_PORT:-7979}"
WORK="$(mktemp -d -t thinkpath-mcp)"
SERVER_PID=""

cleanup() {
	if [[ -n "$SERVER_PID" ]]; then
		kill "$SERVER_PID" 2>/dev/null || true
		wait "$SERVER_PID" 2>/dev/null || true
	fi
	rm -rf "$WORK"
	echo "cleaned up: server stopped, $WORK removed"
}
trap cleanup EXIT

export THINKPATH_ADDR="$ADDR"
export THINKPATH_DATA="$WORK/data.json"
export THINKPATH_PROJECT="/try-mcp"

echo "==> building thinkpath into $WORK"
go build -o "$WORK/thinkpath" .

echo "==> starting a throwaway server at http://$ADDR"
"$WORK/thinkpath" serve >"$WORK/serve.log" 2>&1 &
SERVER_PID=$!
for _ in {1..50}; do
	curl -fs "http://$ADDR/api/traps" >/dev/null && break
	sleep 0.1
done
curl -fs "http://$ADDR/api/traps" >/dev/null || { echo "server didn't start:"; cat "$WORK/serve.log"; exit 1; }

# Each message is sent to a fresh `thinkpath mcp`, after the handshake an MCP
# client would do, so you can see one request and its reply at a time.
id=1
send() {
	local method="$1" params="$2"
	id=$((id + 1))
	local msg="{\"jsonrpc\":\"2.0\",\"id\":$id,\"method\":\"$method\",\"params\":$params}"
	echo
	echo ">>> $msg"
	printf '%s\n%s\n%s\n' \
		'{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
		'{"jsonrpc":"2.0","method":"notifications/initialized"}' \
		"$msg" |
		"$WORK/thinkpath" mcp | tail -n 1 | sed 's/^/<<< /'
}

echo
echo "==> handshake (look for serverInfo and the agent instructions)"
printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' | "$WORK/thinkpath" mcp | sed 's/^/<<< /'

echo
echo "==> tool calls (expect isError:false, except the two marked as errors)"
send tools/list '{}'
send tools/call '{"name":"goal","arguments":{"goal":"Try out the thinkpath MCP server"}}'
send tools/call '{"name":"step","arguments":{"direction":"toward","distance":3,"summary":"set a goal over MCP"}}'
send tools/call '{"name":"step","arguments":{"direction":"sideways","distance":2,"ghost":"rabbit-hole","summary":"read the MCP spec"}}'
send tools/call '{"name":"step","arguments":{"direction":"stalled","summary":"waited for coffee"}}'
echo
echo "    (error expected: unknown direction)"
send tools/call '{"name":"step","arguments":{"direction":"backwards","summary":"x"}}'
send tools/call '{"name":"traps","arguments":{}}'
send tools/call '{"name":"status","arguments":{}}'

echo
echo "==> open http://$ADDR to see the session on the board:"
echo "    goal \"Try out the thinkpath MCP server\" with 3 steps (toward, sideways with a rabbit-hole ghost, stalled)"
echo
read -r -p "press Enter to finish the session and clean up... " _

send tools/call '{"name":"done","arguments":{}}'
echo
echo "    (error expected: no open session left)"
send tools/call '{"name":"step","arguments":{"direction":"toward","summary":"x"}}'
echo
