# 5. Troubleshooting

[← 4. Automation](04-automation.md) · [Documentation](README.md)

## Start with `doctor`

```sh
epm-cli doctor            # or: epm-cli -p sandbox doctor
```

It checks, in this order: config file, profile, client secret, DNS, TLS, token request and an API call.
The first failing line tells you where to look. Exit code 1 means at least one check failed.

## See the HTTP traffic

```sh
epm-cli -v computers list
```

```
> GET https://acme-services.pm.beyondtrustcloud.com/management-api/v3/Computers
< 200 application/json 48213B 412ms
```

Request bodies are logged too (secrets and tokens are not). Combine with `--dry-run` to see a request
without sending it.

## Common errors

| Message | Cause | Fix |
|---|---|---|
| `no EPM tenant configured` | no profile and no `EPM_URL` / `EPM_CLIENT_ID` | `epm-cli config add <name>` or set the environment variables |
| `profile 'x' not found` | typo, or `EPM_PROFILE` points to a missing profile | `epm-cli config list` |
| `no client secret for profile` | secret missing from Keychain / file | `epm-cli config set-secret <name>` |
| `token request failed … invalid_client` (exit 3) | wrong client ID or secret, or the secret was regenerated | create a new secret in API Settings, then `config set-secret` |
| `HTTP 401` / `403` (exit 3) | API account lacks the needed access (e.g. SCIM) | enable the access type on the API account |
| `cannot reach … no such host` (exit 6) | wrong tenant name, VPN or proxy | check the URL with `epm-cli config show`; set `HTTPS_PROXY` if needed |
| `x509: certificate …` | TLS inspection by a corporate proxy | install the proxy's root CA in the system trust store |
| `HTTP 404` (exit 4) | ID does not exist or belongs to another tenant | check the ID with the matching `list` command and `-p` |
| `HTTP 409 Conflict` / `423 Locked` (exit 5) | object in use or locked, e.g. policy opened in the web editor | close the editor / discard the draft (`policies discard-draft`) and retry |
| `HTTP 400 Bad Request: …` (exit 5) | invalid value or missing field | read the detail text; compare with `--body-template` and the [reference](reference/README.md) |
| `… refusing without confirmation; re-run with --yes` (exit 2) | destructive command without a terminal | check with `--dry-run`, then add `--yes` |
| `invalid value "x" for --os` (exit 2) | value not allowed | the message lists the allowed values |
| `required flag(s) … not set` (exit 2) | mandatory filter missing | e.g. `events search` needs start, end and OS |
| `! HTTP 429, retrying in …` | rate limit (1000 requests / 100 s) | nothing to do; the CLI waits. Reduce parallel jobs if it happens often |

## Token and cache

```sh
epm-cli auth status       # request a fresh token and show expiry, scope, client
epm-cli auth logout       # delete the cached token
```

If you regenerate the client secret in the EPM console, old tokens stop working immediately. The CLI
notices the 401 and requests a new token once; if that fails, update the secret with
`config set-secret`.

## Where things are stored

| What | Where |
|---|---|
| Profiles | `~/.config/epm-cli/config.json` (`epm-cli config path`) |
| Client secret | macOS Keychain, item "epm-cli"; elsewhere `~/.config/epm-cli/secrets.json` (mode 0600) |
| Token cache | `~/.cache/epm-cli/tokens/` (mode 0600) |

To remove the CLI completely: delete the binary (`rm "$(command -v epm-cli)"`), the two folders above
and the Keychain item "epm-cli" (Keychain Access, or `epm-cli config remove <name>` for each profile
first).

## Reporting a problem

Include the output of these commands (they contain no secrets):

```sh
epm-cli version
epm-cli doctor -o json
epm-cli -v <the failing command> 2>&1 | tail -20
```
