#!/usr/bin/env bash
set -euo pipefail

latest_tag="${LATEST_TAG:-$(git tag --list 'v[0-9]*.[0-9]*.[0-9]*' --sort=-v:refname | head -n 1)}"

if [[ -n "$latest_tag" ]]; then
	range="${latest_tag}..HEAD"
	base_version="${latest_tag#v}"
else
	range="HEAD"
	base_version="0.0.0"
fi

commits="$(git rev-list --reverse "$range" 2>/dev/null || true)"

bump="none"
if [[ -n "$commits" ]]; then
	for sha in $commits; do
		subject="$(git log -1 --format=%s "$sha")"
		body="$(git log -1 --format=%b "$sha")"

		if printf '%s\n%s\n' "$subject" "$body" | grep -qi '\[skip release\]'; then
			continue
		fi

		if printf '%s\n' "$subject" | grep -Eq '^[[:alpha:]][[:alnum:]_-]*(\([^)]+\))?!:' ||
			printf '%s\n' "$body" | grep -Eq '(^|[[:space:]])BREAKING[ -]CHANGE:'; then
			bump="major"
			break
		fi

		if printf '%s\n' "$subject" | grep -Eq '^feat(\([^)]+\))?:'; then
			if [[ "$bump" != "minor" ]]; then
				bump="minor"
			fi
			continue
		fi

		if printf '%s\n' "$subject" | grep -Eq '^(fix|perf)(\([^)]+\))?:'; then
			if [[ "$bump" == "none" ]]; then
				bump="patch"
			fi
		fi
	done
fi

IFS=. read -r major minor patch <<< "$base_version"
major="${major:-0}"
minor="${minor:-0}"
patch="${patch:-0}"

if ! [[ "$major" =~ ^[0-9]+$ && "$minor" =~ ^[0-9]+$ && "$patch" =~ ^[0-9]+$ ]]; then
	echo "Invalid latest version: ${base_version}" >&2
	exit 1
fi

case "$bump" in
major)
	major=$((major + 1))
	minor=0
	patch=0
	;;
minor)
	minor=$((minor + 1))
	patch=0
	;;
patch)
	patch=$((patch + 1))
	;;
none)
	echo "should_release=false"
	echo "bump=none"
	echo "previous_tag=${latest_tag}"
	echo "previous_version=${base_version}"
	echo "version="
	echo "tag="
	echo "No conventional release commit found since ${latest_tag:-repository start}." >&2
	exit 0
	;;
*)
	echo "Unknown bump: ${bump}" >&2
	exit 1
	;;
esac

version="${major}.${minor}.${patch}"
tag="v${version}"

echo "should_release=true"
echo "bump=${bump}"
echo "previous_tag=${latest_tag}"
echo "previous_version=${base_version}"
echo "version=${version}"
echo "tag=${tag}"
echo "Next version: ${tag} (${bump} from ${latest_tag:-v0.0.0})." >&2
