# SSPGPT v0.8 First Alchemy development line

This directory is the active fresh v0.8 implementation generated from the frozen First Alchemy contract set dated 2026-08-21. It is not a port of the v0.7 process topology.

## Toolchain

- Go 1.27.0
- module `sspgpt/v08`
- Windows amd64 primary target
- Windows 386 compatibility target
- `CGO_ENABLED=0`
- SSPGPT background services use the Windows GUI subsystem so SSP does not spawn console windows

## Canonical development input

The base v0.8 source snapshot is reconstructed from the versioned text chunks referenced by `.github/workflows/sspgpt-v08-go127.yml` and is pinned by SHA-256. `overlays/` contains the small canonical corrections discovered during Go 1.27 acceptance; the effective source must pass `go mod tidy -diff`, frozen-contract tests, `go vet`, and the v0.8 authority audit before any Windows binary is produced.

Binary SSP host assets (`yaya.dll` and authored shell artwork) are deliberately not transported through this source CI path. Release assembly uses the independently pinned host seed identified by `HOST_SEED_SHA256.txt`, then combines that host with the CI-proven SSPGPT service binaries. This keeps binary transport concerns separate from cognition-source provenance.

Live `profile/**`, Plug/MCP, credentials, logs, audit state and other mutable/private state are not First Alchemy public-release inputs.

The existing `v07/` tree and old `build/sspgpt/src` remain historical/regression material only. They are not inputs to the v0.8 workflow.

## CI acceptance path

`.github/workflows/sspgpt-v08-go127.yml` verifies exact Go 1.27.0/module identity, base-source integrity, canonical overlays, frozen-contract tests, `go vet`, v0.8 authority boundaries, then cross-builds exactly five SSPGPT services for Windows amd64 and 386. It verifies PE architecture plus GUI subsystem, inspects embedded Go build provenance, records SHA-256 hashes, and uploads provenance artifacts.

The complete Ghost/NAR is assembled only after these service artifacts pass, using the pinned SSP host seed and the v0.8 release verifier.
