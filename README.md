# launch

A terminal process manager for developers running multiple projects and services locally.

<!-- screenshot -->

Run all your services with one command, watch their logs live, start and stop individually or all at once. Works with a single project or a whole workspace.

## Features

- **One config file per project** — drop a `launch.yml` in each service directory; run `launch` from the workspace root and it scans subdirectories automatically
- **Multi-project sidebar** — services grouped by project, collapse/expand with `enter`
- **Live log streaming** — per-process log pane, persists across restarts
- **Process persistence** — detach with `q`, reattach later; processes keep running
- **Dependency ordering** — `depends_on` starts services in the right order, across projects
- **Health checks** — `ready_check` holds dependents until a service is actually up
- **Task runner** — runs tasks from a `Taskfile.yml` alongside processes

## Install

```bash
git clone https://github.com/adamarutyunov/launch.git ~/launch
cd ~/launch && go build -o launch .
```

Add to your PATH:

```bash
# .zshrc / .bashrc
export PATH="$HOME/launch:$PATH"

# config.fish
fish_add_path ~/launch
```

## Usage

```bash
launch              # current directory
launch ~/projects   # discovers launch.yml in subdirectories
```

## Config

Place a `launch.yml` in your project directory:

```yaml
name: backend

processes:
  postgres:
    title: PostgreSQL
    command: docker run --rm -p 5432:5432 postgres:16
    auto_start: true
    ready_check:
      command: pg_isready -h 127.0.0.1
      interval: 2s
      retries: 15

  api:
    title: API Server
    command: go run ./cmd/api
    auto_start: true
    depends_on:
      - postgres
```

### Full schema

| Field | Default | Description |
|---|---|---|
| `name` | directory name | Project name shown in sidebar |
| `processes.<slug>.title` | — | Display name |
| `processes.<slug>.command` | — | Shell command (`sh -c`) |
| `processes.<slug>.auto_start` | `false` | Start on launch |
| `processes.<slug>.working_dir` | launch.yml dir | Working directory |
| `processes.<slug>.env` | — | Extra environment variables |
| `processes.<slug>.depends_on` | — | Slugs that must be running first |
| `processes.<slug>.ready_check.command` | — | Polled to detect readiness |
| `processes.<slug>.ready_check.interval` | `2s` | Time between retries |
| `processes.<slug>.ready_check.retries` | `30` | Max attempts |

### depends_on

```yaml
depends_on:
  - postgres          # same project
  - shared:redis      # cross-project (project name : slug)
```

When starting a process whose dependencies aren't running, launch prompts: start with deps, force start, or cancel.

### Taskfile integration

Drop a `Taskfile.yml` next to `launch.yml` and its tasks appear in the sidebar automatically.

## Keybindings

| Key | Action |
|---|---|
| `j` / `k` or `↑` / `↓` | Navigate |
| `enter` | Collapse/expand project |
| `s` / `space` | Start or stop selected |
| `r` | Restart selected |
| `A` | Start all |
| `S` | Stop all |
| `g` / `G` | Jump to top/bottom of logs |
| `ctrl+u` / `ctrl+d` | Page up/down in logs |
| `c` | Clear logs |
| `h` | Hide/show task in sidebar |
| `q` | Detach (processes keep running) |
| `Q` / `ctrl+c` | Kill all and exit |

## Contributing

Bug reports and pull requests welcome at [github.com/adamarutyunov/launch](https://github.com/adamarutyunov/launch/issues).

---

Made by [Adam](https://adam.ci) · [@_adamci](https://twitter.com/_adamci)
