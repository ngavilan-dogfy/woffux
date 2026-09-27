# Architecture

This document is a map: where things live, how a sign happens, and the
decisions that shaped the code. It describes what is true today; if you
change one of the invariants below, change this file in the same commit.

## Bird's-eye view

woffux is one Go binary with three faces — commands, the dashboard and the
signers — over one Woffu client:

```
              you · scripts · Claude Code
                          │
   ┌──────────────────────▼───────────────────────┐        ┌──────────────────────┐
   │ cmd/   commands · setup · doctor · update    │─woffux─▶│ internal/tui         │
   │        terminal · TSV · --json               │        │ the dashboard        │
   └───────┬───────────────┬──────────────┬───────┘        └──────────────────────┘
           │               │              │
   ┌───────▼──────┐ ┌──────▼───────┐ ┌────▼──────────────────┐
   │ internal/    │ │ internal/    │ │ internal/github       │
   │ woffu        │ │ config ·     │ │ fork · secrets ·      │
   │ REST client  │ │ timing       │ │ workflow generation   │
   └───────┬──────┘ └──────────────┘ └────┬──────────────────┘
           │                              │ gh
       Woffu API                   your GitHub repository
```

Two signers run the same binary on a timer and reach the same decision:

```
 launchd, every 15 min (:01 :16 :31 :46)       GitHub Actions cron (from your week)
   woffux sign --scheduled                       woffux sign --catch-up <week> [--season …]
   reads ~/.woffux.yaml                          reads WOFFU_* secrets, runs 3 min later
                     └───────────────┬──────────────────────┘
                          one decision (cmd/sign.go)
               today's moment → already signed? → sign once → verify
```

## Code map

| Path | What lives there |
|---|---|
| `cmd/woffux/main.go` | Entry point: `cmd.Execute()`. |
| `cmd/` | One file per command area, registered in `init()`. `root.go` runs the command, prints failures (one line; JSON with `--json`) and the daily update notice, and offers the setup when there are no settings. `output.go` and `ui.go` are the terminal / TSV / JSON helpers and the small visual vocabulary every command shares; `views.go` renders the query commands. |
| `cmd/sign.go` | The signing decision: today's events, their natural moments, the catch-up window, waiting for a moment up to 20 minutes ahead, the direction guard, one sign and its verification. |
| `cmd/` — onboarding | `setup.go` (sign-in, locations, who signs, Telegram), `onboarding.go` (step headers, the schedule and timing wizards), `config.go` (`config edit`), `doctor.go`, `uninstall.go`, `update.go` + `version.go`, `skill.go` (the embedded Claude Code skill). |
| `internal/woffu` | The Woffu client: account lookup and sign-in (`auth.go`), the session cache (`tokencache.go`), calendar, signs, history and requests. Reads are retried; writes never are. |
| `internal/config` | `~/.woffux.yaml`, the keychain, the schedule text parser (`schedtext.go`), presets and seasons. |
| `internal/timing` | Natural timing: each sign's moment from a private seed and the date. |
| `internal/agent` | This Mac's signer: the launchd plist and its log. |
| `internal/github` | The GitHub backup: fork or repository setup, secrets, the generated workflows (sign, manual sign, keepalive), their status and sync. |
| `internal/tui` | The dashboard. `model.go` holds state, `commands.go` the asynchronous work, `screens.go` / `today.go` / `calendar.go` / `schedule.go` the screens, `dayplan.go` the pure "where do I stand" logic. |
| `internal/geocode` | Google Maps link parsing and OpenStreetMap search for the office. |
| `internal/notify` | Telegram messages. |
| `internal/selfupdate` | `woffux update` and the update notice. Shared, file for file, with the other ngavilan-dogfy CLIs. |
| `scripts/`, `install.sh`, `.github/workflows/` | Release pipeline, installer and the setup recording (see [CONTRIBUTING.md](CONTRIBUTING.md)). |

## How a scheduled sign happens

1. **Something wakes up.** launchd runs `woffux sign --scheduled` four times an
   hour; the GitHub workflow runs `woffux sign --catch-up <week>` from crons
   generated for each sign time (in both UTC offsets, for daylight saving) plus
   a 15-minute catch-up cron during working hours, after a short random delay.
2. **A cheap check first.** If no event today is due — within the two-hour
   catch-up window, or up to 20 minutes ahead — it logs `SKIP` and exits
   without touching the network.
