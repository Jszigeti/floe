# floe

**Turn the iCloud storage you already pay for into a real versioned backup.**

macOS CLI in Go that gives Time Machine-style backups (daily snapshots, full history, dedup, encryption) into iCloud Drive — or any synced folder — using restic under the hood. The moat is iCloud awareness: dataless-file re-materialization, quota checks, sync-aware pruning (`floe doctor`).

## Project status

- ✅ Design spec validated: `docs/superpowers/specs/2026-07-10-floe-design.md`
- ✅ Implementation plan ready: `docs/superpowers/plans/2026-07-10-floe-v0.1.md` (20 TDD tasks)
- ⬜ Implementation not started — no Go code exists yet
- ⬜ GitHub repo not created yet (target: `Jszigeti/floe`, public, MIT)

## How to implement

Execute the plan task-by-task with `superpowers:subagent-driven-development` (fresh subagent per task, review between tasks). Tasks are ordered by dependency — do them in sequence. Each task is self-contained: files, complete test code, complete implementation, exact commands, commit message.

## Hard constraints

- Go ≥ 1.24, macOS only (darwin), CI on `macos-latest`.
- Dependencies: `spf13/cobra` + `BurntSushi/toml` ONLY. Everything else stdlib. Tests: stdlib `testing` only.
- Never guess versions or checksums — the plan gives the exact commands to fetch real values (restic pinned version in Task 5).
- All user-facing strings and docs in English.
- Home resolution via `os.UserHomeDir()` only; tests isolate with `t.Setenv("HOME", t.TempDir())`.
- All external commands (`restic`, `brctl`, `launchctl`, `security`, `osascript`) go through `internal/run.Runner` — never `exec.Command` directly outside that package.

## Commands

```bash
go test ./...                              # unit tests (offline)
go test -tags integration ./integration/  # real restic cycle (downloads binary)
go vet ./...
sh -n install.sh                           # once Task 20 lands
```

## Key design decisions (full rationale in the spec)

- One restic repo per machine: `<dest>/Floe.backup/<hostname>` — never shared (iCloud sync corrupts restic locks).
- Passphrase in macOS Keychain (service `floe`, account `restic`), shown once at init.
- Never prune while the iCloud repo has pending uploads; never auto-delete history on low quota (warn instead).
- Docker volume export is a documented example hook (`docs/examples/`), not a feature.
- Distribution: `install.sh` (curl one-liner) + GitHub Releases + `go install`. No Homebrew tap; homebrew-core once ~75 stars.
