#!/bin/sh
# Re-records assets/setup.gif: the e2e build of woffux pointed at a tiny fake
# Woffu (scripts/record/fakewoffu.py) for a made-up person, in a throwaway
# HOME whose git config keeps a personal and a work identity. The e2e build
# keeps the password in memory and never installs a real launchd agent.
# Needs vhs.
#
#   make e2e && scripts/record-setup.sh
set -eu

root="$(cd "$(dirname "$0")/.." && pwd)"
[ -x "$root/bin/woffux-e2e" ] || { echo "build it first: make e2e" >&2; exit 1; }
command -v vhs >/dev/null 2>&1 || { echo "needs vhs: brew install vhs" >&2; exit 1; }

port=18432
work="$(mktemp -d)"
mkdir -p "$work/home/.claude" "$work/bin"
printf '[user]\n\temail = ana@users.noreply.github.com\n[includeIf "gitdir:~/work/"]\n\tpath = ~/.gitconfig-work\n' > "$work/home/.gitconfig"
printf '[user]\n\temail = ana@acme.com\n' > "$work/home/.gitconfig-work"
ln -s "$root/bin/woffux-e2e" "$work/bin/woffux"
: > "$work/open.log"
sed -e "s|@R@|$work|g" -e "s|@PORT@|$port|g" "$root/scripts/record/setup.tape.in" > "$work/setup.tape"

python3 "$root/scripts/record/fakewoffu.py" "$port" &
fake=$!
trap 'kill "$fake" 2>/dev/null; wait "$fake" 2>/dev/null; rm -rf "$work"' EXIT
sleep 1
(cd "$work" && vhs setup.tape)
cp "$work/setup.gif" "$root/assets/setup.gif"
echo "assets/setup.gif updated: look at it before committing"
