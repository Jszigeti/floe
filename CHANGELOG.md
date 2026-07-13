# Changelog

All notable changes to floe are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and floe adheres to [Semantic Versioning](https://semver.org/).

## [0.1.1] - 2026-07-13

### Fixed

- Incomplete backups (missing Full Disk Access) now log a one-line summary — unreadable-file count, a few example paths and a pointer to `~/.local/state/floe/last_incomplete.log` — instead of dumping restic's full output (#1, #2).
- The Full Disk Access hint names the app to grant (Terminal, iTerm or your editor, detected via `TERM_PROGRAM`) alongside the floe binary (#3).
- `floe init` ends with a reminder that the passphrase lives in the macOS Keychain and can be shown again with `floe passphrase` (#4).

## [0.1.0] - 2026-07-12

### Added

- `floe init`: guided setup with size estimate, largest-directory exclusions, generated passphrase stored in the macOS Keychain (shown once), daily LaunchAgent schedule and a first backup with live restic progress.
- Daily encrypted backups into iCloud Drive or any synced folder, powered by a pinned, checksum-verified restic 0.19.1: AES-256 encryption, deduplication, zstd compression, retention 7 daily / 4 weekly / 6 monthly.
- Commands: `backup`, `status`, `snapshots`, `restore`, `doctor`, `passphrase`, `uninstall`.
- iCloud awareness: dataless repo files detected and re-downloaded, quota checked before every backup, prune deferred while an iCloud sync is pending (applied at the start of the next run).
- System reinstall manifest captured on every backup: Homebrew `Brewfile`, App Store apps, `/Applications` listing, crontab.
- Smart default excludes covering regenerable content only (caches, package stores, Chromium caches, Docker's VM disk); app data is never excluded by default.
- macOS TCC handling: backups missing protected files (no Full Disk Access) complete as "incomplete" with a weekly-throttled notice instead of failing; `doctor` checks Full Disk Access.
- Notifications: failures and low quota immediately, weekly health digest, never a per-backup ping.
- Distribution: `install.sh` one-liner, GitHub Releases (darwin arm64/amd64) and `go install`.

[0.1.1]: https://github.com/Jszigeti/floe/releases/tag/v0.1.1
[0.1.0]: https://github.com/Jszigeti/floe/releases/tag/v0.1.0
