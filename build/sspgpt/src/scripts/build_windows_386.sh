#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
out=dist/windows386
rm -rf "$out"
mkdir -p "$out"
export GOOS=windows GOARCH=386 GO386=sse2 CGO_ENABLED=0
build() { go build -trimpath -ldflags='-H=windowsgui -s -w' -o "$out/$1" "$2"; }
build CharacterGPTRuntime.exe ./cmd/runtime
build CharacterGPTMemoryService.exe ./cmd/memoryservice
build CharacterGPTBridge.exe ./cmd/bridge
build CharacterGPTTouchProgress.exe ./cmd/touchprogress
python3 scripts/verify_windows_gui.py "$out"/*.exe
