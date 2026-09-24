# Contributing to woffux

Thanks for helping! woffux signs real work hours for real people, so the bar
is simple: **never sign something the user didn't expect.**

## Build and test

```bash
go build ./...
go test ./...
go vet ./...
```

Go 1.25+. No Woffu account is needed for the test suite.

## Preview the dashboard without Woffu

The TUI has a preview harness with realistic fake data (working, on a break,
late, holiday, calendar selection, palette, confirmations…):

```bash
WOFFUX_TUI_PREVIEW=/tmp/woffux-previews go test ./internal/tui -run TestPreview
cat /tmp/woffux-previews/02-working.ans
```

`TestViewFitsEverySizeAndScreen` renders every screen at many terminal sizes
and fails if any line overflows, so layout changes are safe to iterate on.

## Where things live

| Path | What |
|---|---|
| `cmd/` | CLI commands; `onboarding.go` + `setup.go` are the guided setup |
| `internal/tui/` | Dashboard (Bubble Tea). `dayplan.go` holds the pure "where do I stand" logic |
| `internal/timing/` | Natural timing: deterministic, HR-safe sign moments |
| `internal/config/` | Config, schedule text parser (`schedtext.go`), seasonal presets |
| `internal/woffu/` | Woffu API client |
| `internal/github/` | Fork, secrets and workflow generation for the GitHub backup |
| `internal/agent/` | macOS launchd agent |

## Safety rules for changes that sign or send

- Anything that writes to Woffu must go through a confirmation in the UI.
- Scheduled signing must stay idempotent: a satisfied event is never signed
  again, and the direction guard (`--expected`) must hold.
- The local agent and the GitHub backup must compute the same moment
  (`internal/timing`); the backup always goes a few minutes later.
- Add a test for every new decision path.

## Commits and releases

Conventional Commits on `main` release automatically:
`fix:` → patch, `feat:` → minor, `feat!:` / `BREAKING CHANGE:` → major.
`docs:`, `chore:`, `ci:`, `test:` don't release.
