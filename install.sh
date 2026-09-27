#!/bin/sh
# Installs woffux (https://github.com/ngavilan-dogfy/woffux) step by step:
# finds the right build for your computer, checks it, puts it on your PATH
# and offers the guided setup.
#
#   curl -fsSL https://raw.githubusercontent.com/ngavilan-dogfy/woffux/main/install.sh | sh
#
# Options (environment variables):
#   WOFFUX_INSTALL_DIR=~/bin   where to put woffux (default: where it already
#                              is, otherwise ~/.local/bin)
#   WOFFUX_VERSION=v5.14.0     a specific release (default: the latest)
#   WOFFUX_NO_SETUP=1          don't offer to run 'woffux setup' at the end
#   WOFFUX_NO_SKILL=1          don't offer the Claude Code skill
#   WOFFUX_NO_MODIFY_PATH=1    never touch your shell's config file
#
# Nothing here needs sudo, unless woffux is already installed in a folder
# only an administrator can write to; then it asks first. Run it again any
# time to update: your settings are never touched.

set -eu

REPO="ngavilan-dogfy/woffux"
RELEASES="${WOFFUX_RELEASES_URL:-https://github.com/$REPO/releases}"
TMP=""

# ─── output ──────────────────────────────────────────────────────

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
	B=$(printf '\033[1m') DIM=$(printf '\033[90m') OK=$(printf '\033[32m')
	WARN=$(printf '\033[33m') ERR=$(printf '\033[31m') ACC=$(printf '\033[35m') R=$(printf '\033[0m')
else
	B="" DIM="" OK="" WARN="" ERR="" ACC="" R=""
fi

say()  { printf '  %s\n' "$*"; }
ok()   { printf '  %s●%s %s\n' "$OK" "$R" "$*"; }
info() { printf '  %s·%s %s\n' "$DIM" "$R" "$*"; }
warn() { printf '  %s!%s %s\n' "$WARN" "$R" "$*"; }
hint() { printf '    %s%s%s\n' "$DIM" "$*" "$R"; }
fail() {
	printf '  %s×%s %s\n' "$ERR" "$R" "$1" >&2
	shift
	for line in "$@"; do printf '    %s\n' "$line" >&2; done
	printf '\n  %sStuck? Open an issue: https://github.com/%s/issues%s\n\n' "$DIM" "$REPO" "$R" >&2
	exit 1
}

cleanup() { if [ -n "$TMP" ]; then rm -rf "$TMP"; fi; }
trap cleanup EXIT
trap 'cleanup; printf "\n"; exit 130' INT TERM

# tildify shows paths under your home as ~/…
tildify() {
	# shellcheck disable=SC2088 # a literal ~ for display
	case "$1" in
	"$HOME"/*) printf '~/%s' "${1#"$HOME"/}" ;;
	*) printf '%s' "$1" ;;
	esac
}

# can_ask: is there a person at a terminal to answer questions? With
# 'curl | sh' stdin is the script itself, so questions go to /dev/tty.
can_ask() { [ -t 1 ] && (: </dev/tty) 2>/dev/null; }

# ask QUESTION → yes (0) / no (1); Enter means yes.
ask() {
	printf '  %s?%s %s %s[Y/n]%s ' "$ACC" "$R" "$1" "$DIM" "$R"
	read -r answer </dev/tty || answer=n
	case "$answer" in [nN]*) return 1 ;; *) return 0 ;; esac
}

# version_of BINARY → "v5.14.0", or nothing when it doesn't run.
version_of() { "$1" --version 2>/dev/null | sed -n 's/^woffux version //p' | cut -d' ' -f1; }

# ─── what computer is this? ──────────────────────────────────────

detect_platform() {
	case "$(uname -s)" in
	Darwin) OS=darwin OS_NAME=macOS ;;
	Linux) OS=linux OS_NAME=Linux ;;
	*) fail "woffux runs on macOS and Linux, not on $(uname -s)." ;;
	esac
	case "$(uname -m)" in
	x86_64 | amd64) ARCH=amd64 ;;
	arm64 | aarch64) ARCH=arm64 ;;
	*) fail "Unsupported processor: $(uname -m)" "Build it from source: go install github.com/$REPO/cmd/woffux@latest" ;;
	esac
	# A shell running under Rosetta reports x86_64 on Apple Silicon.
	if [ "$OS" = darwin ] && [ "$ARCH" = amd64 ] && [ "$(sysctl -n hw.optional.arm64 2>/dev/null || true)" = 1 ]; then
		ARCH=arm64
	fi
	case "$OS/$ARCH" in
	darwin/arm64) CPU="Apple Silicon" ;;
	darwin/amd64) CPU="Intel" ;;
	*) CPU="$ARCH" ;;
	esac
}

# ─── where does it go? ───────────────────────────────────────────

# choose_dir: WOFFUX_INSTALL_DIR, else the folder of the woffux you already
# have (so this updates it in place, and this Mac's signer keeps pointing
# at it), else ~/.local/bin. SUDO is set when that folder needs it.
choose_dir() {
	SUDO=""
	if [ -n "${WOFFUX_INSTALL_DIR:-}" ]; then
		INSTALL_DIR="$WOFFUX_INSTALL_DIR"
		return
	fi
	INSTALL_DIR="$HOME/.local/bin"
	found=$(command -v woffux 2>/dev/null || true)
	case "$found" in
	/*) ;;
	*) return ;;
	esac
	dir=$(dirname "$found")
	if [ -w "$dir" ]; then
		INSTALL_DIR="$dir"
		return
	fi
	info "You have woffux in $dir, which only an administrator can change."
	if can_ask && ask "Update it there? (asks for your password)"; then
		INSTALL_DIR="$dir"
		SUDO="sudo"
	else
		info "Installing to $(tildify "$INSTALL_DIR") instead."
	fi
}

# ─── downloads ───────────────────────────────────────────────────

if command -v curl >/dev/null 2>&1; then
	fetch() { curl -fsSL --retry 2 -o "$2" "$1"; }
	final_url() { curl -fsSLI -o /dev/null -w '%{url_effective}' "$1"; }
elif command -v wget >/dev/null 2>&1; then
	fetch() { wget -q -O "$2" "$1"; }
	final_url() { wget -q -S --spider "$1" 2>&1 | sed -n 's/^ *[Ll]ocation: *//p' | tail -n 1 | tr -d '\r'; }
else
	fail "Neither curl nor wget is installed." "Install one of them (e.g. sudo apt install curl) and run this again."
fi

sha256_of() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d' ' -f1
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | cut -d' ' -f1
	else
		openssl dgst -sha256 "$1" | sed 's/.*= //'
	fi
}

