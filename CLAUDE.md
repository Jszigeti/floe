# floe

**Turn the iCloud storage you already pay for into a real versioned backup.**

macOS CLI in Go that gives Time Machine-style backups (daily snapshots, full history, dedup, encryption) into iCloud Drive — or any synced folder — using restic under the hood. The moat is iCloud awareness: dataless-file re-materialization, quota checks, sync-aware pruning (`floe doctor`).

## Project status

- ✅ Design spec: `docs/superpowers/specs/2026-07-10-floe-design.md` (historical — some decisions evolved, see below)
- ✅ Implementation plan: `docs/superpowers/plans/2026-07-10-floe-v0.1.md` (historical)
- ✅ Fully implemented and audited: all commands (`init`, `backup`, `status`, `snapshots`, `restore`, `doctor`, `passphrase`, `uninstall`), 96%+ unit coverage, integration + e2e suites against real restic, golangci-lint clean
- ⬜ GitHub repo not created yet (target: `Jszigeti/floe`, public, MIT); tag `v0.1.0` triggers goreleaser

## Deviations from the original spec (deliberate, reviewed)

- Retention runs BEFORE the backup (spec said after) — otherwise the just-written packs keep iCloud sync pending and prune would defer forever.
- restic pinned at 0.19.1 (spec's 0.18.1 was an example); missing repo detected via restic's documented exit code 10.
- restic exit code 3 = "incomplete" (snapshot written, some files unreadable — TCC): weekly-throttled notice, not a failure.
- Schedule displayed in 12-hour clock (1:00 PM); quota labels in GiB.
- Built-in system manifest (Brewfile, mas list, /Applications, crontab) written to `~/.local/state/floe/manifest/` on every backup, best-effort.
- Product line: defaults stay audience-agnostic — excludes only cover regenerable content, app data is never excluded by default.

## Hard constraints

- Go ≥ 1.24, macOS only (darwin), CI on `macos-latest`.
- Dependencies: `spf13/cobra` + `BurntSushi/toml` ONLY. Everything else stdlib. Tests: stdlib `testing` only.
- Never guess versions or checksums — the plan gives the exact commands to fetch real values (restic pinned version in Task 5).
- All user-facing strings and docs in English.
- Home resolution via `os.UserHomeDir()` only; tests isolate with `t.Setenv("HOME", t.TempDir())`.
- All external commands (`restic`, `brctl`, `launchctl`, `security`, `osascript`) go through `internal/run.Runner` — never `exec.Command` directly outside that package.

## Commands

```bash
go test ./...                              # unit tests (offline), CI enforces ≥90% coverage
go test -tags integration ./integration/   # real restic cycle (network on first run)
go test -tags e2e ./e2e/ -timeout 15m      # full CLI surface against real restic
go vet ./... && gofmt -l .                 # must be clean
golangci-lint run ./...                    # v2 standard set, must be 0 issues
goreleaser release --snapshot --clean      # validate the release pipeline locally
```

## Key design decisions (full rationale in the spec)

- One restic repo per machine: `<dest>/Floe.backup/<hostname>` — never shared (iCloud sync corrupts restic locks).
- Passphrase in macOS Keychain (service `floe`, account `restic`), shown once at init.
- Never prune while the iCloud repo has pending uploads; never auto-delete history on low quota (warn instead).
- Docker volume export is a documented example hook (`docs/examples/`), not a feature.
- Distribution: `install.sh` (curl one-liner) + GitHub Releases + `go install`. No Homebrew tap; homebrew-core once ~75 stars.
