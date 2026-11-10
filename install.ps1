<#
.SYNOPSIS
  Installs epm-cli on Windows from the (private) GitHub release. No admin rights needed.

.DESCRIPTION
  Downloads epm-cli_<version>_windows_amd64.zip, verifies its SHA-256 checksum, installs
  epm-cli.exe to %LOCALAPPDATA%\Programs\epm-cli and adds that folder to the user PATH.

  Authentication (the repository is private), first match wins:
    1. GitHub CLI "gh" that is logged in (gh auth login)
    2. $env:GH_TOKEN or $env:GITHUB_TOKEN with read access to the repository

.EXAMPLE
  powershell -ExecutionPolicy Bypass -File install.ps1
.EXAMPLE
  .\install.ps1 -Version v0.2.0 -InstallDir C:\Tools\epm-cli
.EXAMPLE
  .\install.ps1 -File .\epm-cli_0.2.0_windows_amd64.zip   # offline / Intune package
.EXAMPLE
  gh api repos/droidnik/epm-cli/contents/install.ps1 -H "Accept: application/vnd.github.raw" | powershell -NoProfile -Command -
#>
[CmdletBinding()]
param(
  [string]$Version = $(if ($env:EPM_CLI_VERSION) { $env:EPM_CLI_VERSION } else { 'latest' }),
  [string]$InstallDir = $(if ($env:EPM_CLI_INSTALL_DIR) { $env:EPM_CLI_INSTALL_DIR } else { Join-Path ([string]$env:LOCALAPPDATA) 'Programs\epm-cli' }),
  [string]$Repo = $(if ($env:EPM_CLI_REPO) { $env:EPM_CLI_REPO } else { 'droidnik/epm-cli' }),
  [string]$File,
  [switch]$NoPath
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'   # Invoke-WebRequest is very slow with a progress bar

function Fail([string]$msg) { [Console]::Error.WriteLine("error: $msg"); exit 1 }

$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("epm-cli-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
  # ------------------------------------------------------------ download
  if ($File) {
    if (-not (Test-Path $File)) { Fail "archive not found: $File" }
    Copy-Item $File $tmp
    $name = Split-Path $File -Leaf
    $sums = Join-Path (Split-Path (Resolve-Path $File) -Parent) 'checksums.txt'
    if (Test-Path $sums) { Copy-Item $sums $tmp }
  } else {
    $pattern = 'epm-cli_*_windows_amd64.zip'
    $gh = Get-Command gh -ErrorAction SilentlyContinue
    $ghOk = $false
    if ($gh) { & gh auth status *> $null; $ghOk = ($LASTEXITCODE -eq 0) }
    if ($ghOk) {
      Write-Host "Downloading $pattern ($Version) from $Repo via gh ..."
      $ghArgs = @('release', 'download')
      if ($Version -ne 'latest') { $ghArgs += $Version }
      $ghArgs += @('-R', $Repo, '-p', $pattern, '-p', 'checksums.txt', '-D', $tmp)
      & gh @ghArgs
      if ($LASTEXITCODE -ne 0) { Fail "download failed - do you have access to $Repo and does release '$Version' exist?" }
    } else {
      $token = if ($env:GH_TOKEN) { $env:GH_TOKEN } else { $env:GITHUB_TOKEN }
      if (-not $token) { Fail "the repository is private: log in with 'gh auth login' or set `$env:GH_TOKEN" }
      $headers = @{ Authorization = "Bearer $token"; Accept = 'application/vnd.github+json'; 'User-Agent' = 'epm-cli-installer' }
      $api = "https://api.github.com/repos/$Repo/releases"
      $relUrl = if ($Version -eq 'latest') { "$api/latest" } else { "$api/tags/$Version" }
      Write-Host "Downloading $pattern ($Version) from $Repo via the GitHub API ..."
      try { $release = Invoke-RestMethod -Uri $relUrl -Headers $headers }
      catch { Fail "cannot read release '$Version' of $Repo (token valid? repo access?)" }
      foreach ($want in @($pattern, 'checksums.txt')) {
        $asset = $release.assets | Where-Object { $_.name -like $want } | Select-Object -First 1
        if (-not $asset) { Fail "release has no asset matching $want" }
        $h = $headers.Clone(); $h.Accept = 'application/octet-stream'
        Invoke-WebRequest -Uri $asset.url -Headers $h -OutFile (Join-Path $tmp $asset.name)
      }
    }
    $name = (Get-ChildItem $tmp -Filter $pattern | Select-Object -First 1).Name
    if (-not $name) { Fail 'no matching archive downloaded' }
  }

  # -------------------------------------------------------------- verify
  $sumFile = Join-Path $tmp 'checksums.txt'
  if (Test-Path $sumFile) {
    $line = Get-Content $sumFile | Where-Object { ($_ -split '\s+')[1] -eq $name } | Select-Object -First 1
    if (-not $line) { Fail "$name is not listed in checksums.txt" }
    $want = ($line -split '\s+')[0].ToLower()
    $got = (Get-FileHash -Algorithm SHA256 (Join-Path $tmp $name)).Hash.ToLower()
    if ($want -ne $got) { Fail "checksum mismatch for $name" }
    Write-Host 'Checksum OK'
  } elseif (-not $File) {
    Fail 'checksums.txt missing from release'
  } else {
    Write-Warning "no checksums.txt next to $File - skipping verification"
  }

  # ------------------------------------------------------------- install
  $extract = Join-Path $tmp 'x'
  Expand-Archive -Path (Join-Path $tmp $name) -DestinationPath $extract -Force
  $exe = Join-Path $extract 'epm-cli.exe'
  if (-not (Test-Path $exe)) { Fail 'archive does not contain epm-cli.exe' }
  New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
  Copy-Item $exe (Join-Path $InstallDir 'epm-cli.exe') -Force
  Get-ChildItem $extract -Filter 'README.md' | Copy-Item -Destination $InstallDir -Force

  $isWin = ($env:OS -eq 'Windows_NT')
  if ($isWin) {
    $version = & (Join-Path $InstallDir 'epm-cli.exe') --version
    Write-Host "Installed $version -> $InstallDir\epm-cli.exe"
  } else {
    Write-Host "Installed epm-cli.exe -> $InstallDir (not running it: not on Windows)"
  }

  if ($isWin -and -not $NoPath) {
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $parts = @($userPath -split ';' | Where-Object { $_ })
    if ($parts -notcontains $InstallDir) {
      [Environment]::SetEnvironmentVariable('Path', (($parts + $InstallDir) -join ';'), 'User')
      $env:Path = "$env:Path;$InstallDir"
      Write-Host "Added $InstallDir to your user PATH (open a new terminal to pick it up)."
    }
  }
  Write-Host 'Next: epm-cli config add <name>   (see epm-cli --help)'
}
finally {
  Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
