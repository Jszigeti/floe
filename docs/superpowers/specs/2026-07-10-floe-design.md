# floe — design spec (v0.1)

*2026-07-10 — validated with Jonas*

## Pitch

**Turn the iCloud storage you already pay for into a real versioned backup.**

floe is a macOS CLI that gives you Time Machine-style backups (daily snapshots, full history, deduplication, encryption) into your iCloud Drive — or any synced folder — using restic under the hood. Zero restic knowledge required.

## Why it doesn't exist yet

- restic/kopia/borg are engines, not turnkey macOS experiences.
- autorestic/resticprofile are generic config wrappers with zero iCloud awareness.
- Every GitHub project named "icloud-backup" goes the *other* direction (backing up iCloud to local).
- Arq does this properly but is commercial.
- Naively dropping a restic repo in iCloud Drive breaks: macOS evicts repo files ("Optimize Mac Storage" → dataless files), quota exhaustion is silent, concurrent sync corrupts locks. Handling this is floe's moat.

## Decisions (locked)

| Topic | Decision |
|---|---|
| Positioning | Generalist (any Mac user with a terminal), dev needs covered via config + hooks |
| Language | Go (cobra), single binary |
| Engine | restic, official static binary downloaded at `floe init` (pinned version, checksum verified) into `~/.local/share/floe/bin/` — no external dependency |
| Default scope | Hybrid: whole `~` with smart default excludes; wizard shows size estimate and offers to exclude the largest detected directories before first backup |
| Destinations | Any folder; iCloud Drive is the default and the marketing focus. iCloud-specific checks activate only when the destination is inside iCloud Drive |
| Multi-Mac | One repo per machine: `<dest>/Floe.backup/<hostname>/`. No shared repo (iCloud sync would corrupt restic locks) |
| Quota full | Back up anyway + alert with remediation steps; fail cleanly if truly full. Never auto-delete history |
| Frequency | Daily, configurable hour (default 13:00), launchd catches up after wake |
| Retention | keep-daily 7 / keep-weekly 4 / keep-monthly 6, configurable |
| Notifications | Failures + quota alerts immediately; weekly success digest ("7 backups OK, repo 8.2 GB"). Never per-backup |
| Passphrase | Generated at init, stored in macOS Keychain (`security`), displayed once with instruction to save it in a password manager. `floe passphrase` re-displays it |
| Hooks | `pre` (and `post`) shell commands in config. Docker volume export ships as a documented example, not a feature |
| v0.1 scope | Full cycle: init, backup, status, snapshots, restore, doctor, scheduling, notifications |
| Repo | `Jszigeti/floe`, public from first commit, MIT |
| Distribution | curl one-liner (installer script in repo, served from GitHub) + GitHub Releases binaries + `go install`. homebrew-core submission once notability threshold (~75 stars) is reached. No .dmg (CLI, no app bundle; curl download avoids Gatekeeper quarantine entirely) |
| Telemetry | None |
| Docs | English. Polished README is the landing page at launch |

## Architecture

```
cmd/floe/            main + cobra commands (thin)
internal/config/     TOML load/validate/defaults (~/.config/floe/config.toml)
internal/restic/     restic binary management (download/pin/verify) + command execution
internal/icloud/     brctl quota/status, dataless detection, re-materialization, "Optimize Storage" detection
internal/launchd/    LaunchAgent plist generation, load/unload (com.floe.backup)
internal/keychain/   security add/find-generic-password wrappers
internal/notify/     osascript notifications
internal/backup/     orchestration: hooks → snapshot → retention → digest bookkeeping
```

Every integration (`restic`, `brctl`, `launchctl`, `security`, `osascript`) sits behind an interface so commands are unit-testable with a fake runner. All state beyond config lives in the restic repo or the Keychain; a small `~/.local/state/floe/` holds logs and the digest timestamp.

### Config file (`~/.config/floe/config.toml`)

```toml
destination = "~/Library/Mobile Documents/com~apple~CloudDocs"  # any folder
hour = 13

[retention]
daily = 7
weekly = 4
monthly = 6

[scope]
paths = ["~"]
exclude = []  # smart defaults written at init, user-editable

[hooks]
pre = []   # e.g. ["~/.config/floe/hooks/export-docker-volumes.sh"]
post = []
```

## Commands

- **`floe init`** — wizard: download+verify restic → pick destination (default iCloud) → size estimate of default scope, offer to exclude largest directories → generate passphrase → Keychain + one-time display → create repo at `<dest>/Floe.backup/<hostname>/` → write config → install LaunchAgent → run first backup with progress.
- **`floe backup`** — pre-hooks → quota pre-check (warn, don't block) → `restic backup` → `restic forget --prune` per retention → post-hooks → notify on failure only. If ≥7 days have passed since the last digest (timestamp in `~/.local/state/floe/`), send the weekly digest notification at the end of a successful run — no separate scheduler.
- **`floe status`** — last/next backup, snapshot count, repo size, destination sync state, iCloud quota remaining, LaunchAgent health.
- **`floe snapshots`** — history list (wraps `restic snapshots`, readable output).
- **`floe restore <snapshot-id|latest> [path] [--target dir]`** — full or partial restore.
- **`floe doctor`** — the iCloud-awareness showcase: Optimize Mac Storage enabled? → warn with fix; dataless files in repo? → `brctl download` to re-materialize; repo fully synced? (block prune otherwise); quota headroom; restic binary integrity; LaunchAgent loaded; Keychain entry present.
- **`floe passphrase`** — re-display from Keychain.

## iCloud-specific behavior (active only when destination is in iCloud Drive)

1. Warn at init and in doctor if "Optimize Mac Storage" is enabled.
2. Before restic operations that read the repo (prune, check, restore), detect dataless repo files (`SF_DATALESS` flag) and re-materialize via `brctl download`, with progress.
3. `brctl quota` before each backup; notify with remediation if repo size approaches remaining quota.
4. Never prune while the repo has un-synced local changes (avoids sync conflicts on rewritten packs).
5. Source-side: always exclude the destination itself and iCloud Drive from the backup scope (no self-backup, no duplicating already-synced data).

## Error handling

- Any failed step logs to `~/Library/Logs/floe.log` and exits non-zero; launchd-run failures trigger a macOS notification with the failing step.
- restic lock left by a crashed run: detect stale lock (same host, dead PID) and `restic unlock` automatically; never unlock locks we can't attribute.
- Missing Keychain entry / config → actionable error pointing to `floe init`.

## Testing

- Unit tests per package with a fake command runner (no real restic/brctl).
- Integration test (CI, macos runner): real restic against a temp-dir repo — init, backup, mutate files, second backup, restore, verify contents.
- `floe doctor` logic tested against fixture outputs of `brctl` and `stat`.

## Release engineering

goreleaser: darwin arm64+amd64 binaries on GitHub Releases; `install.sh` at repo root (arch detection, download latest release, install to `~/.local/bin`, PATH hint). CI: test + lint on PR, release on tag.

## Non-goals (v0.1)

- GUI / menu bar app.
- Hourly frequency (revisit after field feedback on iCloud sync behavior).
- Shared multi-Mac repo.
- Linux/Windows.
- Backing up from iCloud (opposite direction — plenty of tools exist).

## Risks

- **iCloud sync semantics are undocumented** — mitigations in doctor + conservative prune. Field feedback will refine.
- **restic CLI output parsing** — use `--json` everywhere it exists.
- **Apple could change brctl** — isolate in `internal/icloud`, degrade gracefully to warnings.
