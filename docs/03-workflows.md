# 3. Workflows

[← 2. Basics](02-basics.md) · Next: [4. Automation](04-automation.md)

Step-by-step recipes for common admin tasks. Placeholders like `<GROUP-ID>` are UUIDs; get them from the
matching `list` command (`--field id` prints only the IDs). Try changes with `--dry-run` first.

- [Computers](#computers)
- [Computer groups](#computer-groups)
- [Policies](#policies)
- [Application groups in a policy](#application-groups-in-a-policy)
- [Just-in-time admin requests](#just-in-time-admin-requests)
- [Events and audits](#events-and-audits)
- [Console users and roles](#console-users-and-roles)
- [Tenant overview and backup](#tenant-overview-and-backup)

---

## Computers

### Find computers

```sh
epm-cli computers list --all                                  # everything (table)
epm-cli computers list --host WS-0142                         # by host name
epm-cli computers list --os Mac --group-name "Office" --all   # Macs in one group
epm-cli computers list --agent-version 24.7.1 --all           # by agent version
epm-cli computers list --policy-revision-status AwaitingPolicyUpdate --all   # policy not yet applied
epm-cli computers get <COMPUTER-ID>                           # all details
```

Values such as connection or authorisation state are free text in the API. Look at the
`connectionStatus` / `authorisationState` columns of `computers list` to see the values your tenant uses
(for example `Authorised` and `Pending`).

### Computers that have not checked in

```sh
epm-cli computers list --days-disconnected 30 --all --columns host,os,lastConnected,groupName
epm-cli computers list --days-disconnected 30 --all -o csv --out-file stale.csv
```

### Authorise new computers

New agents may wait for authorisation before they get a policy.

```sh
# 1. see who is waiting
epm-cli computers list --authorisation-state Pending --all

# 2. authorise specific computers and put them into a group
epm-cli computers authorise --computer-ids <ID1>,<ID2> --group-id <GROUP-ID>

# or: authorise all pending Windows computers
epm-cli computers authorise --all-computers --set filter.os=Windows --group-id <GROUP-ID> --dry-run
```

Reject unknown devices (destructive):

```sh
epm-cli computers reject --computer-ids <ID>
```

### Move computers to another group

```sh
epm-cli groups assign-computers <GROUP-ID> --computer-ids <ID1>,<ID2>

# from a CSV file (format as exported from the EPM console)
epm-cli groups assign-computers-by-csv <GROUP-ID> --csv-file ./computers.csv

# all computers that match a filter, except some
epm-cli groups assign-computers <GROUP-ID> --all-computers --set filter.os=Mac --excluded-computer-ids <ID>
```

### Archive and clean up

```sh
# archive one computer
epm-cli computers archive <COMPUTER-ID>

# archive every computer offline for 90+ days
ids=$(epm-cli computers list --days-disconnected 90 --all --field id | paste -sd, -)
epm-cli computers bulk-archive --computer-ids "$ids" --dry-run    # check
epm-cli computers bulk-archive --computer-ids "$ids"              # asks for confirmation

# restore
epm-cli computers unarchive <COMPUTER-ID>

# delete for good
epm-cli computers delete --computer-ids <ID1>,<ID2>
```

`computers deactivate` no longer exists in EPM. The API now archives **and deletes** the computer instead.

### Agent logs and status

```sh
epm-cli computers details-request --computer-id <COMPUTER-ID>   # ask the agent to send fresh status
epm-cli computers logs-request --computer-id <COMPUTER-ID>      # ask the agent to upload its logs
epm-cli computers logs <COMPUTER-ID>                            # list uploaded logs (wait a few minutes)
epm-cli computers log-content <LOG-ID> --out-file logs.zip      # download one
epm-cli computers command-log <COMPUTER-ID>                     # commands sent to this computer
epm-cli computers certificate-renew --computer-id <COMPUTER-ID>
```

Some actions return an ID. If it belongs to a background job, follow it with
`epm-cli tasks get <ID>`.

---

## Computer groups

Each computer belongs to one group; each group has one policy revision.

```sh
epm-cli groups list                                           # name, computers, policy, revision, status
epm-cli groups list --policy-revision-status NoPolicy         # groups without a policy
epm-cli groups get <GROUP-ID>
epm-cli groups create --name "Pilot Macs" --description "Early adopters"
epm-cli groups update --id <GROUP-ID> --name "Pilot Macs (2026)"
epm-cli groups mark-as-default <GROUP-ID>                     # new computers land here
epm-cli groups delete <GROUP-ID>
```

### Assign a policy to a group

A group gets a specific **policy revision**, not just a policy:

```sh
# 1. find the policy and its latest revision
epm-cli policies list --name "Office Base"
epm-cli policies revisions <POLICY-ID>

# 2. assign
epm-cli groups assign-policy-revision <GROUP-ID> --policy-revision-id <REVISION-ID>

# same revision for several groups
epm-cli groups assign-policy-to-groups --group-ids <G1>,<G2> --policy-revision-id <REVISION-ID>

# remove the policy from a group (destructive)
epm-cli groups clear-policy <GROUP-ID>
epm-cli groups bulk-clear-policy --group-ids <G1>,<G2>
```

---

## Policies

### Look at policies

```sh
epm-cli policies list                         # revision, assigned, open draft, last change
epm-cli policies get <POLICY-ID>
epm-cli policies revisions <POLICY-ID>        # history
epm-cli policies assigned-groups <POLICY-ID>  # where it is used
```

### Download, edit, upload (round trip)

Policies are XML files. You can edit them in the BeyondTrust Policy Editor, a text editor or under
version control.

```sh
# 1. download the latest revision
epm-cli policies content <POLICY-ID> --out-file office-base.xml

# 2. edit office-base.xml …

# 3. upload it as a new revision and roll it out to every group that uses this policy
epm-cli policies upload <POLICY-ID> --policy-file office-base.xml --auto-assign-to-group

# 4. check that the computers pick it up
epm-cli groups list --policy-revision-status AwaitingPolicyUpdate
```

Without `--auto-assign-to-group` the new revision is stored but not rolled out. Roll it out later with
`groups assign-policy-revision` or for all groups of the policy:

```sh
epm-cli groups auto-assign-policy-revision --policy-id <POLICY-ID> --policy-revision-id <REVISION-ID>
```

An older revision can be downloaded with `policies revision-content <REVISION-ID>`.

### Create, rename, delete

```sh
epm-cli policies create --name "Developer Workstations" --description "Admin tools allowed" --policy-file ./dev.xml
epm-cli policies update <POLICY-ID> --name "Developer Workstations v2"
epm-cli policies discard-draft <POLICY-ID>          # drop unsaved changes from the web editor (destructive)
epm-cli policies delete <POLICY-ID>                 # fails while assigned …
epm-cli policies delete <POLICY-ID> --force-delete  # … unless forced
```

---

## Application groups in a policy

Read and change applications inside a policy without editing XML:

```sh
# list applications (Windows / macOS)
epm-cli policy-editor windows-applications <POLICY-ID>
epm-cli policy-editor windows-applications <POLICY-ID> --application-group-name "Allowed Admin Tools"
epm-cli policy-editor macos-applications <POLICY-ID> -o json

# add or remove applications: start from the template, edit, send
epm-cli policy-editor windows-applications-modify <POLICY-ID> --body-template > change.json
epm-cli policy-editor windows-applications-modify <POLICY-ID> --body @change.json --dry-run
epm-cli policy-editor windows-applications-modify <POLICY-ID> --body @change.json
```

The body names the application group (`applicationGroups.edit[].id`) and lists the applications to
`add` and `delete`. The [reference](reference/policy-editor.md) shows the full structure.

---

## Just-in-time admin requests

Users can request temporary admin rights. Review and decide from the CLI:

```sh
epm-cli admin-access-requests list --active-only            # open requests
epm-cli admin-access-requests get <REQUEST-ID>

# approve for 30 minutes
epm-cli admin-access-requests decide --request-id <REQUEST-ID> --decision Approved \
  --duration 1800 --decision-performed-by-user "<YOUR-ACCOUNT>" --message "Approved for driver install"

# deny
epm-cli admin-access-requests decide --request-id <REQUEST-ID> --decision Denied \
  --decision-performed-by-user "<YOUR-ACCOUNT>" --message "Please open a ticket"
```

`--duration` is in seconds. Valid decisions are `Approved` and `Denied`.

A request can also be created on behalf of a user (for example from a ticket system). Include
`--decision` and `--decision-performed-by-user` to create it already approved:

```sh
epm-cli admin-access-requests create --body-template
```

Elevation requests from the application rules (as opposed to full admin sessions) are in
`authorization-requests`; the history of both is in `authorization-request-audits`:

```sh
epm-cli authorization-requests list --active-only
epm-cli authorization-request-audits list --sort timeOfRequest:desc
```

---

## Events and audits

### Endpoint events

```sh
# events of one day on Windows
epm-cli events search --start-date 2026-11-09T00:00:00Z --end-date 2026-11-10T00:00:00Z \
  --operating-system Windows --all -o ndjson > events.ndjson

# narrow down
epm-cli events search --start-date … --end-date … --operating-system Windows \
  --host-name WS-0142 --user-name jdoe --workstyle-name "Office Workers"

# continuous export: everything since a point in time (max 1000 per call)
epm-cli events from-start-date --start-date 2026-11-10T00:00:00Z --record-size 1000 -o ndjson
```

`--start-date`, `--end-date` and `--operating-system` are required for `events search`. Dates are UTC.
Check the exact operating-system value and other filter values in the output of a first search.

### Who changed what in the console

```sh
epm-cli activity-audits list --sort created:desc
epm-cli activity-audits list --changed-by API --all       # changes made through the API
epm-cli activity-audits get <AUDIT-ID>
```

---

## Console users and roles

```sh
epm-cli users list
epm-cli users list --disabled
epm-cli global-roles list                     # role IDs
epm-cli roles list

epm-cli users create --email-address <EMAIL-ADDRESS> --global-roles-ids <ROLE-ID>
epm-cli users create --body-template          # all options (group/policy roles, language, time zone)
epm-cli users assign-roles <USER-ID> --global-roles-ids <ROLE-ID1>,<ROLE-ID2>
epm-cli users disable <USER-ID>
epm-cli users enable <USER-ID>
epm-cli users resend-invite <USER-ID>
```

Users from an identity provider are managed with the `scim-users` commands (needs SCIM access on the API
account).

API accounts and allowed sign-in domains:

```sh
epm-cli api-accounts list
epm-cli accepted-domains list
epm-cli accepted-domains create --domain-name <YOUR-DOMAIN>
epm-cli authentication-provider show
```

---

## Tenant overview and backup

```sh
epm-cli overview
```

```
profile                     prod
url                         https://acme-services.pm.beyondtrustcloud.com
about.version               25.4.0
computers                   1173
groups                      14
policies                    9
users                       21
groupsWithoutPolicy         1
groupsAwaitingPolicyUpdate  2
defaultGroup                Unassigned
policiesWithOpenDraft       1
policiesUnassigned          2
```

### Backup

`backup` exports the configuration into a folder: tenant info, accepted domains, API accounts,
authentication provider, roles, users, groups and **every policy with metadata, revision list,
assigned groups and the latest policy XML**.

```sh
epm-cli backup                                  # → ./epm-backup-prod-20261110-141500/
epm-cli backup --dir ./epm-config               # fixed folder
epm-cli backup --dir ./epm-config --include-computers   # also the computer inventory
```

```
epm-config/
├── manifest.json            what was exported, and any errors
├── about.json
├── groups.json  users.json  roles.json  global-roles.json  …
└── policies/
    └── Office_Base-1a2b3c4d/
        ├── policy.json  revisions.json  assigned-groups.json
        └── latest.xml
```

Put the folder under Git to see every change over time:

```sh
cd epm-config && git init -q && git add -A && git commit -qm "initial"
# later
epm-cli backup --dir . && git add -A && git commit -qm "backup $(date +%F)" && git log --stat -1
```

Scheduling this nightly is shown in [4. Automation](04-automation.md#nightly-backup).

Next: [4. Automation](04-automation.md)
