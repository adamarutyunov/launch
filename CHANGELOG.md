# Changelog

## [1.0.2] - 2026-03-23

### Fixed
- Taskfile-only projects (no `launch.yml`) now work when run from the project root; project name defaults to the directory name
- Group header counter `(x/y)` now counts only processes, not tasks; hidden entirely when the group has no processes
- `A` (start all) now starts only processes, not tasks
- Manual process start with unmet dependencies now prompts with three options: start with all dependencies (full tree), force start (skip dep check), or cancel — instead of silently failing

### Added
- `Manager.DependencyTree(proc)` — builds the full transitive dependency tree for a process (handles cycles via visited set)
- Expanded README config reference with full YAML schema, field table, and notes on circular dependency behavior — structured for easy use by automated tooling

## [1.0.1] - 2026-03-20

### Added
- Taskfile support: tasks are scanned from `Taskfile.yml` / `Taskfile.yaml` in project directories and shown alongside processes in the sidebar
- Tasks section header in the sidebar separates processes from tasks within a group
- Task status colors: cyan while running, green on success, red on failure (with exit code)
- Hide tasks with `h`; toggle visibility of hidden tasks with `H` (shown dimmed)
- Hidden task state persisted in user settings
- Task description shown as subtitle in the log pane when a `desc` field is set in the Taskfile
- Sidebar title now shows "Launch <version>"
- ANSI 16-color palette throughout — colors adapt to terminal light/dark themes
- Adaptive selection background (dark on dark themes, light on light themes)

### Changed
- Sidebar items are now backed by a `SidebarItem` interface, with `ManagedProcess` and `ManagedTask` as concrete types
- `StopAll`, `StopGroup`, `RunningInGroup`, and `Summary` now account for both processes and tasks

## [1.0.0] - 2026-03-19

### Added
- Initial release: terminal process manager with YAML-based configuration
- Multi-project support via subdirectory scanning
- Dependency-ordered process startup with ready checks
- Process detach and reattach across sessions
- Collapse/expand project groups in the sidebar
- Log streaming with auto-scroll and manual scroll
- Persistent session state and user settings
