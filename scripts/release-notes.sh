#!/usr/bin/env bash
# Release notes from the conventional commits since the previous release:
#
#   scripts/release-notes.sh v1.2.0 [v1.1.0]
#
# Breaking changes, features, fixes and performance work get a section each;
# docs, chores, CI and refactors are left out. 'woffux update' shows these
# bullets before updating, so each one should read well on its own.
set -euo pipefail

tag="${1:?usage: scripts/release-notes.sh <new tag> [previous tag]}"
repo="ngavilan-dogfy/woffux"

prev="${2:-}"
if [[ -z "$prev" ]]; then
	# The tag may not exist yet (CI creates it with the release).
	if git rev-parse -q --verify "refs/tags/${tag}" >/dev/null; then
		prev="$(git describe --tags --abbrev=0 --match 'v[0-9]*' "${tag}^" 2>/dev/null || true)"
	else
		prev="$(git describe --tags --abbrev=0 --match 'v[0-9]*' HEAD 2>/dev/null || true)"
	fi
fi

end="HEAD"
if git rev-parse -q --verify "refs/tags/${tag}" >/dev/null; then
	end="$tag"
fi
range="$end"
if [[ -n "$prev" ]]; then
	range="${prev}..${end}"
fi

breaking=""
features=""
fixes=""
perf=""

# "feat(tui): add the thing" → "Add the thing"
describe() {
	local text
	text="$(printf '%s' "$1" | sed -E 's/^[[:alpha:]]+(\([^)]*\))?!?:[[:space:]]*//')"
	printf '%s%s' "$(printf '%s' "${text:0:1}" | tr '[:lower:]' '[:upper:]')" "${text:1}"
}

while IFS= read -r sha; do
	[[ -z "$sha" ]] && continue
	subject="$(git log -1 --format=%s "$sha")"
	body="$(git log -1 --format=%b "$sha")"
	short="${sha:0:7}"
	if printf '%s\n%s\n' "$subject" "$body" | grep -qi '\[skip release\]'; then
		continue
	fi
	line="- $(describe "$subject") (${short})"
	if printf '%s\n' "$subject" | grep -Eq '^[[:alpha:]]+(\([^)]*\))?!:' ||
		printf '%s\n' "$body" | grep -Eq '(^|[[:space:]])BREAKING[ -]CHANGE:'; then
		breaking+="${line}"$'\n'
	elif printf '%s\n' "$subject" | grep -Eq '^feat(\([^)]*\))?:'; then
		features+="${line}"$'\n'
	elif printf '%s\n' "$subject" | grep -Eq '^fix(\([^)]*\))?:'; then
		fixes+="${line}"$'\n'
	elif printf '%s\n' "$subject" | grep -Eq '^perf(\([^)]*\))?:'; then
		perf+="${line}"$'\n'
	fi
done < <(git rev-list --reverse --no-merges "$range")

section() {
	if [[ -n "$2" ]]; then
		printf '## %s\n\n%s\n' "$1" "$2"
	fi
}

section "Breaking changes" "$breaking"
section "Features" "$features"
section "Fixes" "$fixes"
section "Performance" "$perf"

if [[ -z "${breaking}${features}${fixes}${perf}" ]]; then
	printf 'Maintenance release.\n\n'
fi

# shellcheck disable=SC2016 # the backticks are Markdown
printf 'Update with `woffux update`, or install with\n\n'
# shellcheck disable=SC2016
printf '```sh\ncurl -fsSL https://raw.githubusercontent.com/%s/main/install.sh | sh\n```\n' "$repo"
if [[ -n "$prev" ]]; then
	printf '\n**Full Changelog**: https://github.com/%s/compare/%s...%s\n' "$repo" "$prev" "$tag"
fi
