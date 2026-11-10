#!/bin/sh
# Generate THIRD_PARTY_NOTICES from the licenses of all Go modules that are
# compiled into the binary. Run from the repository root (also done by GoReleaser).
set -eu
OUT="${1:-THIRD_PARTY_NOTICES}"
{
  echo "THIRD-PARTY NOTICES"
  echo "==================="
  echo
  echo "epm-cli itself is licensed under the MIT License (see LICENSE). It includes the"
  echo "following third-party Go modules, each under its own license:"
  # Union over all release targets: some modules (e.g. mousetrap) are Windows-only.
  for os in darwin linux windows; do
    GOOS=$os GOARCH=amd64 go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}} {{.Dir}}{{end}}{{end}}' ./cmd/epm-cli
  done |
    sort -u | while read -r path version dir; do
      echo
      echo "--------------------------------------------------------------------------------"
      echo "$path $version"
      echo "--------------------------------------------------------------------------------"
      f="$(ls "$dir"/LICENSE* "$dir"/COPYING* "$dir"/License* 2>/dev/null | head -n 1 || true)"
      if [ -n "$f" ]; then cat "$f"; else echo "(no license file found in module)"; fi
    done
} > "$OUT"
echo "notices: wrote $OUT" >&2
