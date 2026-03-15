<p align="center">
  <img src="assets/logo.png" alt="woffuk" width="280">
</p>

<h1 align="center">woffuk</h1>

<p align="center">Automatic clock in/out for <a href="https://app.woffu.com">Woffu</a>. Set it up once, never think about it again.</p>

<p align="center">
  <a href="https://github.com/ngavilan-dogfy/woffuk-cli/releases/latest"><img src="https://img.shields.io/github/v/release/ngavilan-dogfy/woffuk-cli?style=flat-square&color=7c3aed&label=release" alt="Release"></a>
  <a href="https://github.com/ngavilan-dogfy/woffuk-cli/actions/workflows/release.yml"><img src="https://img.shields.io/github/actions/workflow/status/ngavilan-dogfy/woffuk-cli/release.yml?style=flat-square&label=build" alt="Build"></a>
  <a href="https://goreportcard.com/report/github.com/ngavilan-dogfy/woffuk-cli"><img src="https://goreportcard.com/badge/github.com/ngavilan-dogfy/woffuk-cli?style=flat-square" alt="Go Report"></a>
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Linux-lightgrey?style=flat-square" alt="Platform">
  <img src="https://img.shields.io/badge/go-%3E%3D1.24-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-green?style=flat-square" alt="License"></a>
</p>

<br>

## What it does

- Clocks in/out on Woffu automatically via GitHub Actions
- Detects holidays, absences, and telework (approved **or pending**)
- Uses office or home GPS coordinates based on your work mode
- Sends Telegram notifications on every sign (optional)
- Interactive TUI dashboard to check your status anytime

## Install

### One-liner (recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/ngavilan-dogfy/woffuk-cli/main/install.sh | sh
```

Downloads the correct binary for your OS and architecture. No dependencies needed.

### Other options

<details>
<summary><strong>Download binary manually</strong></summary>

Go to [Releases](https://github.com/ngavilan-dogfy/woffuk-cli/releases/latest) and download:

| Platform | File |
|---|---|
| macOS Apple Silicon (M1+) | `woffuk-darwin-arm64` |
| macOS Intel | `woffuk-darwin-amd64` |
| Linux x64 | `woffuk-linux-amd64` |
| Linux ARM64 | `woffuk-linux-arm64` |

```bash
chmod +x woffuk-darwin-arm64
sudo mv woffuk-darwin-arm64 /usr/local/bin/woffuk
```

</details>

<details>
<summary><strong>Build from source (Go 1.24+)</strong></summary>

```bash
go install github.com/ngavilan-dogfy/woffuk-cli/cmd/woffuk@latest
```

</details>

### Prerequisites

You need the [GitHub CLI](https://cli.github.com) for auto-signing setup:

```bash
brew install gh        # macOS
sudo apt install gh    # Debian/Ubuntu
```

Then: `gh auth login`

## Setup

```bash
woffuk setup
```

The wizard does everything:

```
woffuk setup

┃ Login to Woffu
┃ Email: ngavilan@dogfydiet.com
┃ Password: ••••••••

◯ Connecting to Woffu...

✓ Logged in as NAHUEL GAVILAN BERNAL
→ Dogfy Diet — IT, Senior Platform Engineer
→ Office: Oficinas Landmark

┃ Home location
┃ > Paste a Google Maps URL
┃   Search by address
┃   Open interactive map

  → Open Google Maps, find your location, copy the URL.
  Paste Google Maps URL: https://www.google.com/maps/place/.../@41.385,2.173,17z/...

✓ 41.385064, 2.173404

┃ Use default schedule? Yes

┃ Enable Telegram notifications? Yes

  1. Open @BotFather on Telegram → /newbot → copy token
  2. Open @userinfobot on Telegram → get your Chat ID

◯ Sending test message...
✓ Test message sent! Check your Telegram.

◯ Setting up GitHub...
✓ Fork: yourusername/woffuk-cli
✓ Secrets + workflows configured

All set!
```

**What happens behind the scenes:**
1. Logs into Woffu → auto-detects company, office, department
2. Opens an interactive map in your browser to pick home location
3. Guides you step-by-step through Telegram bot creation
4. Forks this repo, sets all secrets, enables GitHub Actions

After setup, your Woffu is clocked in/out automatically every workday.

## Commands

| Command | Description |
|---|---|
| `woffuk` | Interactive TUI dashboard |
| `woffuk status` | Today's date, mode (office/remote), working day |
| `woffuk events` | Remaining vacations, hours, personal days |
| `woffuk sign` | Clock in/out right now |
| `woffuk schedule` | View auto-sign schedule |
| `woffuk schedule edit` | Edit schedule + push to GitHub |
| `woffuk sync` | Re-sync all secrets and workflows |
| `woffuk setup` | Re-run setup wizard |

## Auto-signing

GitHub Actions clocks you in/out on schedule:

| Day | Default times (CET) |
|---|---|
| Mon — Thu | 08:30, 13:30, 14:15, 17:30 |
| Fri | 08:00, 15:00 |

Each run adds a random 2–5 min delay for variance. Change times with `woffuk schedule edit`.

Two workflows:
- **Auto Sign** — runs on cron schedule
- **Manual Sign** — trigger from the Actions tab anytime

## How it works

```
1. Authenticate with Woffu
2. Fetch calendar → holidays, absences, telework
3. Determine mode: OFFICE or REMOTE (checks both approved and pending events)
4. Resolve GPS coordinates based on mode
5. POST /api/svc/signs/signs → clock in/out
6. Send Telegram notification (if configured)
```

## Location picker

Three ways to set your location:

1. **Google Maps URL** (easiest) — find your place on Google Maps, copy the URL, paste it
2. **Search by address** — type an address and pick from results
3. **Interactive map** — opens a map in your browser with search, click, and GPS location

## Telegram notifications

Optional. Get a message on every sign:

```
✅ Fichaje realizado correctamente
📅 2026-03-17
🏠 Teletrabajo
```

The setup wizard guides you through creating a bot and sends a test message to verify everything works.

## Configuration

| What | Where |
|---|---|
| Config | `~/.woffuk.yaml` |
| Password | OS keychain (never stored in plain text) |
| GitHub secrets | Set automatically by `woffuk setup` |

### Environment variables (CI)

Used by GitHub Actions. Set automatically — you don't need to touch these.

| Variable | Description |
|---|---|
| `WOFFU_URL` | Woffu API base URL |
| `WOFFU_COMPANY_URL` | Your company's Woffu URL |
| `WOFFU_EMAIL` / `WOFFU_PASSWORD` | Credentials |
| `WOFFU_LATITUDE` / `WOFFU_LONGITUDE` | Office coordinates |
| `WOFFU_HOME_LATITUDE` / `WOFFU_HOME_LONGITUDE` | Home coordinates |
| `TELEGRAM_BOT_TOKEN` / `TELEGRAM_CHAT_ID` | Telegram (optional) |

## License

MIT
