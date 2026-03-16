# launch

Terminal process manager with tree-grouped sidebar, log streaming, health checks, and process persistence.

## Install

```bash
git clone https://github.com/adamarutyunov/launch.git ~/launch
cd ~/launch
go build -o launch .
ln -sf ~/launch/launch /opt/homebrew/bin/launch
```

After making changes, rebuild:

```bash
cd ~/launch && go build -o launch .
```

The symlink means `launch` is available globally — no re-linking needed after rebuilds.

## Usage

```bash
cd ~/projects && launch          # discovers launch.yml in subdirectories
cd ~/projects/backend && launch  # single project mode
launch ~/projects                # explicit path
```

## Config (launch.yml)

```yaml
name: backend
processes:
  database:
    title: Database
    command: docker compose up db
    auto_start: true
    ready_check:
      command: pg_isready -h 127.0.0.1 -p 5432
      interval: 2s
      retries: 30
  api:
    title: API Server
    command: go run ./cmd/api
    auto_start: true
    depends_on:
      - database                  # same-project dependency (just slug)
      - shared:redis              # cross-project dependency (group:slug)
  frontend:
    title: Frontend
    command: pnpm dev
    working_dir: web
    auto_start: true
    env:
      PORT: "3001"
```

### Fields

| Field | Required | Description |
|---|---|---|
| `title` | yes | Human-readable name shown in sidebar |
| `command` | yes | Shell command to run |
| `auto_start` | no | Start automatically on launch (default: false) |
| `depends_on` | no | Dependencies that must be running (ready) before start |
| `ready_check` | no | Health check to determine when the process is ready |
| `working_dir` | no | Working directory (default: launch.yml location) |
| `env` | no | Extra environment variables |

### depends_on

Dependencies reference other processes by slug:

- `database` — same project
- `shared:redis` — cross-project (`group:slug`)

If a dependency isn't running when you try to start a process, an alert is shown and the process won't start. Dependencies must be in `running` state (not just `starting`) — so if a dependency has a `ready_check`, it must pass before dependents can start.

### ready_check

Polls a shell command to determine when a process is actually ready to serve, not just that the OS process is alive.

```yaml
ready_check:
  command: pg_isready -h 127.0.0.1 -p 5432  # any shell command
  interval: 2s     # time between retries (default: 2s)
  retries: 30      # max attempts before giving up (default: 30)
```

While the check is polling, the process shows as `starting` (yellow ◐ in sidebar). Once the check passes, it transitions to `running` (green ●). If all retries are exhausted, the process stays in `starting` state and an alert is shown.

Without `ready_check`, a process is considered `running` immediately after the OS process starts.

## Keybindings

| Key | Action |
|---|---|
| `↑/↓` or `j/k` | Select process |
| `s` / `space` | Start/stop selected process or group |
| `A` | Start all processes |
| `S` | Stop all processes |
| `r` | Restart selected process or group |
| `c` | Clear logs |
| `g` / `G` | Jump to top/bottom of logs |
| `ctrl+u` / `ctrl+d` | Page up/down |
| `q` | Detach (processes keep running) |
| `Q` / `ctrl+c` | Kill all processes and exit |

## Process persistence

Processes survive TUI restarts:

- **`q` (detach)**: Exits the TUI but processes keep running. State is saved to `~/.launch/`. Run `launch` again to reattach and see live logs.
- **`Q` (kill)**: Stops all processes and cleans up state.

Logs are written to `~/.launch/logs/` so they persist across sessions.

## Multi-project discovery

When run from a parent directory, launch scans immediate subdirectories for `launch.yml` files and groups them in a tree:

```
workspace
▸ shared (2/2)
    ● Redis
    ● Database
▸ backend (3/3)
    ● API Server
    ◐ Worker
    ...
▸ frontend (1/2)
    ● Web App
    ○ Storybook
```
