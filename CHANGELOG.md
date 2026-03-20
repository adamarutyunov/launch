# Changelog

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