3. **Woffu decides whether today counts.** It signs in (reusing a cached
   session) and reads today: not a working day means `SKIP`.
4. **The plan.** The next direction is IN unless you're signed in. For each of
   today's events in that direction, its natural moment (three minutes later
   for GitHub); an event that already has a matching sign is satisfied. The
   most recent overdue event wins; otherwise one up to 20 minutes ahead is
   waited for (`WAIT`) and everything re-checked.
5. **One sign, verified.** The sign is a single request, never retried. The
   in/out state must flip afterwards, or the run fails and says so. `OK` goes
   to the log, and to Telegram when it's on.

## The dashboard's data flow

Screens render from the model and never call Woffu or GitHub themselves:
work runs as Bubble Tea commands and comes back as messages that update the
model. `dayplan.go` turns the schedule, the timing and today's signs into
"where do I stand" without any I/O, so its answers are tested directly.
Anything that writes to Woffu goes through a confirmation that shows what
will be sent.

## Decisions

**Two signers, one moment.** GitHub's cron fires late, sometimes by hours, so
the Mac is the primary signer and GitHub the fallback. Both compute each
sign's moment the same way and check Woffu before acting; GitHub runs three
minutes later, so when the Mac is awake GitHub finds the event satisfied.
Idempotency comes from "is this event already satisfied?", not from
coordination between signers.

**Natural timing is deterministic.** A moment is a function of a private seed
and the date, so this Mac, the GitHub backup (the seed travels in the
`WOFFUX_TIMING` secret) and the dashboard agree without talking to each other.
The windows lean the safe way — in early, out late — and a work block is
never shorter than planned.

**Writes are single-shot and verified.** A lost response doesn't mean Woffu
didn't apply a sign, so signs and requests are never retried; the sign is
verified by reading the state back. Reads are retried on timeouts, rate
limits and Woffu errors. A login is retried only on network errors: repeating
a password on an error we don't understand could lock the account.

**The GitHub backup is a public fork.** A fork of a public repository is
public, so the workflow shows sign times, and everything else — credentials,
locations, the timing seed — is an encrypted secret. The workflow is
generated from your settings and pushed by `woffux sync`: through the
Contents API for the upstream owner (so a working copy is never clobbered),
by commit for forks. A monthly keepalive commit stops GitHub pausing
scheduled workflows after 60 quiet days.

**Setup asks Woffu, then saves.** The company's Woffu address and whether
password sign-in is allowed come from the same lookup Woffu's login page
does, before any password is sent. Nothing is saved until every step passed.

**Doctor gathers facts, then judges them.** The probes only read; the
verdicts are pure functions over the facts, tested with made-up installs.

## Invariants

- Nothing writes to Woffu without a person's confirmation, except scheduled
  signing, which only happens through the plan above and its guards.
- A write to Woffu is never retried.
- This Mac and the GitHub backup compute the same moment; GitHub always goes
  later.
- Release asset names never change: every GitHub signer downloads
  `releases/latest/download/woffux-linux-amd64`.
- The repository's own workflows only run in `ngavilan-dogfy/woffux`
  (`if: github.repository == …`): forks are signers, not development copies.
- Every frame of the dashboard fits the terminal; `TestViewFitsEverySizeAndScreen`
  renders every screen at many sizes.
- Tests never talk to a real Woffu, GitHub, keychain or launchd.

## Testing

| Layer | How |
|---|---|
| Woffu client | `internal/woffu`: fake servers over HTTP for retries, the session cache, sign-in errors and the account lookup. |
| Signing decisions | `cmd/sign_test.go`: catch-up plans with natural timing, satisfied events, manual signs, the GitHub grace, seasons. |
| Schedules and timing | `internal/config` (the text parser, presets, seasons) and `internal/timing` (the windows' rules). |
| GitHub workflows | `internal/github`: generated crons across time zones and daylight saving, workflow YAML, status parsing. |
| Dashboard | `internal/tui`: a harness that drives the model key by key and checks every frame's size; `TestPreview` renders every state with made-up data (`make preview`). |
| Commands | `cmd/`: doctor verdicts from made-up facts, uninstall with fake actions, the update's follow-up for this Mac's signer, the skill copies. |
| Release | CI on Linux and macOS with the race detector, gofmt, the `e2e` build vetted, the installer and scripts shellchecked, release notes rendered. |