# latest_tag resolves "latest" through the releases page redirect
# (…/releases/latest → …/releases/tag/v1.2.3), which needs no API token.
latest_tag() {
	url=$(final_url "$RELEASES/latest" 2>/dev/null || true)
	case "$url" in
	*/tag/*) printf '%s' "${url##*/tag/}" ;;
	*) printf '' ;;
	esac
}

# place FILE: move the verified binary into INSTALL_DIR in one step, so a
# woffux that is running (this Mac's signer) is never half-written.
place() {
	$SUDO mkdir -p "$INSTALL_DIR" 2>/dev/null ||
		fail "Can't create $(tildify "$INSTALL_DIR")." "Choose another folder: WOFFUX_INSTALL_DIR=~/bin sh install.sh"
	[ -n "$SUDO" ] || [ -w "$INSTALL_DIR" ] ||
		fail "Can't write to $(tildify "$INSTALL_DIR")." "Choose another folder: WOFFUX_INSTALL_DIR=~/bin sh install.sh"
	if ! { $SUDO cp "$1" "$INSTALL_DIR/.woffux.new" && $SUDO mv -f "$INSTALL_DIR/.woffux.new" "$INSTALL_DIR/woffux"; }; then
		fail "Couldn't install to $(tildify "$INSTALL_DIR")."
	fi
}

# ─── fallback: build from source ─────────────────────────────────

build_from_source() {
	info "$1"
	if ! command -v go >/dev/null 2>&1; then
		fail "No ready-made build to download, and Go isn't installed to build one." \
			"Install Go from https://go.dev/dl/ and run this installer again."
	fi
	say "Building from source with $(go version | cut -d' ' -f3) (takes a minute)…"
	GOBIN="$TMP" go install "github.com/$REPO/cmd/woffux@${WOFFUX_VERSION:-latest}" ||
		fail "The build failed." "Check the error above; your Go may be too old (woffux needs Go 1.25+)."
	place "$TMP/woffux"
	ok "Built and installed $(tildify "$INSTALL_DIR/woffux")"
}

# ─── PATH ────────────────────────────────────────────────────────

on_path() {
	case ":$PATH:" in *":$INSTALL_DIR:"*) return 0 ;; *) return 1 ;; esac
}

