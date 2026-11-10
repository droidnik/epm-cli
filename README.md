# epm-cli

Command-line interface for **BeyondTrust Endpoint Privilege Management for Windows and Mac**
(EPM-WM / PM Cloud, Management API v3), modelled on
[jamf-cli](https://github.com/Jamf-Concepts/jamf-cli): a single binary with tenant profiles, Keychain
storage, several output formats, automatic paging, dry runs, confirmation for destructive actions,
backups and an MCP server for AI assistants.

All 94 operations of the v3 API are available as commands. They are generated from the OpenAPI
specification extracted from the [official API reference](https://docs.beyondtrust.com/epm-wm/reference/get_v3-accepteddomains).

```sh
epm-cli computers list --os Mac --days-disconnected 30 --all -o csv --out-file stale.csv
epm-cli policies content <POLICY-ID> --out-file policy.xml
epm-cli policies upload <POLICY-ID> --policy-file policy.xml --auto-assign-to-group
epm-cli admin-access-requests decide --request-id <ID> --decision Approved --duration 1800 …
epm-cli backup --dir ./epm-config
```

## Documentation

| | |
|---|---|
| [1. Getting started](docs/01-getting-started.md) | install, API account, connect a tenant, first commands |
| [2. Basics](docs/02-basics.md) | command structure, flags, output, filters, paging, request bodies, safety, exit codes |
| [3. Workflows](docs/03-workflows.md) | computers, groups, policies, JIT admin requests, events, users, backup |
| [4. Automation](docs/04-automation.md) | scripts, GitHub Actions, GitLab CI, Docker, Kubernetes, MCP |
| [5. Troubleshooting](docs/05-troubleshooting.md) | `doctor`, common errors, verbose logging |
| [Command reference](docs/reference/README.md) | every command with all flags, values and body templates (generated) |

## Installation

The repository is private: you need read access to `droidnik/epm-cli` and a logged-in GitHub CLI
(`gh auth login`) or a token in `GH_TOKEN`. No Go toolchain is required.

Every release contains:

| Platform | File | Contents |
|---|---|---|
| macOS | `epm-cli_<v>_darwin_all.pkg` | installer → `/usr/local/bin/epm-cli` + shell completions |
| macOS | `epm-cli_<v>_darwin_all.zip` / `.tar.gz` | universal binary (Intel + Apple Silicon) |
| Windows | `epm-cli_<v>_windows_amd64.zip` | `epm-cli.exe` (also runs on ARM through emulation) |
| Linux | `.deb`, `.rpm`, `.apk`, `.tar.gz` | amd64 and arm64 |
| Container | `ghcr.io/droidnik/epm-cli:<v>` | multi-arch image for CI/CD and Kubernetes |
| all | `checksums.txt`, `install.sh`, `install.ps1` | SHA-256 checksums, installer scripts |

**macOS / Linux**

```sh
gh api repos/droidnik/epm-cli/contents/install.sh -H "Accept: application/vnd.github.raw" | sh
```

The script downloads the right archive, verifies its checksum and installs to `/usr/local/bin` (if
writable) or `~/.local/bin`. Options: `-v v0.2.0` (version), `-d ~/bin` (directory), `-f <archive>`
(offline). Running it again updates the CLI.

**macOS package** (suitable for deployment with Jamf Pro)

```sh
gh release download -R droidnik/epm-cli -p '*darwin_all.pkg'
sudo installer -pkg epm-cli_*_darwin_all.pkg -target /
```

Unsigned packages install fine through Jamf or `installer`; only a double-click in Finder is blocked by
Gatekeeper. A ZIP downloaded in a browser is quarantined: run `xattr -d com.apple.quarantine epm-cli`
once (not needed for signed releases, see below).

**Windows** (PowerShell, no admin rights; installs to `%LOCALAPPDATA%\Programs\epm-cli` and updates the
user PATH)

```powershell
gh api repos/droidnik/epm-cli/contents/install.ps1 -H "Accept: application/vnd.github.raw" | powershell -NoProfile -Command -
# offline, e.g. packaged as an Intune Win32 app:
powershell -ExecutionPolicy Bypass -File install.ps1 -File .\epm-cli_0.2.0_windows_amd64.zip
```

**Linux packages**

```sh
gh release download -R droidnik/epm-cli -p '*_amd64.deb' && sudo apt install ./epm-cli_*_amd64.deb
gh release download -R droidnik/epm-cli -p '*.x86_64.rpm' && sudo dnf install ./epm-cli-*.x86_64.rpm
```

**Docker** (private image; log in with a token that has `read:packages`)

```sh
docker run --rm -e EPM_URL=acme -e EPM_CLIENT_ID -e EPM_CLIENT_SECRET \
  ghcr.io/droidnik/epm-cli:latest computers list --all -o json
```

CI/CD and Kubernetes examples: [4. Automation](docs/04-automation.md).

**With Go / from source** (Go ≥ 1.26)

```sh
GOPRIVATE=github.com/droidnik/* go install github.com/droidnik/epm-cli/cmd/epm-cli@latest
make build      # → ./bin/epm-cli
make install    # → $(go env GOPATH)/bin/epm-cli
```

## Quick start

```sh
epm-cli config add prod      # tenant, client ID, secret (stored in the macOS Keychain)
epm-cli doctor               # verify DNS, TLS, token and API access
epm-cli overview             # tenant summary
epm-cli --help               # all commands
```

Create the API account in the EPM console under **Configuration › Settings › API Settings**. Details:
[Getting started](docs/01-getting-started.md).

## Releasing

```sh
git tag v0.3.0 && git push origin v0.3.0
```

`.github/workflows/release.yml` builds every package with GoReleaser on a macOS runner (needed for the
`.pkg`), publishes the GitHub release and pushes the container image. Locally, without publishing:
`make snapshot` (packages into `dist/`) and `make image` (needs Docker).

**Signing and notarization (optional).** When the repository secrets `MACOS_CERTS_P12`,
`MACOS_CERTS_PASSWORD`, `CODESIGN_IDENTITY` and `PKG_SIGN_IDENTITY` exist (plus `NOTARY_KEY_P8`,
`NOTARY_KEY_ID`, `NOTARY_ISSUER` for notarization), the binary and the `.pkg` are signed with a
Developer ID and notarized. Without them, the artifacts are unsigned. See the header of `release.yml`
and `scripts/build-pkg.sh`.

## Development

```
cmd/epm-cli/          main
internal/cli/         cobra commands, generated API operations, MCP server
internal/client/      HTTP, OAuth token cache, retries, paging, multipart
internal/config/      profiles, URL normalization, Keychain
internal/output/      table / json / csv / yaml / …
internal/ojson/       JSON with preserved key order
internal/spec/        commands.json (generated, embedded with go:embed)
spec/openapi-v3.json  OpenAPI specification extracted from the docs
tools/specgen/        fetch the spec, generate the command table and the reference docs
docs/                 user guide; docs/reference/ is generated
scripts/              .pkg build, shell completions for packages
install.sh/.ps1       installers (macOS/Linux, Windows)
Dockerfile            container image
.goreleaser.yaml      release packages; .github/workflows/ CI and release
```

```sh
make test        # tests against a mock API server
make spec        # re-download the spec from docs.beyondtrust.com, regenerate commands and docs
make generate    # regenerate internal/spec/commands.json and docs/reference/
make docs        # regenerate docs/reference/ only
```

Command names, verb overrides and the list of destructive actions are defined in
`tools/specgen/main.go`. With tenant access, the official spec can also be downloaded directly:
`epm-cli api-definition download --out-file spec/openapi-v3.yaml`.
