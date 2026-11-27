#!/bin/bash
# Builds windows/InputGuard.exe on any computer with Go 1.25 or later, and
# checks it. Nothing here runs the .exe or blocks any input.
set -euo pipefail
cd "$(dirname "$0")"
VERSION=0.1.0
export CGO_ENABLED=0

echo "== Format and vet"
unformatted=$(gofmt -l .)
if [ -n "$unformatted" ]; then echo "Run gofmt on: $unformatted"; exit 1; fi
go vet ./core ./art ./tools/...
# The hooks read Windows-owned structures from callback pointers, which vet's
# unsafe-pointer check can't tell apart from Go memory.
GOOS=windows GOARCH=amd64 go vet -unsafeptr=false ./...

echo "== Tests (passphrase, prompt, five clicks, input rules, file, recovery helper, icons)"
go test -count=1 ./core ./art

echo "== Windows resources (manifest, icon, version)"
go run ./tools/mkres -version "$VERSION" -manifest app/InputGuard.manifest -o app/rsrc_windows_amd64.syso

echo "== InputGuard.exe"
GOOS=windows GOARCH=amd64 go build -trimpath -buildvcs=false \
  -ldflags "-H windowsgui -s -w -buildid=" -o InputGuard.exe ./app
go run ./tools/checkexe InputGuard.exe app/InputGuard.manifest

size=$(wc -c < InputGuard.exe | tr -d ' ')
hash=$(shasum -a 256 InputGuard.exe 2>/dev/null | cut -d' ' -f1 || sha256sum InputGuard.exe | cut -d' ' -f1)
echo
echo "Built InputGuard.exe, version $VERSION, $size bytes"
echo "SHA-256 $hash"
echo "On the PC, PowerShell's Get-FileHash .\\InputGuard.exe should show the same."
