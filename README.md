<p align="center">
  <img src="assets/logo.png" alt="woffux" width="280">
</p>

<h1 align="center">woffux</h1>

<p align="center">Automatic clock in/out for <a href="https://app.woffu.com">Woffu</a>. Set it up once, never think about it again.</p>

<p align="center">
  <a href="https://github.com/ngavilan-dogfy/woffux/releases/latest"><img src="https://img.shields.io/github/v/release/ngavilan-dogfy/woffux?style=flat-square&color=7c3aed&label=release" alt="Release"></a>
  <a href="https://github.com/ngavilan-dogfy/woffux/actions/workflows/release.yml"><img src="https://img.shields.io/github/actions/workflow/status/ngavilan-dogfy/woffux/release.yml?style=flat-square&label=build" alt="Build"></a>
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Linux-lightgrey?style=flat-square" alt="Platform">
  <img src="https://img.shields.io/badge/go-%3E%3D1.24-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-green?style=flat-square" alt="License"></a>
</p>

<br>

## What it does

- Clocks in/out on Woffu **automatically** via GitHub Actions
- Detects holidays, absences, and telework (approved **or pending**)
- Uses office or home GPS coordinates based on your work mode
- **Create and cancel requests** (telework, vacation, absence) from the CLI
- Sends **Telegram notifications** on every sign (optional)
- **Interactive TUI dashboard** with tabs, overlays, and live sign status
- Fully **scriptable** — `--json` and `--plain` output on all query commands

## Install

### One-liner (recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/ngavilan-dogfy/woffux/main/install.sh | sh
```

Downloads the correct binary for your OS/arch. No dependencies.

### Other options

<details>
<summary><strong>Download binary manually</strong></summary>

Go to [Releases](https://github.com/ngavilan-dogfy/woffux/releases/latest) and download:

| Platform | File |
|---|---|
| macOS Apple Silicon (M1+) | `woffux-darwin-arm64` |
| macOS Intel | `woffux-darwin-amd64` |
| Linux x64 | `woffux-linux-amd64` |
| Linux ARM64 | `woffux-linux-arm64` |

```bash
chmod +x woffux-darwin-arm64
sudo mv woffux-darwin-arm64 /usr/local/bin/woffux
```

</details>

<details>
<summary><strong>Build from source (Go 1.24+)</strong></summary>

```bash
go install github.com/ngavilan-dogfy/woffux/cmd/woffux@latest
```

Or clone and build:

```bash
git clone https://github.com/ngavilan-dogfy/woffux.git
cd woffux
go build -o woffux ./cmd/woffux
sudo mv woffux /usr/local/bin/
```

</details>

### Prerequisites

[**Git**](https://git-scm.com) is required for setup and syncing workflows to your fork.

The [**GitHub CLI**](https://cli.github.com) is needed for auto-signing. The setup wizard checks for it and guides you through installation if missing.

```bash
brew install git gh        # macOS
sudo apt install git gh    # Debian/Ubuntu
sudo dnf install git gh    # Fedora
```

Then: `gh auth login`

### Update

```bash
woffux update
```

## Setup

```bash
woffux setup
```

You only need your **email** and **password**. The wizard auto-detects everything else:

```
woffux setup

  ✓ GitHub CLI ready

┃ Email: ngavilan@dogfydiet.com
┃ Password: ••••••••

◯ Signing in to dogfydiet.woffu.com...

  ✓ Logged in as NAHUEL GAVILAN BERNAL
  → Dogfy Diet — IT, Senior Platform Engineer
  → Office: Oficinas Landmark

┃ Home location > Paste a Google Maps URL
  ✓ 41.385064, 2.173404

┃ Auto-sign schedule > Standard split (8.5h)

┃ Enable Telegram? Yes
  ◯ Sending test message...
  ✓ Test message sent!

◯ Setting up GitHub...
  ✓ Secrets + workflows configured

