# woffux — build and install from source.
#
#   make install        build and copy to ~/.local/bin/woffux (override with PREFIX=...)
#   make build          build into bin/woffux
#   make check          gofmt + vet + test + build: run it before committing
#   make preview        render every dashboard screen with fake data into previews/
#   make release        every release binary + checksums.txt into dist/
#
# Most people don't need this: the README has the one-line installer.
# Releases are cut by CI from conventional commits (see CONTRIBUTING.md).

BINARY   := woffux
MODULE   := github.com/ngavilan-dogfy/woffux
BUILD    := bin/$(BINARY)
PREFIX   ?= $(HOME)/.local
BINDIR   := $(PREFIX)/bin
# A release version only on a release tag; anything else is a dev build,
# which never nags about updates and reports its commit instead.
VERSION  ?= $(shell git describe --tags --exact-match --match 'v[0-9]*' 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X $(MODULE)/cmd.Version=$(VERSION)

.PHONY: build install uninstall fmt test vet check preview release clean

build:
	@command -v go >/dev/null 2>&1 || { echo "Go is not installed: https://go.dev/dl (or: brew install go)"; exit 1; }
	@mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o $(BUILD) ./cmd/$(BINARY)

# Copy + rename, never overwrite in place: macOS kills a binary rewritten
# over its old inode ("killed: 9"), and a running woffux keeps working.
install: build
	@mkdir -p $(BINDIR)
	@cp $(BUILD) $(BINDIR)/.$(BINARY).new && mv -f $(BINDIR)/.$(BINARY).new $(BINDIR)/$(BINARY)
	@echo "installed → $(BINDIR)/$(BINARY) ($(VERSION))"
	@case ":$$PATH:" in *":$(BINDIR):"*) ;; *) \
		echo ""; \
		echo "! $(BINDIR) is not in your PATH, so 'woffux' won't be found yet."; \
		echo "  Add this line to your ~/.zshrc (or ~/.bashrc) and open a new terminal:"; \
		echo "    export PATH=\"$(BINDIR):\$$PATH\"";; esac

uninstall:
	rm -f $(BINDIR)/$(BINARY)
	@echo "removed $(BINDIR)/$(BINARY) — 'woffux uninstall' also stops signing and removes your settings"

fmt:
	@unformatted="$$(gofmt -l .)"; if [ -n "$$unformatted" ]; then echo "gofmt -w needed on:"; echo "$$unformatted"; exit 1; fi

test:
	go test ./...
	@if command -v shellcheck >/dev/null 2>&1; then shellcheck -s sh install.sh && shellcheck scripts/*.sh; fi

vet:
	go vet ./...

check: fmt vet test build

# The dashboard in every state (working, on a break, holiday, palette…)
# with made-up data, as ANSI files: cat previews/02-working.ans
preview:
	@mkdir -p previews
	WOFFUX_TUI_PREVIEW=$(CURDIR)/previews go test ./internal/tui -run TestPreview -count=1
	@echo "cat previews/*.ans"

release:
	scripts/build-release.sh $(VERSION)

clean:
	rm -rf bin/ dist/ previews/
