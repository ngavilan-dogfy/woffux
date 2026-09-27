# Security

woffux holds a Woffu password and signs working hours for real people, so
security reports are welcome and get a quick answer.

## Reporting a vulnerability

Please report it privately through GitHub: **Security → Report a
vulnerability** on this repository, or
[this link](https://github.com/ngavilan-dogfy/woffux/security/advisories/new).
Don't open a public issue for it.

Include what you found, how to reproduce it and what it lets an attacker
do. Leave out real credentials, tokens and personal data — made-up ones
reproduce the problem just as well. You'll get an answer within a few days,
and credit in the release notes if you'd like.

Only the latest release is supported: fixes ship as a new release, which
`woffux update` and every GitHub signer pick up.

## What woffux stores, and where

| What | Where | Protection |
|---|---|---|
| Your Woffu password | The system keychain (service `woffux`) | The keychain's own; never written to a file or printed |
| Settings: email, company, office and home locations, schedule, timing seed, Telegram bot token | `~/.woffux.yaml` | Readable only by you (0600); woffux tightens it if it isn't |
| Woffu sessions | `<OS cache dir>/woffux/`, until they expire | Readable only by you |
| What this Mac's signer did | `~/Library/Logs/woffux-agent.log` | One line per run: times, office or remote, errors — no credentials |
| The GitHub backup's credentials, locations and timing seed | Encrypted Actions secrets in your repository | Only that repository's workflows can read them; its sign workflows run on a schedule or by hand, never on pull requests |

`woffux uninstall` removes all of it, including the secrets on GitHub.

## What woffux talks to

- **Woffu**: `app.woffu.com` and your company's `*.woffu.com` address. The
  company address is only ever taken from a `*.woffu.com` host, since the
  session cookie is sent there.
- **GitHub**: the releases of this repository (update checks and
  downloads) and, when the backup signer is on, your repository through the
  GitHub CLI.
- **Telegram**, only if you turn notifications on.
- **OpenStreetMap Nominatim**, only during setup and only when Woffu doesn't
  know where your office is: it receives the office's name.

There is no telemetry.

## The public fork

The GitHub backup runs in a fork of this repository, and forks of public
repositories are public. Its workflow file therefore shows your sign times
and time zone. Credentials and locations are secrets, not part of the file.
If your schedule shouldn't be public, let only this Mac sign.

## Releases and updates

Releases are built by GitHub Actions from `main` and published with a
`checksums.txt`. The installer and `woffux update` verify every binary
against it and check that it runs before replacing anything. GitHub
signers download the latest release on each run, so a release reaches them
within minutes: the tests run before every release is built.