All set!
```

## Commands

### Querying

| Command | Description | Flags |
|---|---|---|
| `woffux status` | Today: working day, mode, coordinates | `--json` `--plain` |
| `woffux today` | Detailed day info + today's sign slots | `--json` |
| `woffux events` | Remaining vacations, hours, personal days | `--json` `--plain` |
| `woffux requests` | Your requests (telework, vacation, absence) | `--json` `--plain` `--page` `--size` |
| `woffux history` | Sign history (clock in/out records) | `--json` `--plain` `--from` `--to` `-d` |
| `woffux calendar` | Working days, holidays, telework for a month | `--json` `--plain` `-m` |
| `woffux holidays` | Company calendar holidays | `--json` `--plain` |
| `woffux schedule` | View auto-sign schedule | `--json` |
| `woffux whoami` | Current user profile | `--json` |

### Actions

| Command | Description |
|---|---|
| `woffux sign` | Clock in/out right now |
| `woffux sign --force` | Sign even on non-working days |
| `woffux request` | Create a request (telework, vacation, absence) |
| `woffux request -t Teletrabajo -d 2026-03-20` | Request telework for a specific date |
| `woffux request cancel <id>` | Cancel a request |
| `woffux auto` | Check if auto-signing is active |
| `woffux auto on` / `off` | Toggle auto-signing |
| `woffux open` | Open Woffu dashboard in browser |
| `woffux open docs` | Open personal documents |
| `woffux open calendar` | Open calendar view |
| `woffux open github` | Open GitHub fork actions |

### Configuration

| Command | Description |
|---|---|
| `woffux setup` | Full setup wizard |
| `woffux config` | View all settings at a glance |
| `woffux config edit` | Change any individual setting |
| `woffux schedule edit` | Edit schedule with presets or custom blocks |
| `woffux sync` | Push local config to GitHub |
| `woffux update` | Update to latest version |
| `woffux --version` | Show current version |

## Output modes

All query commands auto-detect your terminal:

| Context | Format | Use case |
|---|---|---|
| Terminal | Colored, human-friendly | Reading |
| Piped (`\|`, `>`) | TSV (tab-separated) | `awk`, `grep`, `cut` |
| `--json` | Structured JSON | `jq`, scripting |
| `--plain` | Force TSV in terminal | Consistent output |

### Scripting examples

```bash
# How many vacation days left?
woffux events --json | jq '.[] | select(.name == "Vacaciones") | .available'

# Approved telework days this month
woffux requests --json | jq '[.[] | select(.event_name | contains("Teletrabajo")) | select(.status == "approved")] | length'

# Next holiday
woffux holidays --json | jq '.[0]'

# Export calendar to CSV
woffux calendar --plain > march.tsv

# Am I clocked in right now?
woffux today --json | jq '.slots[-1].out // "still clocked in"'

# Request telework for next week
woffux request -t Teletrabajo -d 2026-03-23,2026-03-25,2026-03-27

# Cancel all pending requests
woffux requests --json | jq -r '.[] | select(.status == "pending") | .request_id' | xargs -I{} woffux request cancel {}
```

## Interactive TUI

```bash
woffux
```

Multi-tab dashboard with live data:

- **Status** — today's sign info, sign slots (clocked in/out), schedule, auto-sign status
- **Events** — available vacations, hours, personal days
- **Calendar** — upcoming holidays and events

Keyboard shortcuts:

| Key | Action |
|---|---|
| `Tab` / `1-3` | Switch tabs |
| `s` | Sign (with confirmation) |
| `a` | Toggle auto-sign (with confirmation) |
| `r` | Refresh data |
| `o` | Open Woffu in browser |
| `g` | Open GitHub Actions |
| `Enter` | Action menu (sign, auto-sign, sync, presets, open) |
| `q` | Quit |

## Auto-signing

GitHub Actions clocks you in/out on schedule. Toggle with `woffux auto on/off`.

| Day | Default times (CET) |
|---|---|
| Mon — Thu | 08:30, 13:30, 14:15, 17:30 |
| Fri | 08:00, 15:00 |

Each run adds a random 2–5 min delay for variance.

### Schedule presets

```
> Standard split (8.5h)   IN 08:30  OUT 13:30  IN 14:15  OUT 17:30
  Intensive (6h)          IN 08:00  OUT 14:00
  Morning shift (7h)      IN 07:00  OUT 14:00
  Flexible (8h)           IN 09:00  OUT 14:00  IN 15:00  OUT 18:00
  Custom — pick days and define blocks