# shell_rc: the file your shell reads at startup, and the line to add.
shell_rc() {
	dir="$INSTALL_DIR"
	case "$dir" in "$HOME"/*) dir="\$HOME/${dir#"$HOME"/}" ;; esac
	LINE="export PATH=\"$dir:\$PATH\""
	case "$(basename "${SHELL:-sh}")" in
	zsh) RC="${ZDOTDIR:-$HOME}/.zshrc" ;;
	bash) if [ "$OS" = darwin ]; then RC="$HOME/.bash_profile"; else RC="$HOME/.bashrc"; fi ;;
	fish)
		RC="$HOME/.config/fish/config.fish"
		LINE="fish_add_path \"$dir\""
		;;
	*) RC="$HOME/.profile" ;;
	esac
}

fix_path() {
	shell_rc
	warn "$(tildify "$INSTALL_DIR") isn't in your PATH yet, so typing 'woffux' won't find it."
	if [ -z "${WOFFUX_NO_MODIFY_PATH:-}" ] && can_ask && ask "Add it for you? (one line at the end of $(tildify "$RC"))"; then
		mkdir -p "$(dirname "$RC")"
		printf '\n# Added by the woffux installer\n%s\n' "$LINE" >>"$RC"
		ok "Added to $(tildify "$RC") — new terminals will find woffux"
		PATH_CHANGED=1
	else
		hint "Add this line to $(tildify "$RC") and open a new terminal:"
		hint "  $LINE"
	fi
}

# other_copies: every other woffux in your PATH (older installs).
other_copies() {
	old_ifs=$IFS
	IFS=:
	for d in $PATH; do
		[ -n "$d" ] && [ "$d" != "$INSTALL_DIR" ] && [ -x "$d/woffux" ] && [ ! -d "$d/woffux" ] && printf '%s\n' "$d/woffux"
	done | awk '!seen[$0]++'
	IFS=$old_ifs
}

# ─── this Mac's signer ───────────────────────────────────────────

# agent_binary: the woffux this Mac's signer (launchd) runs, if it's on.
agent_binary() {
	plist="$HOME/Library/LaunchAgents/dev.woffux.agent.plist"
	[ "$OS" = darwin ] && [ -f "$plist" ] || return 0
	sed -n '/ProgramArguments/,/<\/array>/p' "$plist" | sed -n 's:.*<string>\(.*\)</string>.*:\1:p' | head -n 1
}

# follow_agent keeps this Mac's signer on the woffux just installed. It runs
# the binary at its path, so an in-place update needs nothing but a fresh
# plist; one pointing at another copy is switched after asking.
follow_agent() {
	current=$(agent_binary)
	[ -n "$current" ] || return 0
	new="$INSTALL_DIR/woffux"
	if [ "$current" = "$new" ]; then
		"$new" agent on >/dev/null 2>&1 && ok "This Mac's signer runs the new version"
		return 0
	fi
	have=$(version_of "$current" || true)
	warn "This Mac's signer runs another woffux: $(tildify "$current")${have:+ ($have)}"
	if can_ask && ask "Switch it to the one just installed?"; then
		if "$new" agent on >/dev/null 2>&1; then
			ok "This Mac's signer now runs $(tildify "$new")"
		else
			warn "Couldn't switch it"
			hint "Run: woffux agent on"
		fi
	else
		hint "To switch it later: $(tildify "$new") agent on"
	fi
}

# configured: woffux already has your settings.
configured() { [ -f "$HOME/.woffux.yaml" ]; }

# ─── main ────────────────────────────────────────────────────────

main() {
	printf '\n  %s◆ woffux%s installer\n\n' "$ACC$B" "$R"

	detect_platform
	ok "Your computer: $OS_NAME · $CPU"
	choose_dir

	previous=""
	if [ -x "$INSTALL_DIR/woffux" ]; then
		previous=$(version_of "$INSTALL_DIR/woffux" || true)
	fi

	TMP=$(mktemp -d 2>/dev/null || mktemp -d -t woffux-install)
	version="${WOFFUX_VERSION:-}"
	if [ -z "$version" ]; then
		version=$(latest_tag)
	fi

	if [ -z "$version" ]; then
		build_from_source "No release could be found."
	else
		if [ -n "${WOFFUX_VERSION:-}" ]; then ok "Release: $version"; else ok "Latest release: $version"; fi
		asset="woffux-${OS}-${ARCH}"
		if ! fetch "$RELEASES/download/$version/$asset" "$TMP/$asset" 2>/dev/null; then
			[ -z "${WOFFUX_VERSION:-}" ] || fail "There's no release $version for $OS_NAME ($CPU)." "See which ones exist: $RELEASES"
			build_from_source "Release $version can't be downloaded right now."
		elif ! fetch "$RELEASES/download/$version/checksums.txt" "$TMP/checksums.txt" 2>/dev/null; then
			fail "Release $version has no checksums.txt: it's older than verified installs." \
				"Install the latest release instead, or download it by hand from $RELEASES/tag/$version"
		else
			size=$(wc -c <"$TMP/$asset" | tr -d ' ')
			ok "Downloaded $asset ($((size / 1024 / 1024)).$((size / 1024 % 1024 * 10 / 1024)) MB)"

			want=$(awk -v f="$asset" '$2 == f || $2 == "*" f { print $1 }' "$TMP/checksums.txt")
			got=$(sha256_of "$TMP/$asset")
			[ -n "$want" ] || fail "$asset isn't listed in checksums.txt." "Not installing an unverified file."
			[ "$want" = "$got" ] || fail "Checksum mismatch: the download is corrupt or was tampered with." \
				"Nothing was installed. Try again; if it keeps happening, open an issue."
			ok "Checksum verified"

			mv "$TMP/$asset" "$TMP/woffux"
			chmod 755 "$TMP/woffux"
			"$TMP/woffux" --version >/dev/null 2>&1 ||
				fail "The downloaded woffux doesn't run on this computer." "Nothing was installed."
			[ -z "$SUDO" ] || info "Your password is needed to write to $INSTALL_DIR."
			place "$TMP/woffux"
			# macOS: files from the internet get quarantined; this one was verified.
			if [ "$OS" = darwin ]; then $SUDO xattr -d com.apple.quarantine "$INSTALL_DIR/woffux" 2>/dev/null || true; fi
			ok "Installed $(tildify "$INSTALL_DIR/woffux")"
		fi
	fi

	now=$(version_of "$INSTALL_DIR/woffux" || true)
	[ -n "$now" ] || fail "woffux was installed but doesn't start." "Run $(tildify "$INSTALL_DIR/woffux") --version to see why."
	if [ -n "$previous" ] && [ "$previous" != "$now" ]; then
		ok "Updated from $previous to $now"
	fi

	follow_agent

	# The Claude Code skill ships inside the binary: refresh it if it's there.
	if [ -f "$HOME/.claude/skills/woffux/SKILL.md" ] && "$INSTALL_DIR/woffux" skill install --quiet >/dev/null 2>&1; then
		ok "Claude Code skill refreshed"
	fi

	PATH_CHANGED=""
	if ! on_path; then
		fix_path
	fi

	# Another woffux earlier in PATH would shadow this one; later ones are
	# just leftovers.
	first=$(command -v woffux 2>/dev/null || true)
	if on_path && [ -n "$first" ] && [ "$first" != "$INSTALL_DIR/woffux" ]; then
		warn "Typing 'woffux' runs another copy: $(tildify "$first")"
		hint "Remove it, or put $(tildify "$INSTALL_DIR") first in your PATH."
	fi
	signer=$(agent_binary)
	other_copies | while IFS= read -r copy; do
		[ "$copy" = "$first" ] && continue
		have=$(version_of "$copy" || true)
		if [ "$copy" = "$signer" ]; then
			info "This Mac's signer still runs $(tildify "$copy")${have:+ ($have)}."
		else
			rm_cmd="rm"
			[ -w "$(dirname "$copy")" ] || rm_cmd="sudo rm"
			info "An older copy is still at $(tildify "$copy")${have:+ ($have)}; nothing runs it. Remove it with: $rm_cmd $(tildify "$copy")"
		fi
	done

	printf '\n'
	# Claude Code users get the skill offered: it teaches Claude this CLI.
	if [ ! -f "$HOME/.claude/skills/woffux/SKILL.md" ] && { [ -d "$HOME/.claude" ] || command -v claude >/dev/null 2>&1; }; then
		if [ -z "${WOFFUX_NO_SKILL:-}" ] && can_ask && ask "You use Claude Code: teach it woffux? (installs the /woffux skill)"; then
			if "$INSTALL_DIR/woffux" skill install --quiet >/dev/null 2>&1; then
				ok "Claude Code skill installed: ask Claude whether you've signed, or to request a day off"
			else
				warn "Couldn't install the Claude Code skill"
				hint "Run: woffux skill install"
			fi
			printf '\n'
		else
			info "Using Claude Code? ${B}woffux skill install${R} teaches it woffux (/woffux)."
			printf '\n'
		fi
	fi
	if configured; then
		say "${OK}Done.${R} Your settings are untouched. ${DIM}What's new: $RELEASES${R}"
		say "${DIM}Check everything with: woffux doctor · later updates: woffux update${R}"
		printf '\n'
		return
	fi
	say "${B}Next: tell woffux your week${R} — about three minutes, and nothing is signed."
	if [ -z "${WOFFUX_NO_SETUP:-}" ] && can_ask && ask "Run 'woffux setup' now?"; then
		printf '\n'
		"$INSTALL_DIR/woffux" setup </dev/tty || true
		if ! on_path; then
			printf '\n'
			info "Open a new terminal to use 'woffux' (this one doesn't know the new PATH yet)."
		fi
	elif on_path; then
		hint "Run: woffux setup"
	elif [ -n "$PATH_CHANGED" ]; then
		hint "Open a new terminal and run: woffux setup"
	else
		hint "Once PATH is set, run: woffux setup   (or right now: $(tildify "$INSTALL_DIR/woffux") setup)"
	fi
	printf '\n'
}

main "$@"
