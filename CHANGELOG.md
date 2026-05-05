# Changelog

## [Unreleased]

### Fixed

- `nview --version` no longer prints a redundant `version v...`; the output is now `nview vX.Y.Z`.

### Changed

- Adopt an Unreleased-first changelog and release-PR versioning workflow so multiple PRs can be bundled into one release ([#117]).
- Re-point the `notes` library dependency from `github.com/dreikanter/notesctl` to `github.com/dreikanter/notes` to follow the upstream rename. No behavior change ([#116]).
- Replace fsnotify-based watcher loop with notesctl's `Store.Watch`, switch the index to incremental `Apply` updates, and gate SSE delivery on a per-view scope (`scope=note&id=X` for single-note views, `scope=list` for index pages). Adds `POST /api/index/refresh` plus a topbar refresh button and visibilitychange-driven reconcile so missed events are recovered on tab focus. ([#110])

[#117]: https://github.com/dreikanter/nview/pull/117
[#116]: https://github.com/dreikanter/nview/pull/116
[#110]: https://github.com/dreikanter/nview/issues/110

## [0.2.0] - 2026-04-26

### Changed

- Replace the mounted tree sidebar with metadata-driven browsing: recent notes, tag/type filters, Tags/Types/Dates index pages, compact note metadata headers, and UID-based editor launch. ([#113])
- Refactor sidebar into a reusable client-side `TreeView` component. Tree state (expanded, selected, focus) lives in the browser; the server exposes `/api/tree/list` for children and a unified `/events` SSE stream that emits both file-change and directory-mutation events. ([#88])
- Trim long autolinks in rendered notes with a trailing ellipsis at the last path-segment boundary; the full URL stays in `href` and is exposed via a `title` tooltip, and a CSS rule wraps anything not trimmed. ([#93])
- Rename the project, module path, command, executable, settings, and UI branding from notesview/notes-view to nview. ([#104])

[#88]: https://github.com/dreikanter/nview/issues/88
[#93]: https://github.com/dreikanter/nview/issues/93
[#104]: https://github.com/dreikanter/nview/pull/104
[#113]: https://github.com/dreikanter/nview/pull/113

[0.2.0]: https://github.com/dreikanter/nview/releases/tag/v0.2.0

## [0.1.0] - 2026-04-12

### Added

- Build-time version injection via `-ldflags` ([#51])
- `--version` flag for the CLI ([#51])
- CHANGELOG.md to track changes between releases ([#51])
- GitHub Action to auto-tag on PR merge ([#51])

[0.1.0]: https://github.com/dreikanter/nview/releases/tag/v0.1.0
[#51]: https://github.com/dreikanter/nview/pull/51
