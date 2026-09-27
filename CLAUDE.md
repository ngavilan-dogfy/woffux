# woffux

Read [AGENTS.md](AGENTS.md): how to use this CLI and how to work on it.
[ARCHITECTURE.md](ARCHITECTURE.md) maps the code; [CONTRIBUTING.md](CONTRIBUTING.md)
covers the workflow and releases.

Essentials:

- Never sign something the user didn't expect: writes to Woffu need a
  confirmation, scheduled signs go through `planCatchUp` (`cmd/sign.go`),
  and a write is never retried.
- `make check` (gofmt, vet, tests, build) before committing; `make preview`
  to look at dashboard changes without a Woffu account.
- This Mac and the GitHub backup must compute the same moment
  (`internal/timing`); GitHub goes later.
- Tests never touch a real Woffu, GitHub, keychain or launchd.
- The skill is `cmd/skill_data/SKILL.md`, embedded in the binary;
  `.claude/skills/woffux/SKILL.md` must stay identical.
- Conventional commits pick the next version and write the release notes.
