#!/bin/sh
set -e

REPO="ngavilan-dogfy/woffux"
INSTALL_DIR="/usr/local/bin"

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
CYAN='\033[0;36m'
BOLD='\033[1m'
DIM='\033[2m'
NC='\033[0m'

# Detect OS and architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
  arm64)   ARCH="arm64" ;;
  *)
    printf "  ${RED}✗${NC} Unsupported architecture: ${BOLD}$ARCH${NC}\n"
    exit 1
    ;;
esac

case "$OS" in
  darwin|linux) ;;
  *)
    printf "  ${RED}✗${NC} Unsupported OS: ${BOLD}$OS${NC}\n"
    exit 1
    ;;
esac

BINARY="woffux-${OS}-${ARCH}"

# Get latest release
printf "  ${DIM}Finding latest release...${NC}\n"
# Resolve the tag from the public redirect (no API rate limit), falling
# back to the API.
TAG=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest" 2>/dev/null | sed -n 's#.*/tag/##p')
if [ -z "$TAG" ]; then
  TAG=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null | grep '"tag_name"' | head -1 | cut -d'"' -f4)
fi

if [ -z "$TAG" ]; then
  printf "  ${RED}✗${NC} Could not find latest release.\n"
  printf "  ${DIM}Check https://github.com/${REPO}/releases${NC}\n"
  exit 1
fi

URL="https://github.com/${REPO}/releases/download/${TAG}/${BINARY}"

printf "\n"
printf "  ${BOLD}woffux ${TAG}${NC}\n"
printf "  ${DIM}${OS}/${ARCH}${NC}\n"
printf "\n"
printf "  ${DIM}Downloading from GitHub...${NC}\n"

HTTP_CODE=$(curl -fsSL -w "%{http_code}" "$URL" -o /tmp/woffux 2>/dev/null || echo "000")

if [ "$HTTP_CODE" != "200" ] && [ ! -s /tmp/woffux ]; then
  printf "  ${RED}✗${NC} Download failed (HTTP ${HTTP_CODE})\n"
  printf "  ${DIM}Binary '${BINARY}' not found in release ${TAG}${NC}\n"
  printf "  ${DIM}Download manually: https://github.com/${REPO}/releases/tag/${TAG}${NC}\n"
  exit 1
fi

chmod +x /tmp/woffux

# Verify it's actually an executable
if ! file /tmp/woffux | grep -q "executable\|Mach-O"; then
  printf "  ${RED}✗${NC} Downloaded file is not a valid binary.\n"
  printf "  ${DIM}This can happen if the release is still being built.${NC}\n"
  printf "  ${DIM}Try again in a few minutes or download manually:${NC}\n"
  printf "  ${DIM}https://github.com/${REPO}/releases/tag/${TAG}${NC}\n"
  rm -f /tmp/woffux
  exit 1
fi

printf "  ${DIM}Installing to ${INSTALL_DIR}/woffux...${NC}\n"

if [ -w "$INSTALL_DIR" ]; then
  mv /tmp/woffux "${INSTALL_DIR}/woffux"
else
  sudo mv /tmp/woffux "${INSTALL_DIR}/woffux"
fi

printf "\n"
printf "  ${GREEN}✓${NC} ${BOLD}woffux ${TAG}${NC} installed\n"
printf "  ${DIM}${INSTALL_DIR}/woffux${NC}\n"
printf "\n"
printf "  Next: run ${BOLD}woffux${NC} — a short guided setup starts the first time.\n"
printf "\n"