```

Custom schedules support multi-select days, per-day blocks, and can be saved as named presets (e.g., "summer", "winter") to switch between them.

### Syncing

Your local config (`~/.woffux.yaml`) is the source of truth. Run `woffux sync` to push changes to GitHub so auto-signing uses your latest settings.

```bash
woffux sync

  Syncing local config → yourusername/woffux

  ✓ Secrets               email, password, office (41.35,2.14), home (41.19,1.60)
  ✓ Workflows             5 days, 3 signs/day, tz=CET

  ✓ GitHub is up to date. Auto-signing will use these settings.
```

### Workflows

| Workflow | Purpose |
|---|---|
| **Auto Sign** | Cron schedule — downloads binary, signs |
| **Manual Sign** | Trigger from Actions tab anytime |
| **Keepalive** | Prevents GitHub from auto-disabling workflows after 60 days |

## Requests

Create and cancel telework, vacation, and absence requests directly from the CLI:

```bash
# Interactive — pick type and dates
woffux request

# One-liner
woffux request -t "Teletrabajo🏡" -d 2026-03-20

# Batch — multiple dates
woffux request -t Vacaciones -d 2026-08-01,2026-08-02,2026-08-03,2026-08-04,2026-08-05

# List your requests
woffux requests

# Cancel a request
woffux request cancel 17117405
```

## Telegram notifications

Optional. Get a message on every sign:

```
✅ Fichaje realizado correctamente
📅 2026-03-17
🏠 Teletrabajo
```

The setup wizard walks you through creating a bot with @BotFather and verifies the connection with a test message.

## Configuration

| What | Where |
|---|---|
| Config | `~/.woffux.yaml` |
| Password | OS keychain (macOS Keychain / Linux keyring) |
| GitHub secrets | Set automatically by `woffux setup` and `woffux sync` |

View with `woffux config`. Edit individual settings with `woffux config edit` — it explains when and why to sync after changes.

### Environment variables (CI)

Used by GitHub Actions. Set automatically by `woffux setup`.

| Variable | Required | Description |
|---|---|---|
| `WOFFU_URL` | Yes | `https://app.woffu.com/api` |
| `WOFFU_COMPANY_URL` | Yes | `https://yourcompany.woffu.com` |
| `WOFFU_EMAIL` | Yes | Woffu login email |
| `WOFFU_PASSWORD` | Yes | Woffu password |
| `WOFFU_LATITUDE` | Yes | Office latitude |
| `WOFFU_LONGITUDE` | Yes | Office longitude |
| `WOFFU_HOME_LATITUDE` | Yes | Home latitude |
| `WOFFU_HOME_LONGITUDE` | Yes | Home longitude |
| `TELEGRAM_BOT_TOKEN` | No | Telegram bot token |
| `TELEGRAM_CHAT_ID` | No | Telegram chat ID |

## Troubleshooting

| Problem | Solution |
|---|---|
| `woffux: command not found` | Binary not in PATH — `sudo mv woffux /usr/local/bin/` |
| `config not found` | Run `woffux setup` |
| `password not in keychain` | Run `woffux setup` to reconfigure |
| Auth fails after password change | `woffux config edit` → Password → sync |
| Auto-signing stopped | `woffux auto` to check. Keepalive prevents 60-day disable |
| Wrong coordinates | `woffux config edit` → Office/Home → sync |
| Telegram not working | `woffux config edit` → Telegram → sync |
| Changes not taking effect | Run `woffux sync` — local config must be pushed to GitHub |
| `gh` not installed | Setup wizard guides you through installation |
| Multiple GitHub accounts | `gh auth switch` to the right account, then `woffux sync` |

## License

MIT
