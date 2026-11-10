#!/bin/sh
# epm-cli installer — downloads a release binary from GitHub, verifies its
# SHA-256 checksum and installs it. No Go toolchain needed.
#
# No login is needed for a public repository. For a private fork, either log in
# with the GitHub CLI (`gh auth login`) or set GH_TOKEN / GITHUB_TOKEN.
#
# Usage:
#   sh install.sh                      # latest release
#   sh install.sh -v v0.3.0            # specific version
#   sh install.sh -d ~/bin             # install directory
#   sh install.sh -f epm-cli_0.3.0_darwin_all.tar.gz   # local archive (offline)
#
# Without a clone:
#   curl -fsSL https://raw.githubusercontent.com/droidnik/epm-cli/main/install.sh | sh
set -eu

REPO="${EPM_CLI_REPO:-droidnik/epm-cli}"
VERSION="${EPM_CLI_VERSION:-latest}"
INSTALL_DIR="${EPM_CLI_INSTALL_DIR:-}"
ARCHIVE=""

say() { printf '%s\n' "$*" >&2; }
die() { say "error: $*"; exit 1; }

while [ $# -gt 0 ]; do
  case "$1" in
    -v|--version) VERSION="${2:?missing value}"; shift 2 ;;
    -d|--dir) INSTALL_DIR="${2:?missing value}"; shift 2 ;;
    -f|--file) ARCHIVE="${2:?missing value}"; shift 2 ;;
    -r|--repo) REPO="${2:?missing value}"; shift 2 ;;
    -h|--help) sed -n '2,17p' "$0" 2>/dev/null | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) die "unknown option: $1" ;;
  esac
done

# ---------------------------------------------------------------- platform
case "$(uname -s)" in
  Darwin) OS=darwin; ARCH=all ;;   # universal binary
  Linux)
    OS=linux
    case "$(uname -m)" in
      x86_64|amd64) ARCH=amd64 ;;
      aarch64|arm64) ARCH=arm64 ;;
      *) die "unsupported architecture: $(uname -m)" ;;
    esac ;;
  *) die "unsupported OS: $(uname -s) (on Windows download the .zip from the release page)" ;;
esac

TMP="$(mktemp -d 2>/dev/null || mktemp -d -t epm-cli)"
trap 'rm -rf "$TMP"' EXIT INT TERM

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{print $1}'
  else shasum -a 256 "$1" | awk '{print $1}'; fi
}

# ---------------------------------------------------------------- download
if [ -n "$ARCHIVE" ]; then
  [ -f "$ARCHIVE" ] || die "archive not found: $ARCHIVE"
  cp "$ARCHIVE" "$TMP/"
  NAME="$(basename "$ARCHIVE")"
  SUMS="$(dirname "$ARCHIVE")/checksums.txt"
  [ -f "$SUMS" ] && cp "$SUMS" "$TMP/checksums.txt"
else
  PATTERN="epm-cli_*_${OS}_${ARCH}.tar.gz"
  if command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then
    say "Downloading $PATTERN ($VERSION) from $REPO via gh …"
    TAG=""; [ "$VERSION" != "latest" ] && TAG="$VERSION"
    # shellcheck disable=SC2086
    gh release download $TAG -R "$REPO" -p "$PATTERN" -p checksums.txt -D "$TMP" \
      || die "download failed — do you have access to $REPO and does release '$VERSION' exist?"
  else
    TOKEN="${GH_TOKEN:-${GITHUB_TOKEN:-}}"
    command -v curl >/dev/null 2>&1 || die "curl is required"
    API="https://api.github.com/repos/$REPO/releases"
    if [ "$VERSION" = "latest" ]; then REL="$API/latest"; else REL="$API/tags/$VERSION"; fi
    say "Downloading $PATTERN ($VERSION) from $REPO via the GitHub API …"
    # The Authorization header is only sent when a token is set (not needed for public repos).
    gh_curl() { if [ -n "$TOKEN" ]; then curl -fsSL -H "Authorization: Bearer $TOKEN" "$@"; else curl -fsSL "$@"; fi; }
    gh_curl -H "Accept: application/vnd.github+json" "$REL" -o "$TMP/release.json" \
      || die "cannot read release '$VERSION' of $REPO (does it exist? for a private repository set GH_TOKEN or run 'gh auth login')"
    # Each asset object has "url": ".../releases/assets/<id>" followed by "name": "<file>".
    assets="$(tr ',{}' '\n\n\n' < "$TMP/release.json" |
      awk -F'"' '/"url": ?".*\/releases\/assets\//{u=$4} /"name": ?"/{if(u!=""){print u" "$4; u=""}}')"
    get_asset() {
      url="$(printf '%s\n' "$assets" | awk -v pat="$1" '{ if ($2 ~ pat) { print $1; exit } }')"
      name="$(printf '%s\n' "$assets" | awk -v pat="$1" '{ if ($2 ~ pat) { print $2; exit } }')"
      [ -n "$url" ] || die "release has no asset matching $1"
      gh_curl -H "Accept: application/octet-stream" "$url" -o "$TMP/$name" \
        || die "download of $name failed"
    }
    get_asset "^epm-cli_.*_${OS}_${ARCH}\\.tar\\.gz$"
    get_asset "^checksums\\.txt$"
  fi
  NAME="$(cd "$TMP" && ls epm-cli_*_"${OS}"_"${ARCH}".tar.gz 2>/dev/null | head -n 1)"
  [ -n "$NAME" ] || die "no matching archive downloaded"
fi

# ---------------------------------------------------------------- verify
if [ -f "$TMP/checksums.txt" ]; then
  want="$(awk -v n="$NAME" '$2 == n {print $1}' "$TMP/checksums.txt")"
  [ -n "$want" ] || die "$NAME is not listed in checksums.txt"
  got="$(sha256 "$TMP/$NAME")"
  [ "$want" = "$got" ] || die "checksum mismatch for $NAME"
  say "Checksum OK"
elif [ -z "$ARCHIVE" ]; then
  die "checksums.txt missing from release"
else
  say "warning: no checksums.txt next to $ARCHIVE — skipping verification"
fi

tar -xzf "$TMP/$NAME" -C "$TMP"
[ -f "$TMP/epm-cli" ] || die "archive does not contain epm-cli"

# ---------------------------------------------------------------- install
if [ -z "$INSTALL_DIR" ]; then
  if [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then INSTALL_DIR=/usr/local/bin
  else INSTALL_DIR="$HOME/.local/bin"; fi
fi
mkdir -p "$INSTALL_DIR" 2>/dev/null || true
if [ -w "$INSTALL_DIR" ]; then
  install -m 0755 "$TMP/epm-cli" "$INSTALL_DIR/epm-cli"
else
  say "$INSTALL_DIR is not writable — using sudo"
  sudo mkdir -p "$INSTALL_DIR"
  sudo install -m 0755 "$TMP/epm-cli" "$INSTALL_DIR/epm-cli"
fi
if [ "$OS" = darwin ]; then
  xattr -d com.apple.quarantine "$INSTALL_DIR/epm-cli" 2>/dev/null || true
fi

say "Installed $("$INSTALL_DIR/epm-cli" --version) → $INSTALL_DIR/epm-cli"
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) say ""
     say "Note: $INSTALL_DIR is not in your PATH. Add it, e.g. for zsh:"
     say "  echo 'export PATH=\"$INSTALL_DIR:\$PATH\"' >> ~/.zshrc && exec zsh" ;;
esac
say "Next: epm-cli config add <name>   (see epm-cli --help)"
