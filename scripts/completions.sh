#!/bin/sh
# Generate shell completion files into ./completions (used by GoReleaser for
# archives, the macOS .pkg and the Linux packages).
set -eu
rm -rf completions && mkdir -p completions
for sh in bash zsh fish; do
  go run ./cmd/epm-cli completion "$sh" > "completions/epm-cli.$sh"
done
