## What and why

<!-- The title becomes the commit subject and a line in the release notes: write it as a conventional commit (feat: …, fix: …). -->

## How I tested it

- [ ] `make check` passes (gofmt, vet, tests, build)
- [ ] Anything that writes to Woffu still asks first, and is never retried
- [ ] Both signers still compute the same moment; GitHub still goes later
- [ ] Dashboard changes checked with `make preview`
- [ ] Commands or flags changed: `cmd/skill_data/SKILL.md` and `.claude/skills/woffux/SKILL.md` updated
- [ ] Docs updated (README, ARCHITECTURE) if behaviour or an invariant changed
