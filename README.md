<p align="center">
  <img src="assets/logo.png" alt="woffuk" width="280">
</p>

<h1 align="center">woffuk</h1>

<p align="center">Automatic clock in/out for <a href="https://app.woffu.com">Woffu</a>. Set it up once, never think about it again.</p>

<p align="center">
  <a href="https://github.com/ngavilan-dogfy/woffuk-cli/releases/latest"><img src="https://img.shields.io/github/v/release/ngavilan-dogfy/woffuk-cli?style=flat-square&color=7c3aed&label=release" alt="Release"></a>
  <a href="https://github.com/ngavilan-dogfy/woffuk-cli/actions/workflows/release.yml"><img src="https://img.shields.io/github/actions/workflow/status/ngavilan-dogfy/woffuk-cli/release.yml?style=flat-square&label=build" alt="Build"></a>
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Linux-lightgrey?style=flat-square" alt="Platform">
  <img src="https://img.shields.io/badge/go-%3E%3D1.24-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-green?style=flat-square" alt="License"></a>
</p>

<br>

## What it does

- Clocks in/out on Woffu **automatically** via GitHub Actions
- Detects holidays, absences, and telework (approved **or pending**)
- Uses office or home GPS coordinates based on your work mode
- Sends **Telegram notifications** on every sign (optional)
- **Interactive TUI dashboard** to check your status anytime
- **Toggle auto-signing** on/off with one command

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

Or clone and build:

```bash
git clone https://github.com/ngavilan-dogfy/woffuk-cli.git
cd woffuk-cli
go build -o woffuk ./cmd/woffuk
sudo mv woffuk /usr/local/bin/
```

</details>

### Prerequisites

The [GitHub CLI](https://cli.github.com) is needed for auto-signing setup. The setup wizard will guide you through installing it if you don't have it.

```bash
brew install gh        # macOS
sudo apt install gh    # Debian/Ubuntu
sudo dnf install gh    # Fedora
```

Then: `gh auth login`

## Setup

```bash
woffuk setup
```

The wizard does everything — you only need your **email** and **password**:

```
woffuk setup

  ✓ GitHub CLI ready

┃ Login to Woffu
┃ Email: ngavilan@dogfydiet.com
┃ Password: ••••••••

◯ Signing in to dogfydiet.woffu.com...

  ✓ Logged in as NAHUEL GAVILAN BERNAL
  → Dogfy Diet — IT, Senior Platform Engineer
  → Office: Oficinas Landmark

┃ Home location
┃ > Paste a Google Maps URL
┃   Enter coordinates manually

  → Open Google Maps, find your location, copy the URL.
  Paste: https://www.google.com/maps/place/.../@41.385,2.173,17z/...
  ✓ 41.385064, 2.173404

┃ Auto-sign schedule
┃ > Standard split (8.5h)   IN 08:30  OUT 13:30  IN 14:15  OUT 17:30

┃ Enable Telegram notifications? Yes

  1. Open @BotFather → /newbot → copy token
  2. Open @userinfobot → get Chat ID
  ◯ Sending test message...
  ✓ Test message sent!

◯ Setting up GitHub...
  ✓ Fork: yourusername/woffuk-cli
  ✓ Secrets + workflows configured

All set!
  Mon  ▶ 08:30  ■ 13:30  ▶ 14:15  ■ 17:30
  Fri  ▶ 08:00  ■ 15:00
```

**What happens behind the scenes:**

1. Auto-detects company, office name, department from your Woffu account
2. Resolves office GPS from Woffu (or geocodes the office name)
3. Opens Google Maps for you to pick your home location
4. Guides you through Telegram bot setup with a test message
5. Forks this repo, sets all secrets, pushes workflows, enables GitHub Actions

After setup, your Woffu is clocked in/out automatically every workday.

## Commands

| Command | Description |
|---|---|
| `woffuk` | Interactive TUI dashboard |
| `woffuk status` | Today's date, mode (office/remote), working day |
| `woffuk events` | Remaining vacations, hours, personal days |
| `woffuk sign` | Clock in/out right now |
| `woffuk config` | View all settings at a glance |
| `woffuk config edit` | Change any individual setting |
| `woffuk schedule` | View auto-sign schedule with IN/OUT indicators |
| `woffuk schedule edit` | Change schedule and push to GitHub |
| `woffuk auto` | Check if auto-signing is active |
| `woffuk auto on` | Enable auto-signing |
| `woffuk auto off` | Disable auto-signing |
| `woffuk sync` | Re-sync all secrets and workflows |
| `woffuk setup` | Re-run setup wizard |

## Auto-signing

GitHub Actions clocks you in/out on schedule. Toggle with `woffuk auto on/off`.

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
  Custom — define your own blocks
```

Change anytime with `woffuk schedule edit`.

### Workflows included

| Workflow | Purpose |
|---|---|
| **Auto Sign** | Runs on cron schedule |
| **Manual Sign** | Trigger from Actions tab anytime |
| **Keepalive** | Prevents GitHub from auto-disabling workflows after 60 days |

## How it works

```
1. Authenticate with Woffu
2. Fetch calendar → holidays, absences, telework
3. Determine mode: OFFICE or REMOTE (checks approved + pending events)
4. Resolve GPS coordinates based on mode
5. POST /api/svc/signs/signs → clock in/out
6. Send Telegram notification (if configured)
```

## Location picker

Two ways to set your location during setup:

1. **Google Maps URL** (recommended) — find your place on Google Maps, copy the URL, paste it. Coordinates are extracted automatically.
2. **Manual coordinates** — enter latitude and longitude directly.

## Telegram notifications

Optional. Get a message on every sign:

```
✅ Fichaje realizado correctamente
📅 2026-03-17
🏠 Teletrabajo
```

The setup wizard guides you step-by-step through creating a Telegram bot and sends a test message to verify the connection.

## Configuration

| What | Where |
|---|---|
| Config | `~/.woffuk.yaml` |
| Password | OS keychain (macOS Keychain / Linux keyring) |
| GitHub secrets | Set automatically by `woffuk setup` |

View with `woffuk config`. Edit individual settings with `woffuk config edit`.

### Environment variables (CI)

Used by GitHub Actions. Set automatically — you don't need to touch these.

| Variable | Required | Description |
|---|---|---|
| `WOFFU_URL` | Yes | Woffu API base URL |
| `WOFFU_COMPANY_URL` | Yes | Your company's Woffu URL |
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
| `woffuk: command not found` | Make sure the binary is in your PATH. Try `sudo mv woffuk /usr/local/bin/` |
| `config not found` | Run `woffuk setup` |
| `password not in keychain` | Run `woffuk setup` to reconfigure |
| Auth fails after password change | Run `woffuk config edit` → Password |
| Auto-signing stopped working | Run `woffuk auto` to check status. GitHub disables workflows after 60 days of inactivity (the keepalive workflow prevents this) |
| Wrong coordinates | Run `woffuk config edit` → Office/Home |
| Telegram not working | Run `woffuk config edit` → Telegram |

## License

MIT
