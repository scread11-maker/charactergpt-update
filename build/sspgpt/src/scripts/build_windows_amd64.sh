#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
out=dist/windows_amd64
rm -rf "$out"
mkdir -p "$out/core" "$out/Plug"
export GOOS=windows GOARCH=amd64 CGO_ENABLED=0
build_gui() { go build -trimpath -ldflags='-s -w -H=windowsgui' -o "$2" "$1"; }
build_gui ./cmd/bridge "$out/core/CharacterGPTBridge.exe"
build_gui ./cmd/memoryservice "$out/core/CharacterGPTMemoryService.exe"
build_gui ./cmd/runtime "$out/core/CharacterGPTRuntime.exe"
build_gui ./cmd/touchprogress "$out/core/CharacterGPTTouchProgress.exe"
build_gui ./cmd/link "$out/Plug/CharacterGPTLink.exe"
python scripts/verify_windows_gui.py \
  "$out/core/CharacterGPTRuntime.exe" \
  "$out/Plug/CharacterGPTLink.exe"
