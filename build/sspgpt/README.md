# SSPGPT reproducible build source

This directory is the reproducible build area for the SSPGPT v0.7 stable line.

- The public/update channel remains `v07/` and is not renamed by internal fix numbers.
- The build toolchain baseline is Go 1.26.5 (`go 1.26.0`, `toolchain go1.26.5`).
- CI validates unit tests, `go vet`, architecture audit, YAYA audit, and Windows cross-builds.
- Build infrastructure is intentionally separate from `v07/`; CI changes do not publish an SSP update by themselves.
- MCP/Plug redesign is not part of the Go 1.26.5 migration baseline.
