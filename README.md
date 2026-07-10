# floe

**Turn the iCloud storage you already pay for into a real versioned backup.**

[![ci](https://github.com/Jszigeti/floe/actions/workflows/ci.yml/badge.svg)](https://github.com/Jszigeti/floe/actions/workflows/ci.yml)

## Why

Time Machine needs a disk you actually plug in. iCloud Drive syncs your files everywhere, but it keeps no history — overwrite a file or delete it, and it's gone on every device. floe closes that gap: daily encrypted, deduplicated, versioned snapshots, written into the iCloud (or Dropbox, Google Drive, external disk — any synced folder) storage you're already paying for.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/Jszigeti/floe/main/install.sh | sh
```

Or grab a binary from [GitHub Releases](https://github.com/Jszigeti/floe/releases), or build from source:

```bash
go install github.com/Jszigeti/floe/cmd/floe@latest
```

## Quickstart

```bash
floe init
```

The wizard downloads and verifies the pinned restic binary, asks for your backup destination (defaults to iCloud Drive), estimates the backup size and lets you exclude the largest directories, generates a passphrase and stores it in the macOS Keychain — shown once, save it now — asks for a daily backup hour, installs the LaunchAgent, and runs the first backup.

```bash
floe status    # snapshot count, repo size, schedule, iCloud quota
floe snapshots # full backup history
floe restore latest
```

## How it works

Each Mac gets its own restic repository at `<destination>/Floe.backup/<hostname>` — never shared between machines, since iCloud sync corrupts restic's repository locks if two Macs touch the same repo. A LaunchAgent runs `floe backup` daily (13:00 by default), which applies your hooks, prunes old snapshots under retention 7 daily / 4 weekly / 6 monthly, and then takes a new snapshot. The passphrase lives in the macOS Keychain (service `floe`, account `restic`) — `floe passphrase` prints it for your password manager. A weekly digest notification summarizes backup health.

## The iCloud problem

macOS can evict local file contents once they're synced to iCloud, replacing them with a "dataless" placeholder to save disk space. If that happens to files inside the restic repo, restic can fail reading them. `floe doctor` checks for evicted repo files and re-materializes them on demand, checks whether "Optimize Mac Storage" is enabled, checks remaining iCloud quota, and refuses to prune while an iCloud sync is still pending — pruning during a partial sync can corrupt the repo.

## Configuration

`~/.config/floe/config.toml`:

```toml
destination = "/Users/jonas/Library/Mobile Documents/com~apple~CloudDocs"
hour = 13

[retention]
daily = 7
weekly = 4
monthly = 6

[scope]
paths = ["/Users/jonas"]
exclude = [
  "/Users/jonas/Library/Caches",
  "/Users/jonas/.cache",
  "node_modules",
]

[hooks]
# Runs before each backup — see docs/examples/export-docker-volumes.sh
pre = ["~/.config/floe/hooks/export-docker-volumes.sh"]
post = []
```

## FAQ

**Is my data encrypted?**
Yes — restic encrypts everything client-side with AES-256 before it ever touches disk. Apple (or Dropbox, or Google) only ever sees ciphertext.

**What if I lose the passphrase?**
Your backup is unrecoverable. There is no reset. Save it in a password manager the moment `floe init` shows it to you — `floe passphrase` can print it again as long as it's still in your Keychain.

**Can I back up multiple Macs to the same iCloud account?**
Yes. Each Mac gets its own repository under `Floe.backup/<hostname>`, so they never collide.

**How do I restore on a new Mac?**
`floe init` sets up a fresh repository named after the new machine's hostname, so it won't see the old Mac's history by itself. To pull files from the old machine's backup: install floe (so the pinned restic binary is available at `~/.local/share/floe/bin/restic`), make sure the destination folder (e.g. iCloud Drive) is synced locally, then run restic directly against the old repository:

```bash
RESTIC_REPOSITORY="<destination>/Floe.backup/<old-hostname>" \
RESTIC_PASSWORD="<saved passphrase>" \
~/.local/share/floe/bin/restic restore latest --target ./restored
```

## License

MIT — see [LICENSE](LICENSE).
