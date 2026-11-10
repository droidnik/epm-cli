# epm-cli documentation

epm-cli is a command-line interface for **BeyondTrust Endpoint Privilege Management for Windows
and Mac** (EPM-WM / PM Cloud). It covers the complete Management API v3 and adds what you need for daily
admin work and automation: tenant profiles, tables/JSON/CSV output, automatic paging, dry runs,
confirmation for destructive actions, backups and an MCP server for AI assistants.

## User guide

| Chapter | What you learn |
|---|---|
| [1. Getting started](01-getting-started.md) | Install, create an API account, connect a tenant, run your first commands |
| [2. Basics](02-basics.md) | How commands are built, global flags, output formats, filters, paging, request bodies, safety, exit codes |
| [3. Workflows](03-workflows.md) | Step-by-step recipes: computers, groups, policies, JIT admin requests, events, users, backups |
| [4. Automation](04-automation.md) | Shell scripts, CI/CD (GitHub Actions, GitLab), Docker, Kubernetes, MCP for Claude |
| [5. Troubleshooting](05-troubleshooting.md) | `doctor`, common errors and how to fix them, verbose logging |

## Command reference

The [command reference](reference/README.md) lists **every command** with its arguments, all flags,
allowed values and an example request body. It is generated from the API specification, so it always
matches the binary.

In the terminal you get the same information with:

```sh
epm-cli --help                        # all resources, grouped
epm-cli computers --help              # all commands of one resource
epm-cli computers list --help         # all flags of one command
epm-cli commands archive              # search all commands
epm-cli groups create --body-template # example request body
```
