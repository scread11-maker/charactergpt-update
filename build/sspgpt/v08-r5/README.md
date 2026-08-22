# SSPGPT v0.8 r5 EXE repair workspace

This branch is the Go 1.27 repair baseline for the five r5 executables described by `SSPGPT_v0.8_r5_EXE_Development_Handoff_2026-08-23.zip`.

## Status

Infrastructure only. No EXE behavior repair has started on this branch.

Canonical implementation baseline from the handoff:

- module: `sspgpt/v08`
- Go baseline: `1.27.0`
- Runtime: `cmd/runtime`
- Bridge: `cmd/bridge`
- MemoryService: `cmd/memoryservice`
- BehaviorService: `cmd/behaviorservice`
- TouchProgress: `cmd/touchprogress`

The current-session approved Emily-conformance YAYA overlay for `ghost/master/sspgpt_core.dic` and `ghost/master/sspgpt_touch.dic` belongs to the working baseline. No Go behavior changes are authorized by this setup commit.

## Baseline gate

The companion workflow is intentionally a repair/build gate, not a release-forging workflow. When the r5 handoff source is materialized at `build/sspgpt/v08-r5/source`, it requires:

1. exact Go `1.27.0` and `module sspgpt/v08`;
2. `go test -count=1 ./...`;
3. `go vet ./...`;
4. cross-build exactly five Windows GUI executables for amd64 and 386;
5. PE machine/subsystem checks;
6. Go build metadata proving `go1.27.0` and the correct five `cmd/*` entry points.

It does not forge a NAR and does not perform any functional repair.

## Handoff authority order

When repairs begin, follow the r5 handoff order:

1. `NEXT_CONVERSATION_HANDOFF.md` user overrides and review corrections;
2. frozen canonical contracts;
3. `SSPGPT_v0.8_Cold_Generation_Outcome_Acceptance_Contract_R1_2026-08-22.md`;
4. `ACCEPTANCE_REPORT_2026-08-22.md` and source tests;
5. older review references only after applying the handoff corrections.

The previous repository workflow that compiled `cmd/memory`, `cmd/behavior`, and `cmd/touch` is obsolete for r5 and must not be used as the repair baseline.
