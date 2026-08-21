# SSPGPT v0.8 First Alchemy development line

This directory is the active fresh v0.8 implementation generated from the frozen First Alchemy contract set dated 2026-08-21. It is not a port of the v0.7 process topology.

## Toolchain

- Go 1.27.0
- module `sspgpt/v08`
- Windows amd64 primary target
- Windows 386 compatibility target where the Go toolchain permits it
- `CGO_ENABLED=0`
- SSPGPT background services are linked with the Windows GUI subsystem so SSP does not spawn console windows

## Development inputs

`source.zip` is the commit-pinned fresh v0.8 Go/YAYA/config/test source snapshot. Binary host material that cognition development must not rewrite (`yaya.dll` and the shell artwork) is isolated in `assets/host_seed.zip` and pinned by `HOST_SEED_SHA256.txt`.

CI expands the source into a clean temporary work tree before every test/build. Live `profile/**`, Plug/MCP, credentials, logs, audit state and other mutable/private state are not release inputs.

The existing `v07/` tree and old `build/sspgpt/src` remain historical/regression material only. They are not inputs to the v0.8 workflow.

## CI acceptance path

`.github/workflows/sspgpt-v08-go127.yml` verifies exact Go 1.27.0/module identity, archive integrity, unit/frozen-contract tests, `go vet`, v0.8 authority boundaries, cross-builds exactly five SSPGPT services, checks Windows PE architecture plus GUI subsystem, inspects embedded Go build provenance, forges a clean public NAR, rejects private/deferred content, and uploads amd64/386 acceptance artifacts.
