# 4. Automation

[← 3. Workflows](03-workflows.md) · Next: [5. Troubleshooting](05-troubleshooting.md)

## Rules for scripts

1. **Use JSON** (`-o json`, or `-o ndjson` for large data) and parse it with `jq`. Never parse tables.
2. **Use `--no-input`** so the CLI never waits for a prompt.
3. **Check exit codes** (see [Basics](02-basics.md#exit-codes)); errors come as JSON on stderr with `-o json`.
4. **Destructive commands need `--yes`.** Run them with `--dry-run` first and only add `--yes` once you
   know what they do.
5. **Credentials from the environment** in CI instead of a profile:

| Variable | Meaning |
|---|---|
| `EPM_URL` | tenant name or URL (`acme` or `https://acme-services.pm.beyondtrustcloud.com`) |
| `EPM_CLIENT_ID` | API account client ID |
| `EPM_CLIENT_SECRET` | API account secret |
| `EPM_PROFILE` | profile to use (if you have a config) |
| `EPM_CLI_ARGS` | default flags, e.g. `--no-input -o json` |
| `EPM_CLI_CACHE_DIR` | where the token is cached (default `~/.cache/epm-cli`) |
| `EPM_TIMEOUT` | HTTP timeout in seconds (default 60) |
| `EPM_CLI_NO_HINTS` | `1` = no hints on stderr |
| `NO_COLOR` | `1` = no colours |

The access token (valid ~1 hour) is cached and reused across calls, so many small commands in a row
don't each request a new token.

## Shell examples

```sh
#!/bin/sh
set -eu
export EPM_CLI_ARGS="--no-input -o json"

# number of computers per OS
epm-cli computers list --all | jq -r 'group_by(.os)[] | "\(.[0].os): \(length)"'

# hosts that have not connected for 30 days, as CSV with selected columns
epm-cli computers list --days-disconnected 30 --all -o csv --columns host,os,lastConnected,groupName > stale.csv

# all groups that still wait for a policy update
epm-cli groups list --policy-revision-status AwaitingPolicyUpdate --all | jq -r '.[].name'

# react to errors
code=0
out=$(epm-cli computers get "$ID" 2>err.json) || code=$?
case $code in
  0) echo "$out" | jq -r .host ;;
  4) echo "computer $ID does not exist" ;;
  *) jq -r .error err.json >&2; exit "$code" ;;
esac
```

### Bulk changes from a list

```sh
# move every computer listed in hosts.txt to the group "Pilot"
group=$(epm-cli groups list --name Pilot --field id -o plain)
ids=$(while read -r host; do
        epm-cli computers list --host "$host" --field id -o plain
      done < hosts.txt | paste -sd, -)
epm-cli groups assign-computers "$group" --computer-ids "$ids" --dry-run
epm-cli groups assign-computers "$group" --computer-ids "$ids"
```

## Docker

The image `ghcr.io/droidnik/epm-cli` contains the CLI, `jq` and CA certificates. It runs as a non-root
user and works with a read-only root filesystem.

```sh
docker run --rm -e EPM_URL=acme -e EPM_CLIENT_ID -e EPM_CLIENT_SECRET \
  ghcr.io/droidnik/epm-cli:latest computers list --all -o json

# backup into the current folder
docker run --rm -e EPM_URL=acme -e EPM_CLIENT_ID -e EPM_CLIENT_SECRET \
  -v "$PWD:/data" -w /data --user "$(id -u)" \
  ghcr.io/droidnik/epm-cli:latest backup --dir /data/epm-config
```

Pin a version (`:0.3.0`) for reproducible pipelines; `:latest` follows the newest release.

## GitHub Actions

Store `EPM_CLIENT_ID` and `EPM_CLIENT_SECRET` as repository secrets.

<a id="nightly-backup"></a>

### Nightly backup into Git

```yaml
name: epm-backup
on:
  schedule: [{ cron: "0 2 * * *" }]
  workflow_dispatch:

permissions:
  contents: write

jobs:
  backup:
    runs-on: ubuntu-latest
    container:
      image: ghcr.io/droidnik/epm-cli:latest
      options: --user root   # checkout and apk need to write as root inside the job container
    env:
      EPM_URL: acme
      EPM_CLIENT_ID: ${{ secrets.EPM_CLIENT_ID }}
      EPM_CLIENT_SECRET: ${{ secrets.EPM_CLIENT_SECRET }}
    steps:
      - uses: actions/checkout@v4
      - run: epm-cli backup --dir epm-config
      - name: commit changes
        run: |
          apk add --no-cache git >/dev/null
          git config --global --add safe.directory "$GITHUB_WORKSPACE"
          git config user.name "epm-backup"; git config user.email "epm-backup@users.noreply.github.com"
          git add epm-config
          git diff --cached --quiet || git commit -m "EPM backup $(date +%F)" && git push
```

The image runs as a non-root user; `--user root` is only needed in GitHub job containers because
`actions/checkout` writes to the workspace. Alternatively run the job without `container:` and install
the CLI with `install.sh`.

### Deploy a policy from Git

Keep the policy XML in a repository and upload it on every change to `main`:

```yaml
name: deploy-policy
on:
  push:
    branches: [main]
    paths: ["policies/office-base.xml"]

jobs:
  deploy:
    runs-on: ubuntu-latest
    container:
      image: ghcr.io/droidnik/epm-cli:latest
      options: --user root
    env:
      EPM_URL: acme
      EPM_CLIENT_ID: ${{ secrets.EPM_CLIENT_ID }}
      EPM_CLIENT_SECRET: ${{ secrets.EPM_CLIENT_SECRET }}
      POLICY_ID: ${{ vars.OFFICE_BASE_POLICY_ID }}
    steps:
      - uses: actions/checkout@v4
      - run: epm-cli doctor
      - run: epm-cli policies upload "$POLICY_ID" --policy-file policies/office-base.xml --auto-assign-to-group
```

## GitLab CI

The image's entrypoint is `epm-cli`; GitLab needs it cleared to run `script:`.

```yaml
epm-backup:
  image:
    name: ghcr.io/droidnik/epm-cli:latest
    entrypoint: [""]
  variables:
    EPM_URL: acme
    # EPM_CLIENT_ID / EPM_CLIENT_SECRET as masked CI/CD variables
  script:
    - epm-cli doctor
    - epm-cli backup --dir epm-config
  artifacts:
    paths: [epm-config/]
  rules:
    - if: $CI_PIPELINE_SOURCE == "schedule"
```

## Kubernetes

Nightly export as a CronJob:

```yaml
apiVersion: v1
kind: Secret
metadata: { name: epm-cli }
stringData:
  EPM_URL: acme
  EPM_CLIENT_ID: "<client-id>"
  EPM_CLIENT_SECRET: "<client-secret>"
---
apiVersion: batch/v1
kind: CronJob
metadata: { name: epm-backup }
spec:
  schedule: "0 2 * * *"
  jobTemplate:
    spec:
      template:
        spec:
          restartPolicy: Never
          containers:
            - name: epm-cli
              image: ghcr.io/droidnik/epm-cli:0.3.0
              args: ["backup", "--dir", "/data"]
              envFrom: [{ secretRef: { name: epm-cli } }]
              securityContext:
                readOnlyRootFilesystem: true
                runAsNonRoot: true
                allowPrivilegeEscalation: false
              volumeMounts:
                - { name: data, mountPath: /data }
                - { name: tmp, mountPath: /tmp }
          volumes:
            - { name: data, persistentVolumeClaim: { claimName: epm-backup } }
            - { name: tmp, emptyDir: {} }
```

## MCP: use epm-cli from Claude and other AI assistants

`epm-cli mcp` starts a [Model Context Protocol](https://modelcontextprotocol.io) server on stdio. An
assistant can then look up commands and run them for you.

```sh
claude mcp add epm-cli -- epm-cli mcp                           # read-only (default)
claude mcp add epm-cli -- epm-cli mcp -p sandbox --allow-writes # allow changes on the sandbox tenant
```

Other MCP clients (Claude Desktop, VS Code, …) use the same command in their server configuration:

```json
{ "mcpServers": { "epm-cli": { "command": "epm-cli", "args": ["mcp"] } } }
```

| Tool | Purpose |
|---|---|
| `list_commands` | search the available commands |
| `describe_command` | flags, allowed values and body template of one command |
| `run_command` | run a command and return its output (JSON by default) |

Safety:

- **Read-only by default.** Any command that would change data is refused until the server is started
  with `--allow-writes`. Dry runs are always allowed.
- **Destructive commands** additionally need `confirm: true` in the tool call, so the assistant has to
  ask you explicitly.
- `auth token` is never available over MCP.
- Start the server with `-p <profile>` to pin it to one tenant; a sandbox profile is a good idea for
  write access.

`epm-cli agent-context` prints a short operating guide for AI agents (auth, flags, exit codes).

Next: [5. Troubleshooting](05-troubleshooting.md)
