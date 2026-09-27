# AGENTS.md

Guidance for AI agents: first for using `woffux` on someone's behalf, then
for working on this repository. Claude Code users get the same playbooks as a
skill with `woffux skill install`.

## Using the CLI

### Ground rules

1. **Read freely; write nothing without an explicit yes.** Signing,
   requests, cancellations, schedule and timing changes, turning signers on
   or off, updating and uninstalling all change something real. Say exactly
   what will happen and wait for a yes. `woffux sign --force` (sign on a
   non-working day) needs the user to insist.
2. **Use `--json` and parse it**; never guess a field. Answer in the user's
   language, conversationally, not with raw JSON.
3. **Start diagnoses with `woffux doctor --json`.** It checks the install,
   the saved password, this Mac's signer, the GitHub backup and the next
   automatic sign, and gives every problem a `fix`. Suggest the fix; run it
   only after the user agrees.

### Where to start

| The user asks | Run |
|---|---|
| "Have I signed today?" "Where do I stand?" | `woffux status --json` and `woffux today --json` |
| "Will it sign tomorrow?" "Why didn't it sign?" | `woffux doctor --json`, then `woffux agent status` and `woffux history --json` |
| "How many vacation days do I have?" | `woffux events --json` |
| "Request telework / vacation for …" | `woffux calendar --json`, `woffux requests --json`, then confirm, then `woffux request -t … -d …` |
| "What's my schedule?" | `woffux schedule --json`; `woffux timing` shows the next week's exact moments |

`woffux doctor --json` returns:

```json
{
  "ok": false,
  "checks": [
    {"section": "Settings", "status": "ok", "detail": "Woffu signs you in as ANA GARCÍA · Acme"},
    {"section": "This Mac signs", "status": "fail", "detail": "Installed, but launchd isn't running it: nothing signs", "fix": "woffux agent on"},
    {"section": "Autopilot", "status": "ok", "detail": "Next sign: IN Mon 28 Sep · 08:27 · This Mac, with GitHub as backup"}
  ]
}
```

`status` is `ok`, `warn`, `fail` or `info`; `ok` is false only when a check
fails.

### Read-only commands

`status`, `today`, `calendar [-m N]`, `holidays`, `events`, `requests`,
`history [--from D --to D]`, `schedule`, `whoami` (all with `--json`);
`timing`, `agent status`, `auto`, `doctor [--json]`, `version [--json]`,
`update --check`, `skill status`.

### Commands that change something — ask first

`sign` (in or out, now), `request -t TYPE -d DATES`, `request cancel ID`,
`schedule set|load|save|delete`, `timing natural|relaxed|exact`,
`agent on|off`, `auto on|off`, `sync`, `config edit`, `update`,
`uninstall` (only when the user asks to uninstall, confirmed twice).

`woffux request -t … -d …` and `woffux sign -y` send immediately, without a
prompt of their own: the confirmation is yours to get.

### How signing works (for diagnosis)

- **This Mac** (primary): launchd runs `woffux sign --scheduled` at :01,
  :16, :31 and :46. It follows the local settings, so schedule changes need
  no sync. When a sign's moment is up to 20 minutes away it waits for it
  (`WAIT` in the log).
- **GitHub Actions** (backup): cron-triggered, three minutes after the Mac's
  moment, so it only acts when the Mac was asleep. It needs `woffux sync`
  after schedule or timing changes; `doctor` says when it's out of date.
- **Natural timing**: each automatic sign has its own moment (in a bit
  early, out a bit late, never a shorter day), the same for every signer.
- Holidays, vacation and absences come from Woffu's calendar and are
  skipped; an event that already has a sign is never signed again.

### Conventions you can rely on

- stdout carries data only: JSON with `--json`, TSV with a header row when
  piped. Failures exit 1 with one line on stderr (`{"error":"…"}` with
  `--json`).
- JSON field names are stable: renaming one is a breaking change.
- Reads are retried by woffux; don't add retries, and never retry a sign or
  a request — check the state instead.

## Working on this repository

Read [ARCHITECTURE.md](ARCHITECTURE.md) first: it maps the code and explains
the decisions. [CONTRIBUTING.md](CONTRIBUTING.md) covers the workflow and
releases. The rules that matter most:

- **Never sign something the user didn't expect.** Anything that writes to
  Woffu goes through a confirmation; scheduled signing only through
  `planCatchUp` and its guards (`cmd/sign.go`). A write is never retried.
- **Both signers compute the same moment** (`internal/timing`), and GitHub
  goes later. Change one side, change the other.
- **`make check`** (gofmt, vet, tests, build) before every commit; `make
  preview` to look at dashboard changes without a Woffu account.
- **Tests never touch a real Woffu, GitHub, keychain or launchd.** Swap the
  function (see `installAgent`, `uninstallOps`, the doctor's facts) or use a
  fake server.
- **Every command speaks the output contract**: styled in a terminal, TSV
  when piped, `--json` for queries. JSON field names are public.
- **Keep the skill current.** When commands or flags change, update
  `cmd/skill_data/SKILL.md` and its copy in `.claude/skills/woffux/` (a test
  checks they match).
- **Release asset names and the workflows' `github.repository` guards are
  load-bearing**: signers download the assets by name, and forks must not run
  CI or releases.
- **Shared files**: `internal/selfupdate/` also lives in the other
  ngavilan-dogfy CLIs; port changes to them.
- **Conventional commits**: the subject becomes a line in the release notes.
