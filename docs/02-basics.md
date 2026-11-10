# 2. Basics

[← 1. Getting started](01-getting-started.md) · Next: [3. Workflows](03-workflows.md)

## How commands are built

```
epm-cli <resource> <command> [ID …] [flags]
```

- **Resource**: an area of the API, for example `computers`, `groups`, `policies`, `users`.
- **Command**: what you want to do. The usual names are:

| Command | Meaning | HTTP |
|---|---|---|
| `list` | list many items, with filters and paging | GET |
| `get <ID>` | one item with all details | GET |
| `show` | a single object without ID (for example `about show`) | GET |
| `create` | create | POST |
| `update` | change | PUT |
| `delete` | delete (asks first) | DELETE |
| anything else | an action, for example `computers authorise`, `groups assign-computers`, `policies upload` | POST/PATCH |

- **IDs** are positional arguments and always UUIDs, for example
  `epm-cli groups get 59a00329-87ac-49ec-bc2c-9b9e26f05185`.
- **Flags** come after the command; global flags can go anywhere.

Find commands:

```sh
epm-cli --help                  # all resources
epm-cli groups --help           # commands of a resource
epm-cli groups list --help      # flags of a command, incl. API endpoint
epm-cli commands policy         # search all 94 operations (resource, command, method, path)
```

Each command maps to exactly one API endpoint (shown in `--help` as `API: GET /v3/Groups`). The full
list is in the [command reference](reference/README.md).

## Global flags

These work with every command:

| Flag | Purpose |
|---|---|
| `-p, --profile NAME` | tenant profile (default: the one set with `config use`, or `EPM_PROFILE`) |
| `-o, --output FORMAT` | `table` (default), `json`, `ndjson`, `csv`, `yaml`, `plain`, `raw` |
| `--field PATH` | print only one field of every result, for example `--field id` |
| `--columns a,b,c` | choose columns for `table`, `csv` and `plain` |
| `--wide` | table: show all columns and don't shorten values |
| `--out-file PATH` | write the output to a file |
| `-n, --dry-run` | show the request that would be sent, send nothing |
| `-y, --yes` | confirm destructive commands without a prompt |
| `--no-input` | never prompt; fail instead (for scripts) |
| `-q, --quiet` | no hints or progress on stderr |
| `-v, --verbose` | log every HTTP request and response status to stderr |
| `--no-color` | no colours (also `NO_COLOR=1`) |

Default flags for every call can be set in `EPM_CLI_ARGS`, for example
`export EPM_CLI_ARGS='-p prod -o json'`.

## Output

### Table (default)

List commands show a curated set of columns per resource. Columns that are empty everywhere are hidden,
and if the table is wider than the terminal, the remaining columns are dropped with a hint on stderr.

```sh
epm-cli computers list
```

```
ID                                    HOST        OS       DOMAIN     AGENTVERSION  AUTHORISATIONSTATE  CONNECTIONSTATUS  GROUPNAME  POLICYNAME
1f0e8c2a-…                            WS-0142     Windows  corp.lan   24.7.1        Authorised          Connected         Office     Office Base
```

Single objects (`get`, `show`) are shown as key/value list; nested objects become dotted keys
(`requestInfo.computerName`).

Choose columns yourself, also nested ones:

```sh
epm-cli computers list --columns host,os,agentVersion,lastConnected
epm-cli computers list --wide        # every column
```

### JSON, YAML, CSV and more

```sh
epm-cli computers list --all -o json      # JSON array – best for scripts and jq
epm-cli computers list --all -o ndjson    # one JSON object per line – for streaming / large data
epm-cli computers list --all -o csv --out-file computers.csv   # for Excel
epm-cli policies get <ID> -o yaml
epm-cli computers list -o raw             # the unmodified API response incl. paging envelope
```

List responses of the API are wrapped in a paging envelope (`pageNumber`, `pageCount`, `data`, …);
epm-cli removes it, so you get the plain array. `-o raw` keeps it.

### Pick a single field

```sh
epm-cli computers list --all --field id                   # one ID per line
epm-cli groups get <ID> --field policyName
epm-cli computers list --all --field host -o json         # JSON array of host names
```

### Files and downloads

Commands that return files (policy XML, computer logs, the API definition) write the raw bytes.
Use `--out-file` or a redirect:

```sh
epm-cli policies content <POLICY-ID> --out-file policy.xml
epm-cli computers log-content <LOG-ID> > agent-logs.zip
```

stdout only contains the result. Hints ("Page 1/5 …"), progress and success messages go to stderr,
so pipes stay clean.

## Filters

List commands have one flag per API filter. `--help` and the [reference](reference/README.md) show them all.

```sh
epm-cli computers list --os Mac --connection-status Disconnected --days-disconnected 30
epm-cli computers list --group-name "Office" --agent-version 24.7.1
epm-cli groups list --policy-revision-status AwaitingPolicyUpdate
epm-cli users list --disabled
```

Rules:

