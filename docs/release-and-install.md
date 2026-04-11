# Release & Install Guide (PLANNED — NOT YET IMPLEMENTED)

> **Note:** This document describes the planned release pipeline. None of the described artifacts (`.goreleaser.yaml`, release workflows, install scripts) exist yet. Currently Zpit is built from source via `go build`.

Release and installation strategy for Zpit. Install target: `~/.local/bin` (same location as Claude Code).

Repo: https://github.com/zac15987/zpit (public)

## Installation Methods

| Method | Target Audience | Notes |
|--------|----------------|-------|
| Install script (recommended) | All users | Downloads release binary to `~/.local/bin` |
| `go install` | Go developers | Defaults to `$GOPATH/bin`, or set `GOBIN` |
| Scoop bucket | Windows users | Future consideration |
| Homebrew tap | macOS/Linux users | Future consideration |

## 1. GoReleaser Configuration

Cross-compile and publish to GitHub Releases using GoReleaser v2.

### Prerequisites

```bash
go install github.com/goreleaser/goreleaser/v2@latest
```

### `.goreleaser.yaml`

```yaml
version: 2

before:
  hooks:
    - go mod tidy

builds:
  - main: .
    binary: zpit
    env:
      - CGO_ENABLED=0
    goos: [linux, windows, darwin]
    goarch: [amd64, arm64]
    ldflags:
      - -s -w
      - -X main.version={{.Version}}
      - -X main.commit={{.ShortCommit}}
      - -X main.date={{.Date}}

archives:
  - formats: [tar.gz]
    format_overrides:
      - goos: windows
        formats: [zip]
    name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    files:
      - scripts/setup-hooks.sh
      - agents/*
      - docs/agent-guidelines.md
      - docs/code-construction-principles.md

checksum:
  name_template: checksums.txt

changelog:
  sort: asc
  filters:
    exclude:
      - "^docs:"
      - "^test:"
      - "^chore:"
```

### Version Information

Add version variables to `main.go` so both GoReleaser ldflags and `go install` report the correct version:

```go
var (
    version = "dev"
    commit  = "none"
    date    = "unknown"
)

func getVersion() string {
    if version != "dev" {
        return version
    }
    if info, ok := debug.ReadBuildInfo(); ok {
        return info.Main.Version
    }
    return "dev"
}
```

## 2. Release Workflow

```bash
# Tag the release
git tag v0.1.0
git push origin v0.1.0

# Local release (requires GITHUB_TOKEN)
export GITHUB_TOKEN="your-token"
goreleaser release --clean

# Dry run (no upload)
goreleaser release --snapshot --clean
```

## 3. Automated Release (GitHub Actions)

`.github/workflows/release.yml`:

```yaml
name: Release
on:
  push:
    tags: ["v*"]

jobs:
  release:
    runs-on: ubuntu-latest
    permissions:
      contents: write
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - uses: goreleaser/goreleaser-action@v6
        with:
          version: "~> v2"
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

## 4. Install Script

### Linux / macOS (`install.sh`)

Usage: `curl -sSfL https://raw.githubusercontent.com/zac15987/zpit/main/install.sh | sh`

```bash
#!/bin/sh
set -eu

REPO="zac15987/zpit"
BINARY="zpit"
INSTALL_DIR="${HOME}/.local/bin"

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

VERSION="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
  | grep '"tag_name"' | sed -E 's/.*"v?([^"]+)".*/\1/')"

FILENAME="${BINARY}_${VERSION}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/v${VERSION}/${FILENAME}"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "Downloading ${BINARY} v${VERSION} for ${OS}/${ARCH}..."
curl -fsSL "$URL" -o "${TMP}/${FILENAME}"
tar xzf "${TMP}/${FILENAME}" -C "$TMP"

mkdir -p "$INSTALL_DIR"
mv "${TMP}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
chmod +x "${INSTALL_DIR}/${BINARY}"

echo "Installed ${BINARY} to ${INSTALL_DIR}/${BINARY}"

case ":${PATH}:" in
  *":${INSTALL_DIR}:"*) ;;
  *) echo "WARNING: ${INSTALL_DIR} is not in PATH. Add it to your shell profile." ;;
esac
```

### Windows (`install.ps1`)

Usage: `irm https://raw.githubusercontent.com/zac15987/zpit/main/install.ps1 | iex`

```powershell
$ErrorActionPreference = "Stop"

$Repo = "zac15987/zpit"
$Binary = "zpit"
$InstallDir = "$env:USERPROFILE\.local\bin"

$Arch = if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }

$Release = Invoke-RestMethod "https://api.github.com/repos/$Repo/releases/latest"
$Version = $Release.tag_name -replace '^v', ''
$Filename = "${Binary}_${Version}_windows_${Arch}.zip"
$Url = "https://github.com/$Repo/releases/download/v$Version/$Filename"

$Tmp = Join-Path ([System.IO.Path]::GetTempPath()) "zpit-install"
New-Item -ItemType Directory -Force -Path $Tmp | Out-Null

Write-Host "Downloading $Binary v$Version..."
Invoke-WebRequest -Uri $Url -OutFile "$Tmp\$Filename"
Expand-Archive "$Tmp\$Filename" -DestinationPath $Tmp -Force

New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
Move-Item "$Tmp\$Binary.exe" "$InstallDir\$Binary.exe" -Force
Remove-Item $Tmp -Recurse -Force

# Add to PATH if not present
$UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($UserPath -notlike "*$InstallDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$UserPath;$InstallDir", "User")
    Write-Host "Added $InstallDir to user PATH. Restart your terminal to use."
}

Write-Host "Installed $Binary to $InstallDir\$Binary.exe"
```

## 5. `go install` (Go Developers)

Public GitHub repo, works directly:

```bash
go install github.com/zac15987/zpit@latest
```

Installs to `$GOPATH/bin` by default. To install to `~/.local/bin`:

```bash
go env -w GOBIN="$HOME/.local/bin"
go install github.com/zac15987/zpit@latest
```

Windows:
```powershell
go env -w GOBIN="$env:USERPROFILE\.local\bin"
go install github.com/zac15987/zpit@latest
```

## 6. Future Considerations

- **Scoop bucket**: Create a `scoop-zpit` repo; GoReleaser can auto-generate the manifest
- **Homebrew tap**: Create a `homebrew-tap` repo; GoReleaser auto-pushes the formula
- **winget**: Heavier process; evaluate when user base grows
- **Auto-update prompt**: Check GitHub latest release on TUI startup and notify the user
