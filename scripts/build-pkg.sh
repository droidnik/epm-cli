#!/bin/sh
# Build a macOS installer package from the universal binary.
#
#   scripts/build-pkg.sh <binary> <version> <outdir>
#
# Installs:
#   /usr/local/bin/epm-cli
#   /usr/local/share/zsh/site-functions/_epm-cli
#   /usr/local/share/bash-completion/completions/epm-cli
#   /usr/local/share/fish/vendor_completions.d/epm-cli.fish
#
# Optional signing/notarization (all skipped when the variables are unset):
#   CODESIGN_IDENTITY   "Developer ID Application: …"  signs the binary (hardened runtime)
#   PKG_SIGN_IDENTITY   "Developer ID Installer: …"    signs the package
#   NOTARY_PROFILE      notarytool keychain profile    notarizes + staples the package
#     or NOTARY_KEY_PATH + NOTARY_KEY_ID + NOTARY_ISSUER (App Store Connect API key)
#   PKG_IDENTIFIER      package identifier (default io.github.droidnik.epm-cli)
set -eu

BIN="${1:?binary path}"
VERSION="${2:?version}"
OUT="${3:-dist}"
IDENTIFIER="${PKG_IDENTIFIER:-io.github.droidnik.epm-cli}"

if [ "$(uname -s)" != "Darwin" ]; then
  echo "build-pkg: not on macOS, skipping .pkg" >&2
  exit 0
fi

if [ -n "${CODESIGN_IDENTITY:-}" ]; then
  echo "build-pkg: signing binary with $CODESIGN_IDENTITY" >&2
  codesign --force --options runtime --timestamp --sign "$CODESIGN_IDENTITY" "$BIN"
fi

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
ROOT="$WORK/root"
mkdir -p "$ROOT/usr/local/bin" \
         "$ROOT/usr/local/share/zsh/site-functions" \
         "$ROOT/usr/local/share/bash-completion/completions" \
         "$ROOT/usr/local/share/fish/vendor_completions.d"
install -m 0755 "$BIN" "$ROOT/usr/local/bin/epm-cli"
"$BIN" completion zsh  > "$ROOT/usr/local/share/zsh/site-functions/_epm-cli"
"$BIN" completion bash > "$ROOT/usr/local/share/bash-completion/completions/epm-cli"
"$BIN" completion fish > "$ROOT/usr/local/share/fish/vendor_completions.d/epm-cli.fish"
chmod 0644 "$ROOT"/usr/local/share/*/*/* "$ROOT"/usr/local/share/zsh/site-functions/_epm-cli

mkdir -p "$OUT"
PKG="$OUT/epm-cli_${VERSION}_darwin_all.pkg"
set -- --root "$ROOT" --identifier "$IDENTIFIER" --version "$VERSION" --install-location / \
       --ownership recommended
if [ -n "${PKG_SIGN_IDENTITY:-}" ]; then
  set -- "$@" --sign "$PKG_SIGN_IDENTITY" --timestamp
fi
pkgbuild "$@" "$PKG" >&2

if [ -n "${NOTARY_PROFILE:-}" ]; then
  xcrun notarytool submit "$PKG" --keychain-profile "$NOTARY_PROFILE" --wait >&2
  xcrun stapler staple "$PKG" >&2
elif [ -n "${NOTARY_KEY_PATH:-}" ]; then
  xcrun notarytool submit "$PKG" --key "$NOTARY_KEY_PATH" --key-id "${NOTARY_KEY_ID:?}" \
    --issuer "${NOTARY_ISSUER:?}" --wait >&2
  xcrun stapler staple "$PKG" >&2
fi
echo "build-pkg: wrote $PKG" >&2
