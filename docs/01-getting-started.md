# 1. Getting started

[← Documentation](README.md) · Next: [2. Basics](02-basics.md)

## Install

The repository is private, so you need read access to `droidnik/epm-cli` and either a logged-in
GitHub CLI (`gh auth login`) or a token in `GH_TOKEN`. No Go toolchain is required.

**macOS / Linux**

```sh
gh api repos/droidnik/epm-cli/contents/install.sh -H "Accept: application/vnd.github.raw" | sh
```

**macOS installer package** (for example to deploy with Jamf Pro)

```sh
gh release download -R droidnik/epm-cli -p '*darwin_all.pkg'
sudo installer -pkg epm-cli_*_darwin_all.pkg -target /
```

**Windows** (PowerShell, no admin rights needed)

```powershell
gh api repos/droidnik/epm-cli/contents/install.ps1 -H "Accept: application/vnd.github.raw" | powershell -NoProfile -Command -
```

All installation options (Linux packages, Docker image, Go) are in the [README](../README.md#installation).
Check the installation:

```sh
epm-cli version
```

## Create an API account in EPM

1. Sign in to the EPM console and open **Configuration › Settings › API Settings**.
2. Create an API account and give it **Management API** access. Add *SCIM* access only if you want to
   use the `scim-*` commands.
3. Copy the **Client ID** and the **Client Secret**. The secret is only shown once; if you lose it,
   you have to regenerate it, which invalidates the old secret immediately.
4. Note your tenant URL from the top of the API Settings page, for example
   `https://acme-services.pm.beyondtrustcloud.com`.

## Connect the CLI to your tenant

```sh
epm-cli config add prod
```

You are asked for:

| Prompt | What to enter |
|---|---|
| Tenant | Just the name (`acme`), the console URL or the API URL. All become `https://acme-services.pm.beyondtrustcloud.com`. |
| Client ID | From the API account |
| Client secret | From the API account (typing is hidden) |

The profile is saved in `~/.config/epm-cli/config.json`. The secret goes into the **macOS Keychain**
(item "epm-cli"); on Linux and Windows it is stored in a file only you can read. Right after saving,
the CLI requests a token and calls the API to prove that everything works.

Without prompts (for scripts):

```sh
printf '%s\n' "$SECRET" | epm-cli config add prod --url acme --client-id "$CLIENT_ID" --client-secret-stdin
```

Check the connection at any time:

```sh
epm-cli doctor
```

```
CHECK          STATUS  DETAIL
config file    ok      /Users/you/.config/epm-cli/config.json
profile        ok      prod → https://acme-services.pm.beyondtrustcloud.com (config)
client secret  ok      present (keychain)
dns            ok      acme-services.pm.beyondtrustcloud.com → …
tls            ok      TLS 1.3 in 84ms
token          ok      valid, expires 2026-11-10T13:05:12+01:00
api            ok      GET /v3/About → …
```

## Several tenants

```sh
epm-cli config add sandbox          # add another tenant
epm-cli config list                 # show all profiles; the default one is marked
epm-cli config use sandbox          # change the default
epm-cli -p prod computers list      # use another profile for one command
export EPM_PROFILE=sandbox          # … or for the whole shell session
```

Other profile commands: `config show`, `config set-secret` (after rotating the secret),
`config remove`, `config path`.

## Your first commands

```sh
epm-cli overview                         # tenant summary: version, number of computers, groups, policies, users
epm-cli computers list                   # first page of computers as a table
epm-cli computers list --all             # every computer
epm-cli groups list                      # computer groups with their assigned policy
epm-cli policies list                    # policies, revisions, open drafts
epm-cli computers get <COMPUTER-ID>      # all details of one computer
```

## Shell completion

Tab completion for commands, flags, allowed values and profile names:

```sh
# zsh (macOS default)
epm-cli completion zsh > "${fpath[1]}/_epm-cli"     # then open a new shell
# bash
echo 'source <(epm-cli completion bash)' >> ~/.bashrc
# PowerShell
epm-cli completion powershell | Out-String | Invoke-Expression
```

The macOS `.pkg` and the Linux packages install the completion files automatically.

Next: [2. Basics](02-basics.md)
