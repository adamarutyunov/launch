# launch

Terminal process manager with tree-grouped sidebar, log streaming, health checks, and process persistence.

## Install

```bash
git clone https://github.com/adamarutyunov/launch.git ~/launch
cd ~/launch
go build -o launch .
```

Add `~/launch` to your `PATH`. For example, in `~/.zshrc` or `~/.config/fish/config.fish`:

```bash
# .zshrc / .bashrc
export PATH="$HOME/launch:$PATH"

# config.fish
fish_add_path ~/launch
```

After making changes, rebuild:

```bash
cd ~/launch && go build -o launch .
```

## Usage

```bash
cd ~/projects && launch          # discovers launch.yml in subdirectories
cd ~/projects/backend && launch  # single project mode
launch ~/projects                # explicit path
```

## Config (launch.yml)

Place a `launch.yml` file in your project directory (or any subdirectory when using multi-project mode).

### Full schema

```yaml
# launch.yml
name: my-project          # optional; defaults to the directory name

processes:
  <slug>:                 # identifier used for depends_on references (lowercase, underscores/hyphens ok)
    title: My Process     # required — human-readable name shown in the sidebar
    command: ./start.sh   # required — shell command (run via sh -c)
    auto_start: false     # optional — start automatically when launch opens (default: false)
    working_dir: ./subdir # optional — relative to launch.yml location (default: launch.yml directory)
    env:                  # optional — extra environment variables
      KEY: value
    depends_on:           # optional — other processes that must be running before this starts
      - other-slug        # same-project process (just the slug)
      - group:slug        # cross-project process (group name : slug)
    ready_check:          # optional — health check; process shows "starting" until this passes
      command: curl -sf http://localhost:3000/health
      interval: 2s        # time between retries (default: 2s)
      retries: 30         # max attempts before giving up (default: 30)
```

### Field reference

| Field | Required | Default | Description |
|---|---|---|---|
| `name` | no | directory name | Project name shown in sidebar |
| `processes.<slug>.title` | yes | — | Human-readable name shown in sidebar |
| `processes.<slug>.command` | yes | — | Shell command to run (via `sh -c`) |
| `processes.<slug>.auto_start` | no | `false` | Start automatically on launch |
| `processes.<slug>.working_dir` | no | launch.yml directory | Working directory (relative to launch.yml) |
| `processes.<slug>.env` | no | — | Extra environment variables (map) |
| `processes.<slug>.depends_on` | no | — | List of dependency slugs; must be running before this starts |
| `processes.<slug>.ready_check.command` | no | — | Shell command polled to detect readiness |
| `processes.<slug>.ready_check.interval` | no | `2s` | Time between ready check retries |
| `processes.<slug>.ready_check.retries` | no | `30` | Max ready check attempts before giving up |

### depends_on

Dependencies reference other processes by slug:

- `database` — same project (just the slug)
- `shared:redis` — cross-project (`group:slug`, where group is the other project's `name` or directory name)

When you try to start a process manually and its dependencies are not running, launch prompts you with three options: start with all dependencies, force start (skip deps), or cancel. Dependencies must be in `running` state (not just `starting`) — so if a dependency has a `ready_check`, it must pass before dependents can start.

Circular dependencies are detected at runtime: the affected processes are immediately stopped with an error message rather than hanging.

### ready_check

Polls a shell command to determine when a process is actually ready to serve, not just that the OS process has started.

```yaml
ready_check:
  command: pg_isready -h 127.0.0.1 -p 5432  # any shell command; exit 0 = ready
  interval: 2s     # time between retries (default: 2s)
  retries: 30      # max attempts before giving up (default: 30)
```

While polling, the process shows as `starting` (yellow ◐). Once the check passes, it transitions to `running` (green ●). Without `ready_check`, a process is considered `running` immediately after the OS process starts.

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
