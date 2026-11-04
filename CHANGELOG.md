# Changelog

All notable changes to floe are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and floe adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Fixed

- Scheduled backups failed every day once macOS had moved some of the repository's files to iCloud to free disk space, with `resource deadlock avoided`, while a manual `floe backup` worked. macOS starts scheduled jobs with downloads of those files turned off. floe now turns them on for itself, and restic inherits it.
- The system manifest of scheduled runs missed the Homebrew `Brewfile` and the App Store app list: the schedule's PATH did not include Homebrew. Re-run `floe init` to update an existing schedule.
- A failed backup alert now says to run `floe doctor`, and `floe doctor` reports a failed last scheduled backup instead of calling it complete.

### Changed

- Building from source needs the Xcode Command Line Tools.

## [0.1.3] - 2026-07-17

### Fixed

- A correctly configured machine reported every backup as incomplete because three paths under `~/Library` are unreadable whatever permission is granted: a FileProvider staging area, a speech-model cache, and a Control Center secure preferences directory. The fix is scoped to exactly what is unreadable, so everything readable around them stays in the backup, runs report complete, and the Full Disk Access warning goes back to meaning something (#8).

## [0.1.2] - 2026-07-17

### Fixed

- Scheduled backups could silently lose Full Disk Access after an upgrade. macOS binds the permission to the exact binary, so replacing it clears every grant, and the daily run would quietly drop Mail, Messages, Safari, Contacts and Calendars with nothing to warn you. `floe doctor` now reports what the last scheduled run actually skipped, and `install.sh` says so after an upgrade. Signing releases to keep the permission across upgrades is tracked in #7 (#6).
- `floe doctor`'s Full Disk Access check reported the permissions of the shell it was run from, which said nothing about the scheduled run. It is now labelled `full disk access (this shell)`, and the new `last scheduled backup` check carries the verdict that matters.
- Only scheduled runs own the incomplete report, so running `floe backup` by hand no longer clears the evidence that the schedule is broken. A complete scheduled run clears it, a failed run keeps it.
- `floe doctor` warns when the installed schedule predates this version and needs `floe init` to report incomplete runs.

## [0.1.1] - 2026-07-13

### Fixed

- Incomplete backups (missing Full Disk Access) now log a one-line summary (unreadable-file count, a few example paths and a pointer to `~/.local/state/floe/last_incomplete.log`) instead of dumping restic's full output (#1, #2).
- The Full Disk Access hint names the app to grant (Terminal, iTerm or your editor, detected via `TERM_PROGRAM`) alongside the floe binary (#3).
- `floe init` ends with a reminder that the passphrase lives in the macOS Keychain and can be shown again with `floe passphrase` (#4).

## [0.1.0] - 2026-07-12

### Added

- `floe init`: guided setup with size estimate, largest-directory exclusions, generated passphrase stored in the macOS Keychain (shown once), daily LaunchAgent schedule and a first backup with live restic progress.
- Daily encrypted backups into iCloud Drive or any synced folder, powered by a pinned, checksum-verified restic 0.19.1: AES-256 encryption, deduplication, zstd compression, retention 7 daily / 4 weekly / 6 monthly.
- Commands: `backup`, `status`, `snapshots`, `restore`, `doctor`, `passphrase`, `uninstall`.
- iCloud awareness: dataless repo files detected and re-downloaded, quota checked before every backup, prune deferred while an iCloud sync is pending (applied at the start of the next run).
- System reinstall manifest captured on every backup: Homebrew `Brewfile`, App Store apps, `/Applications` listing, crontab.
- Smart default excludes covering regenerable content only (caches, package stores, Chromium caches, Docker's VM disk). App data is never excluded by default.
- macOS TCC handling: backups missing protected files (no Full Disk Access) complete as "incomplete" with a weekly-throttled notice instead of failing, and `doctor` checks Full Disk Access.
- Notifications: failures and low quota immediately, weekly health digest, never a per-backup ping.
- Distribution: `install.sh` one-liner, GitHub Releases (darwin arm64/amd64) and `go install`.

[0.1.3]: https://github.com/Jszigeti/floe/releases/tag/v0.1.3
[0.1.2]: https://github.com/Jszigeti/floe/releases/tag/v0.1.2
[0.1.1]: https://github.com/Jszigeti/floe/releases/tag/v0.1.1
[0.1.0]: https://github.com/Jszigeti/floe/releases/tag/v0.1.0
