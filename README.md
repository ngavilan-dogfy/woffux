# woffuk-cli

Go CLI to automatically clock in/out of [Woffu](https://app.woffu.com). Detects remote work days, public holidays and absences to decide whether to sign and which GPS coordinates to use.

## Quick start

### 1. Install

```bash
go install github.com/ngavilan-dogfy/woffuk-cli@latest
```

### 2. Setup

```bash
woffuk setup
```

The wizard will:
- Ask for your Woffu credentials
- Ask for your office and home addresses (auto-geocoded to GPS)
- Let you configure the auto-sign schedule
- Fork this repo on GitHub
- Set up all secrets and enable GitHub Actions

That's it. Auto-signing is now active.

## Commands

```bash
woffuk                  # Interactive TUI dashboard
woffuk sign             # Clock in/out manually
woffuk status           # Today's signing status
woffuk events           # Available vacations, hours, etc.
woffuk schedule         # View auto-sign schedule
woffuk schedule edit    # Edit schedule and push to GitHub
woffuk schedule push    # Push current schedule as workflows
woffuk sync             # Re-sync secrets + workflows to GitHub
woffuk setup            # Run the setup wizard again
```

## Auto-signing (GitHub Actions)

Two workflows are included:

- **Auto Sign** — runs on schedule with a random 2-5 min delay
- **Manual Sign** — trigger manually from the Actions tab

Default schedule (CET):

| Day | Times |
|---|---|
| Mon-Thu | 08:30, 13:30, 14:15, 17:30 |
| Fri | 08:00, 15:00 |

Edit with `woffuk schedule edit` and push to your fork.

## How it works

1. Authenticates with your Woffu credentials
2. Checks your calendar for holidays, absences, and telework events
3. Decides whether to sign today
4. If telework (approved or pending), signs with home coordinates
5. Otherwise, signs with office coordinates

## Configuration

Config is stored in `~/.woffuk.yaml`. Password is stored in your OS keychain (macOS Keychain, Linux keyring, Windows Credential Manager).

In CI (GitHub Actions), config is read from environment variables:

| Variable | Description |
|---|---|
| `WOFFU_URL` | `https://app.woffu.com/api` |
| `WOFFU_COMPANY_URL` | Your company's Woffu URL |
| `WOFFU_EMAIL` | Your Woffu email |
| `WOFFU_PASSWORD` | Your Woffu password |
| `WOFFU_LATITUDE` | Office GPS latitude |
| `WOFFU_LONGITUDE` | Office GPS longitude |
| `WOFFU_HOME_LATITUDE` | Home GPS latitude |
| `WOFFU_HOME_LONGITUDE` | Home GPS longitude |

## Requirements

- Go 1.24+
- [gh](https://cli.github.com/) CLI (for setup and sync commands)
