# epm-cli

Kommandozeilen-Tool für **BeyondTrust Endpoint Privilege Management for Windows and Mac**
(PM Cloud, Management API v3), aufgebaut nach dem Vorbild von
[jamf-cli](https://github.com/Jamf-Concepts/jamf-cli): eine einzelne Go-Binärdatei mit Profilen,
Keychain, mehreren Ausgabeformaten, Dry-Run, Sicherheitsabfragen und MCP-Server für KI-Agenten.

Alle 94 Operationen der v3-API stehen als Befehle bereit. Sie werden aus der OpenAPI-Spezifikation
generiert, die aus der [offiziellen API-Referenz](https://docs.beyondtrust.com/epm-wm/reference/get_v3-accepteddomains)
extrahiert ist.

## Installation

Das Repository ist privat. Installieren kann deshalb nur, wer Lesezugriff auf
`droidnik/epm-cli` hat. Go wird dafür **nicht** benötigt.

### Mit dem Installationsskript (empfohlen)

Voraussetzung: GitHub CLI angemeldet (`brew install gh && gh auth login`) oder ein Token in
`GH_TOKEN` mit Lesezugriff auf das Repo.

```sh
gh api repos/droidnik/epm-cli/contents/install.sh -H "Accept: application/vnd.github.raw" | sh
```

Das Skript lädt die passende Binärdatei aus dem neuesten Release (macOS Universal, Linux
amd64/arm64), prüft die SHA-256-Checksumme und installiert nach `/usr/local/bin` (falls
beschreibbar) oder `~/.local/bin`. Optionen: `-v v0.2.0` (Version), `-d ~/bin` (Zielordner).
Aus einem Klon heraus: `sh install.sh`.

**Update:** denselben Befehl erneut ausführen. **Deinstallation:** `rm "$(command -v epm-cli)"`,
optional auch `~/.config/epm-cli` und den Keychain-Eintrag „epm-cli“.

### Manuell

Auf der Release-Seite (`gh release list -R droidnik/epm-cli`) das passende Archiv laden:
`gh release download -R droidnik/epm-cli -p '*darwin_all.tar.gz'`. Dann entpacken und
`epm-cli` in einen Ordner im `PATH` legen. Für Windows gibt es eine `.zip` mit `epm-cli.exe`.

### Mit Go

```sh
GOPRIVATE=github.com/droidnik/* go install github.com/droidnik/epm-cli/cmd/epm-cli@latest
```

### Neue Version veröffentlichen

```sh
git tag v0.2.0 && git push origin v0.2.0
```

Die GitHub Action `.github/workflows/release.yml` baut mit GoReleaser alle Binärdateien und
erstellt das Release mit `checksums.txt`. `make snapshot` baut dasselbe lokal nach `dist/`,
ohne etwas zu veröffentlichen. Bei jedem Push auf `main` laufen die Tests (`ci.yml`).

### Aus dem Quellcode

Voraussetzung: Go ≥ 1.26 (`brew install go`).

```sh
make build      # → ./bin/epm-cli
make install    # → $(go env GOPATH)/bin/epm-cli
```

## Einrichtung

1. In der EPM-Konsole unter **Configuration › Settings › API Settings** einen API-Account anlegen
   (Management API-Zugriff). Client-ID und Secret notieren.
2. Profil anlegen. Das Secret landet in der macOS-Keychain (Dienst `epm-cli`):

```sh
epm-cli config add prod                 # fragt Tenant, Client-ID und Secret ab
epm-cli doctor                          # prüft DNS, TLS, Token und API
```

Für den Tenant reicht der Name (`acme`), die Konsolen-URL oder die API-URL. Daraus wird
`https://acme-services.pm.beyondtrustcloud.com`.

Nicht-interaktiv (z. B. CI):

```sh
epm-cli config add prod --url acme --client-id "$ID" --client-secret-stdin <<< "$SECRET"
# oder ganz ohne Profil:
export EPM_URL=acme EPM_CLIENT_ID=… EPM_CLIENT_SECRET=…
```

Mehrere Tenants: `epm-cli config list`, `epm-cli config use sandbox`, `-p sandbox` pro Aufruf.

## Benutzung

```sh
epm-cli <ressource> <aktion> [ID] [flags]

epm-cli computers list --os Mac --all                  # alle Seiten
epm-cli computers list --connection-status Disconnected --days-disconnected 30 -o csv --out-file stale.csv
epm-cli computers get 59A00329-87AC-49EC-BC2C-9B9E26F05185 -o yaml
epm-cli groups list --columns id,name,computerCount,policyName
epm-cli groups assign-computers <GROUP-ID> --computer-ids id1,id2
epm-cli policies content <POLICY-ID> --out-file policy.xml
epm-cli policies upload <POLICY-ID> --policy-file policy.xml --auto-assign-to-group
epm-cli events search --start-date 2026-11-01 --end-date 2026-11-10 --operating-system Windows
epm-cli admin-access-requests list --active-only
epm-cli overview
epm-cli backup --dir ./epm-backup        # Policies inkl. XML, Gruppen, Benutzer, Rollen …
```

Übersicht und Details:

```sh
epm-cli --help                       # alle Ressourcen, gruppiert
epm-cli commands policy              # alle Operationen mit Methode/Pfad, filterbar
epm-cli groups create --help         # Flags einer Operation
epm-cli groups create --body-template   # Beispiel-Body
epm-cli api GET /v3/About            # Rohzugriff auf jeden Endpunkt
```

### Request-Bodies

Einfache Felder gibt es als Flags (`--name`, `--computer-ids a,b`). Verschachtelte Felder setzt
man mit `--set pfad=wert`, ganze Bodies mit `--body` (inline, `@datei.json` oder `-` für stdin).
Flags werden über `--body` gelegt:

```sh
epm-cli computers authorise --body @auth.json --group-id <GROUP-ID> --set filter.os=Windows
```

Boolesche Flags: `--active-only` heißt true, `--is-default=false` heißt false. Enum-Werte
sind unabhängig von Groß-/Kleinschreibung (`--os mac`).

### Ausgabe

| Flag | Wirkung |
|---|---|
| `-o table\|json\|ndjson\|csv\|yaml\|plain\|raw` | Format (Standard: table) |
| `--field pfad` | ein Feld pro Eintrag, z. B. `--field id` |
| `--columns a,b.c` | Spalten für table/csv/plain |
| `--wide` | alle Spalten, nichts kürzen |
| `--out-file datei` | in Datei statt stdout |
| `--all` / `--max-pages N` | alle Seiten laden (Computers automatisch nach `created asc` sortiert, wie von BeyondTrust empfohlen) |

Hinweise und Fortschritt gehen nach stderr (`-q` schaltet sie ab). `EPM_CLI_ARGS` stellt jedem
Aufruf Standard-Flags voran, z. B. `export EPM_CLI_ARGS='-p prod -o json'`.

### Sicherheit

- **Destruktive Aktionen** (`delete`, `reject`, `archive`, `deactivate`, `clear-policy`, `disable`,
  `discard-draft`, `unassign-computers` …) fragen nach. Ohne Terminal brechen sie ab, solange
  kein `--yes` angegeben ist.
- **`-n/--dry-run`** zeigt Methode, URL und Body und sendet nichts.
- Secrets liegen in der Keychain und werden über stdin an `security` übergeben, nicht in der
  Prozessliste. Config und Token-Cache haben die Rechte 0600.
- Rate-Limit der API: 1000 Requests pro 100 s. Bei HTTP 429 wartet die CLI und versucht es erneut.

### Exit-Codes

`0` ok · `1` allgemein · `2` Bedienfehler/Konfiguration · `3` Auth (401/403) · `4` nicht gefunden ·
`5` sonstige 4xx · `6` 5xx/Netzwerk · `130` abgebrochen.
Mit `-o json` erscheinen Fehler als JSON auf stderr.

### Shell-Completion

```sh
epm-cli completion zsh > "${fpath[1]}/_epm-cli"      # auch bash, fish, powershell
```

Ressourcen, Aktionen, Flags, Enum-Werte und Profilnamen werden vervollständigt.

## MCP-Server (Claude & Co.)

```sh
claude mcp add epm-cli -- epm-cli mcp                         # nur lesend
claude mcp add epm-cli -- epm-cli mcp -p sandbox --allow-writes
```

Tools: `list_commands`, `describe_command`, `run_command`. Standardmäßig sind nur lesende
Operationen erlaubt. Mit `--allow-writes` sind Schreibzugriffe freigegeben; destruktive brauchen
zusätzlich `confirm=true`. `epm-cli agent-context` gibt Agenten eine Kurzanleitung.

## Entwicklung

```
cmd/epm-cli/          main
internal/cli/         cobra-Befehle, generierte Operationen, MCP
internal/client/      HTTP, OAuth-Token-Cache, Retry, Pagination, Multipart
internal/config/      Profile, URL-Normalisierung, Keychain
internal/output/      table/json/csv/yaml/…
internal/ojson/       JSON mit erhaltener Feldreihenfolge
internal/spec/        commands.json (generiert, per go:embed eingebettet)
spec/openapi-v3.json  aus der Doku extrahierte OpenAPI-Spezifikation
tools/specgen/        Spec herunterladen (fetch) und Befehlstabelle erzeugen (gen)
```

```sh
make test        # Tests gegen einen Mock-Server
make spec        # Spec neu aus der Doku ziehen + Befehle neu generieren
make generate    # nur Befehle neu generieren
```

Befehlsnamen, Verb-Overrides und die Liste destruktiver Aktionen stehen in
`tools/specgen/main.go`. Mit Tenant-Zugang lässt sich die offizielle Spezifikation auch direkt
laden: `epm-cli api-definition download --out-file spec/openapi-v3.yaml`.
