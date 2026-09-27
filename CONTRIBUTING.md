# Contributing

Thanks for taking the time. woffux signs real working hours for real
people, so the bar is simple: **never sign something the user didn't
expect.** This guide covers the development loop, the rules a change has to
follow, and how releases happen. For where things live and why, read
[ARCHITECTURE.md](ARCHITECTURE.md).

## Development loop

You need Go 1.25 or later. No Woffu account is needed for the tests or for
looking at the dashboard.

```bash
make check          # gofmt, go vet, the tests and a build into bin/woffux
./bin/woffux …      # try a change
make preview        # every dashboard state with made-up data: cat previews/02-working.ans
make install        # copy the build to ~/.local/bin/woffux (PREFIX=… to change it)
```

`TestViewFitsEverySizeAndScreen` renders every screen at many terminal
sizes and fails if a line overflows, so layout changes are safe to iterate
on. `make e2e` builds `bin/woffux-e2e`, which runs the setup against a fake
Woffu (see `cmd/e2e_hooks.go`); it is never part of a release.

With your own account you can try the read-only commands (`status`,
`calendar`, `doctor`…) against Woffu. Never sign or send a request to try
something: `--force`, `-y` and one-line requests really write.

## What a change needs

- **Safety, for anything that signs or sends.** A write to Woffu goes
  through a confirmation that shows what will be sent, and is never retried.
  Scheduled signing stays idempotent: a satisfied event is never signed
  again, and the direction guard (`--expected`) holds. This Mac and the
  GitHub backup compute the same moment (`internal/timing`), the backup a
  few minutes later.
- **Tests**, for every new decision path. Tests never talk to a real Woffu,
  GitHub, keychain or launchd: use a fake server (`internal/woffu`), made-up
  facts (`cmd/doctor_test.go`) or swapped functions (`cmd/uninstall_test.go`).
- **The output contract.** Queries render a styled view in a terminal, TSV
  when piped and JSON with `--json`, and put nothing but data on stdout.
  JSON field names are a public interface: renaming or removing one is a
  breaking change.
- **Help that teaches.** A command's `Long` help says what it's for and ends
  with examples.
- **The skill.** If commands or flags change, update
  `cmd/skill_data/SKILL.md` and copy it to `.claude/skills/woffux/SKILL.md`.
- **The GitHub signer's contract.** Signers download
  `releases/latest/download/woffux-linux-amd64` and run `woffux sign
  --catch-up …` with the flags the generated workflow uses: keep the asset
  names and those flags working.
- **No real data.** Screenshots come from the preview harness or the fake
  Woffu; examples use made-up people, companies and places.

## Commit messages

Commits on `main` follow [Conventional Commits](https://www.conventionalcommits.org).
They pick the next version and become the release notes, so write the
subject for someone reading "what's new" before updating — one change per
commit.

| Commit | Release |
|---|---|
| `feat: …`, `feat(setup): …` | minor — v5.**15**.0 |
| `fix: …`, `perf: …` | patch — v5.14.**1** |
| `feat!: …`, or `BREAKING CHANGE:` in the body | major — v**6**.0.0 |
| `docs:`, `refactor:`, `test:`, `build:`, `ci:`, `chore:`, `style:` | none |

`[skip release]` in a message keeps that commit out of the next release.
Pull requests are squash-merged, so the pull request's title is the commit
subject: write it the same way.

## Releases

Nobody cuts releases by hand. On every push to `main`,
[Auto Version](.github/workflows/auto-version.yml):

1. works out the next version from the commits since the last tag
   (`scripts/next-version.sh`);
2. runs the tests;
3. builds `woffux-<os>-<arch>` for macOS and Linux, plus `checksums.txt`
   (`scripts/build-release.sh`);
4. writes the notes from the commit subjects (`scripts/release-notes.sh`);
5. publishes the GitHub release, which creates the tag.

A release reaches everyone quickly: `woffux update`, the installer and the
update notice read it, and **every GitHub signer downloads the latest
release on its next run**. Commits made by `woffux sync` (the signer
workflows) start neither CI nor a release. To publish a specific version (a
major bump, say), push a tag — `git tag v6.0.0 && git push origin v6.0.0` —
and [Release](.github/workflows/release.yml) builds it the same way.
`make release VERSION=v5.14.0` builds the release files into `dist/`
locally, to look at before pushing.

## Screenshots and images

- Dashboard screenshots come from the preview harness (`make preview`).
- `assets/setup.gif` is recorded with `make e2e && scripts/record-setup.sh`
  ([vhs](https://github.com/charmbracelet/vhs)): a made-up person at a
  made-up company, served by `scripts/record/fakewoffu.py`, in a throwaway
  home directory. Look at the result frame by frame before committing it.
- `assets/social-preview.png` is the card GitHub shows when the repository
  is shared (*Settings → General → Social preview*). It's rendered from
  `scripts/record/social-preview.html` at 1280×640 — for example with
  `chrome --headless=new --window-size=1280,640 --screenshot=assets/social-preview.png scripts/record/social-preview.html`
  — whenever the tagline or the screenshot changes.
