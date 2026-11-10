# epm-cli

Kommandozeilen-Tool für **BeyondTrust Endpoint Privilege Management for Windows and Mac**
(PM Cloud, Management API v3), aufgebaut nach dem Vorbild von
[jamf-cli](https://github.com/Jamf-Concepts/jamf-cli): eine einzelne Go-Binärdatei mit Profilen,
Keychain, mehreren Ausgabeformaten, Dry-Run, Sicherheitsabfragen und MCP-Server für KI-Agenten.

Alle 94 Operationen der v3-API stehen als Befehle bereit. Sie werden aus der OpenAPI-Spezifikation
generiert, die aus der [offiziellen API-Referenz](https://docs.beyondtrust.com/epm-wm/reference/get_v3-accepteddomains)
extrahiert ist.

## Installation

Das Repository ist privat. Installieren kann deshalb nur, wer Lesezugriff auf `droidnik/epm-cli`
hat (GitHub CLI `gh auth login` oder ein Token in `GH_TOKEN`). Go wird **nicht** benötigt.

Jedes Release enthält:

| Plattform | Datei | Inhalt |
|---|---|---|
| macOS | `epm-cli_<v>_darwin_all.pkg` | Installer → `/usr/local/bin/epm-cli` + Shell-Completions |
| macOS | `epm-cli_<v>_darwin_all.zip` / `.tar.gz` | Universal-Binärdatei (Intel + Apple Silicon) |
| Windows | `epm-cli_<v>_windows_amd64.zip` | `epm-cli.exe` (läuft auch auf ARM per Emulation) |
| Linux | `.deb`, `.rpm`, `.apk`, `.tar.gz` | amd64 und arm64 |
| Container | `ghcr.io/droidnik/epm-cli:<v>` | Multi-Arch-Image für CI/CD und Kubernetes |
| alle | `checksums.txt`, `install.sh`, `install.ps1` | SHA-256-Prüfsummen, Installationsskripte |

### macOS

```sh
# Installationsskript (lädt, prüft Checksumme, installiert)
gh api repos/droidnik/epm-cli/contents/install.sh -H "Accept: application/vnd.github.raw" | sh

# oder das Installer-Paket
gh release download -R droidnik/epm-cli -p '*darwin_all.pkg'
sudo installer -pkg epm-cli_*_darwin_all.pkg -target /
```

Das `.pkg` eignet sich für die **Verteilung per Jamf Pro** (Paket hochladen, Policy anlegen).
Ist es nicht signiert, installiert Jamf es trotzdem problemlos. Nur ein Doppelklick im Finder wird
von Gatekeeper blockiert. Eine ZIP aus dem Browser-Download ist unter Quarantäne; dann einmalig
`xattr -d com.apple.quarantine epm-cli` ausführen (entfällt bei signierten Releases, siehe unten).

### Windows

```powershell
# PowerShell, ohne Admin-Rechte: installiert nach %LOCALAPPDATA%\Programs\epm-cli und setzt den PATH
gh api repos/droidnik/epm-cli/contents/install.ps1 -H "Accept: application/vnd.github.raw" | powershell -NoProfile -Command -

# oder lokal / offline (z. B. als Intune-Win32-App mit ZIP + Skript)
powershell -ExecutionPolicy Bypass -File install.ps1 -File .\epm-cli_0.2.0_windows_amd64.zip
```

### Linux

```sh
gh api repos/droidnik/epm-cli/contents/install.sh -H "Accept: application/vnd.github.raw" | sh
# oder Paket:
gh release download -R droidnik/epm-cli -p '*_amd64.deb' && sudo apt install ./epm-cli_*_amd64.deb
gh release download -R droidnik/epm-cli -p '*.x86_64.rpm' && sudo dnf install ./epm-cli-*.x86_64.rpm
```

Optionen der Skripte: Version (`-v v0.2.0` / `-Version`), Zielordner (`-d` / `-InstallDir`),
lokales Archiv (`-f` / `-File`). Ein **Update** ist derselbe Aufruf.

### Docker, CI/CD und Kubernetes

Das Image ist privat in der GitHub Container Registry. Zum Pullen braucht man einen Token mit
`read:packages`: `echo $TOKEN | docker login ghcr.io -u <user> --password-stdin`.
Ohne Profil liest die CLI `EPM_URL`, `EPM_CLIENT_ID` und `EPM_CLIENT_SECRET` aus der Umgebung.

```sh
docker run --rm -e EPM_URL=acme -e EPM_CLIENT_ID -e EPM_CLIENT_SECRET \
  ghcr.io/droidnik/epm-cli:latest computers list --all -o json
```

GitHub Actions:

```yaml
jobs:
  epm:
    runs-on: ubuntu-latest
    container: ghcr.io/droidnik/epm-cli:latest
    env:
      EPM_URL: acme
      EPM_CLIENT_ID: ${{ secrets.EPM_CLIENT_ID }}
      EPM_CLIENT_SECRET: ${{ secrets.EPM_CLIENT_SECRET }}
    steps:
      - run: epm-cli backup --dir backup && epm-cli overview -o json
```

GitLab CI (Entrypoint überschreiben, damit `script:` läuft):

```yaml
epm-backup:
  image: { name: ghcr.io/droidnik/epm-cli:latest, entrypoint: [""] }
  script:
    - epm-cli doctor
    - epm-cli backup --dir backup
  artifacts: { paths: [backup/] }
```

Kubernetes (z. B. nächtliches Backup als CronJob): Image `ghcr.io/droidnik/epm-cli:<v>`,
`args: ["backup", "--dir", "/data"]`, Zugangsdaten per `envFrom: secretRef`. Das Image läuft als
Non-Root-User (UID 10001) und funktioniert mit `readOnlyRootFilesystem`, weil der Token-Cache
unter `/tmp` liegt. `jq` ist im Image enthalten.

### Mit Go / aus dem Quellcode

```sh
GOPRIVATE=github.com/droidnik/* go install github.com/droidnik/epm-cli/cmd/epm-cli@latest
# oder im Klon (Go ≥ 1.26):
make build      # → ./bin/epm-cli
make install    # → $(go env GOPATH)/bin/epm-cli
```

### Neue Version veröffentlichen

```sh
git tag v0.2.0 && git push origin v0.2.0
```

`.github/workflows/release.yml` baut auf einem macOS-Runner mit GoReleaser alle Pakete und das
`.pkg` und erstellt das Release. Ein zweiter Job baut das Container-Image und pusht es. Lokal
ohne Veröffentlichung: `make snapshot` (Pakete nach `dist/`) und `make image` (braucht Docker).

**Signieren und Notarisieren (optional):** Sind die Repository-Secrets `MACOS_CERTS_P12`,
`MACOS_CERTS_PASSWORD`, `CODESIGN_IDENTITY` und `PKG_SIGN_IDENTITY` gesetzt (optional zusätzlich
`NOTARY_KEY_P8`, `NOTARY_KEY_ID`, `NOTARY_ISSUER` für die Notarisierung), werden Binärdatei und
`.pkg` mit einer Developer ID signiert und notarisiert. Ohne Secrets bleiben sie unsigniert.
Details stehen im Kopf von `release.yml` und `scripts/build-pkg.sh`.

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
scripts/              .pkg-Bau, Shell-Completions für Pakete
install.sh/.ps1       Installationsskripte (macOS/Linux, Windows)
Dockerfile            Container-Image
.goreleaser.yaml      Release-Pakete; .github/workflows/ CI und Release
```

```sh
make test        # Tests gegen einen Mock-Server
make spec        # Spec neu aus der Doku ziehen + Befehle neu generieren
make generate    # nur Befehle neu generieren
```

Befehlsnamen, Verb-Overrides und die Liste destruktiver Aktionen stehen in
`tools/specgen/main.go`. Mit Tenant-Zugang lässt sich die offizielle Spezifikation auch direkt
laden: `epm-cli api-definition download --out-file spec/openapi-v3.yaml`.
