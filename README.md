# thinkpath

## Description

thinkpath is a CLI and a local web board that plot each prompt in an agent session as a move toward your goal, a stall, or a drift sideways into a known trap like yak shaving or a rabbit hole.

It solves the problem of losing a session to curiosity and side quests by showing the drift as it happens, so the session ends with something others can see or use: a doc, a blog post, a PR, a feature, or a tool.

## Usage

Install:

```sh
go install github.com/ashvarts/thinkpath@latest
```

Run the server, then open http://127.0.0.1:7878:

```sh
thinkpath serve
```

### List running servers

```sh
thinkpath server list
```

Each `thinkpath serve` registers itself in `~/.thinkpath/servers` (override with `THINKPATH_SERVERS`) and removes the entry when it exits. The command prints the PID, address, start time and data file of every server still running, and drops entries left behind by servers that were killed.

Tell your agent:

> We're going to use thinkpath: run `thinkpath help` and follow the agent instructions. The goal is ...

### As an MCP server

`thinkpath mcp` runs a local MCP server over stdio that exposes `goal`, `step`, `done`, `status` and `traps` as tools; it still needs `thinkpath serve` running. To add it to Claude Code:

```sh
claude mcp add thinkpath -- thinkpath mcp
```

To add it to [Pi](https://github.com/earendil-works/pi):

```sh
pi mcp add thinkpath --description "thinkpath board: record the session goal and each step toward it" -- thinkpath mcp
pi mcp list   # should show: thinkpath: connected, 5 tools
```

Then tell Pi:

> We're going to use thinkpath: follow the thinkpath MCP server's instructions. The goal is ...

To try it in Pi step by step, and clean up afterwards, follow [docs/pi-runbook.md](docs/pi-runbook.md).
To try Pi with the CLI instead of MCP, follow [docs/pi-cli-runbook.md](docs/pi-cli-runbook.md).