- **Allowed values** (enums) are listed in the help, for example `--os (Windows|Mac)`. Upper/lower case
  does not matter: `--os mac` works.
- **Switches** (true/false): `--disabled` means true, `--disabled=false` means false.
- **Lists**: comma-separated or repeated: `--connection-status Connected,Disconnected`.
- **Dates**: the API accepts `2026-11-01`, `2026-11-01 08:00:00` or full ISO timestamps. Date filters
  have a selection mode:

  ```sh
  epm-cli computers list --created-dates 2026-11-01 --created-dates 2026-11-10 --created-selection-mode Range
  ```

- **Sorting**: `--sort FIELD[:asc|desc]`, repeatable: `--sort lastConnected:desc --sort host`.
- **Anything not covered** can be passed raw: `--query Filter.Something=value`.

## Paging

The API returns results in pages (page size up to 200). Without extra flags you get the first page, and a
hint on stderr tells you how many records exist:

```
Page 1/12 (1173 records total). Use --all to fetch everything, or --page N.
```

| Flag | Effect |
|---|---|
| `--all` / `-A` | fetch every page and return one combined list |
| `--max-pages N` | with `--all`: stop after N pages |
| `--page N`, `--page-size N` | fetch one specific page |

For `computers list --all`, the CLI sorts by creation date automatically. BeyondTrust recommends this to
avoid duplicates while paging.

## Commands that send data

### Field flags

Simple body fields are flags:

```sh
epm-cli groups create --name "Pilot Macs" --description "Early adopters"
epm-cli groups assign-computers <GROUP-ID> --computer-ids <ID1>,<ID2>
```

### Nested fields: `--set`

```sh
epm-cli computers authorise --all-computers --set filter.os=Windows --group-id <GROUP-ID>
```

The value is read as JSON when possible, so `--set 'computerIds=["a","b"]'` gives a list and
`--set allComputers=true` a boolean.

### Whole body: `--body`

```sh
epm-cli groups create --body '{"name":"Pilot","description":"Test"}'
epm-cli computers authorise --body @authorise.json
cat body.json | epm-cli groups create --body -
```

Field flags and `--set` are applied on top of `--body`, so a file can hold the defaults and flags the
values that change.

### Not sure what the body looks like?

```sh
epm-cli computers authorise --body-template
```

```json
{
  "computerIds": ["00000000-0000-0000-0000-000000000000"],
  "excludedComputerIds": ["00000000-0000-0000-0000-000000000000"],
  "filter": { "computerId": "…", "host": "", "os": "Windows", … },
  "allComputers": false,
  "groupId": "00000000-0000-0000-0000-000000000000"
}
```

Save it, edit it, send it with `--body @file.json`.

### File uploads

Commands that upload files take a path:

```sh
epm-cli policies create --name "Base Policy" --policy-file ./base.xml
epm-cli groups assign-computers-by-csv <GROUP-ID> --csv-file ./computers.csv
```

## Safety

**Dry run.** `-n` / `--dry-run` shows method, URL and body and sends nothing. Use it before every
change:

```sh
epm-cli computers delete --computer-ids <ID> --dry-run
```

```json
{
  "method": "DELETE",
  "url": "https://acme-services.pm.beyondtrustcloud.com/management-api/v3/Computers",
  "body": { "computerIds": ["<ID>"] },
  "destructive": true
}
```

**Confirmation.** Destructive commands ask before they run:
`delete`, `reject`, `archive`, `bulk-archive`, `deactivate`, `clear-policy`, `bulk-clear-policy`,
`disable`, `discard-draft`, `unassign-computers`. They are marked ⚠️ in the reference and
`[destructive]` in `--help`.

```
About to delete groups 59a00329-…. Continue? [y/N]
```

In scripts (no terminal) these commands **fail** unless you add `--yes`. This prevents accidental mass
changes from a pipeline.

**Rate limit.** The API allows 1000 requests per 100 seconds. When the limit is hit, the CLI waits and
retries automatically.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | general error (also `doctor` found a problem) |
| 2 | wrong usage or configuration: unknown flag, missing argument, missing profile, refused without `--yes` |
| 3 | authentication: wrong secret, no access (HTTP 401/403) |
| 4 | not found (HTTP 404) |
| 5 | other client error: validation, conflict, locked (HTTP 4xx) |
| 6 | server or network error (HTTP 5xx, timeout, DNS) |
| 130 | cancelled |

With `-o json` errors are printed as JSON on stderr:

```json
{"error":"GET /management-api/v3/Computers/…: HTTP 404 Not Found: Computer not found","exit_code":4,"status":404}
```

## The raw API

For anything special, `api` sends any request with your profile's authentication:

```sh
epm-cli api GET /v3/About
epm-cli api GET /v3/Computers -Q Filter.OS=Mac --all
epm-cli api POST /v3/Groups -d '{"name":"Pilot"}'
epm-cli auth token        # print a valid bearer token, e.g. for curl or Postman
```

Next: [3. Workflows](03-workflows.md)
