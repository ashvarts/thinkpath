# Runbook: try the thinkpath CLI in Pi

This runbook has a local [Pi](https://github.com/earendil-works/pi) agent keep a thinkpath board up to date through the `thinkpath` CLI, run from Pi's bash tool, with no MCP involved. It checks that Pi follows the agent instructions from `thinkpath help`, and then removes everything again. For the MCP version, see [pi-runbook.md](pi-runbook.md).

Everything happens in a throwaway folder, `/tmp/clitest`: a test build of thinkpath and a separate server on port 7980 with its own data file. Pi needs no config changes. It finds the test build and the test server through the environment of the terminal you start it from. Your real board (port 7878), its data (`~/.thinkpath/data.json`) and your Pi config are never touched.

Time needed: about 10 minutes.

## Before you start

You need:

- Go, to build thinkpath.
- Pi.
- A clone of this repository. The commands below assume it's at `~/github.com/ashvarts/thinkpath`; change the path in step 1 if yours is elsewhere.

Create the folder and run every command below from inside it, in the same terminal, because the variables you set are how Pi finds the test setup:

```sh
mkdir /tmp/clitest && cd /tmp/clitest
```

## 1. Build a test copy of thinkpath

```sh
export TP_WORK="$PWD"
go -C ~/github.com/ashvarts/thinkpath build -o "$TP_WORK/thinkpath" .
```

**Check:** `ls` shows a `thinkpath` file.

## 2. Point this terminal at the test setup

```sh
export PATH="$TP_WORK:$PATH"
export THINKPATH_ADDR=127.0.0.1:7980
export THINKPATH_PROJECT=/try-pi-cli
```

These put the test build first on your `PATH`, send every `thinkpath` command to the throwaway server, and file every step under a fixed test project called `/try-pi-cli`. Pi's bash tool inherits these settings.

**Check:** `which thinkpath` prints `/tmp/clitest/thinkpath`.

## 3. Start a throwaway server

```sh
THINKPATH_DATA="$TP_WORK/data.json" thinkpath serve > "$TP_WORK/serve.log" 2>&1 &
```

**Check:** open http://127.0.0.1:7980. The board loads and is empty.

If the board doesn't load, run `cat serve.log`. The usual cause is that port 7980 is already in use. In that case, pick another port and use it everywhere this runbook says 7980, including `THINKPATH_ADDR`.

## 4. Try it in Pi

Start Pi from this terminal:

```sh
pi
```

Send these prompts one at a time, in this order. The goal is met on purpose by the last prompt: once it's met, Pi closes the session and any later steps have nowhere to go. After each one, refresh http://127.0.0.1:7980 and compare the board with the expected result.

| Prompt | Expected on the board |
| --- | --- |
| ``We're going to use thinkpath: run `thinkpath help` and follow the agent instructions. The goal is to write a haiku about Go in haiku.md. Don't write it yet.`` | A new session for `/try-pi-cli` with the haiku goal, plus a first step. |
| `Before we start, tell me the history of the haiku form in detail.` | One `sideways` step. |
| `Now tell me about the history of Go, too.` | Another `sideways` step, possibly with a ghost line (for example rabbit-hole). |
| `Show me the thinkpath status.` | Pi runs `thinkpath status` and shows the goal and every step so far. A `stalled` step may appear. |
| `Go ahead and write the haiku.` | One `toward` step, and the session shows as reached: Pi runs `thinkpath done` by itself once the goal is met. |

What counts as a pass:

- Pi runs `thinkpath step ...` by itself at the end of each reply, without being reminded.
- Each command shows up on the board.
- No `thinkpath` command fails. "can't reach the server" means step 3's server isn't running or `THINKPATH_ADDR` is wrong. If steps run but don't show up on the board, check that `thinkpath status` in Pi shows `project: /try-pi-cli`.

Exit Pi with Ctrl-D (or `/quit`).

## 5. Clean up

Stop the throwaway server, delete the folder and close the terminal. The test build, data file, server log and `haiku.md` all live in the folder, and closing the terminal drops the `PATH` and `THINKPATH_*` settings:

```sh
lsof -ti tcp:7980 -sTCP:LISTEN | xargs kill
cd .. && rm -rf /tmp/clitest
exit
```

**Check**, in a new terminal:

- http://127.0.0.1:7980 no longer loads.
- `ls /tmp/clitest` says the folder doesn't exist.
- `echo $THINKPATH_ADDR` prints an empty line.
