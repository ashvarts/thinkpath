# thinkpath

A board that shows whether a work session is heading toward its goal, standing still, or drifting sideways into a known trap like yak shaving or a rabbit hole.

One binary does both jobs: `thinkpath serve` runs a local server with a JSON API and the board page, and the other commands are a CLI that agents (or you) use to record goals and steps.

## Run it

```sh
go install .            # or: go build -o thinkpath .
thinkpath serve         # board at http://127.0.0.1:7878
```

```sh
thinkpath goal "ship the login page"
thinkpath step -dir toward -dist 3 "wrote the form handler"
thinkpath step -dir sideways -dist 2 -ghost tool-tinkering "reworked my shell prompt"
thinkpath step -dir stalled "re-read the OAuth docs"
thinkpath done              # or: thinkpath done -abandon
thinkpath status
thinkpath traps
```

## Using it with an agent

`thinkpath help` prints the rules an agent needs to follow, so a one-line prompt is enough to start a tracked session:

> We're going to use thinkpath: run `thinkpath help` and follow the agent instructions. The goal is ...

## API

| Method | Path | Body |
|---|---|---|
| GET | `/api/sessions` | |
| GET | `/api/sessions/current` | |
| POST | `/api/sessions` | `{"goal": "..."}` |
| POST | `/api/sessions/current/steps` | `{"direction": "toward", "distance": 3, "summary": "...", "ghost": "yak-shaving"}` |
| POST | `/api/sessions/current/finish` | `{"reached": true}` |
| GET | `/api/traps` | |

Data lives in `~/.thinkpath/data.json` (override with `THINKPATH_DATA`); the address can be changed with `THINKPATH_ADDR`.
