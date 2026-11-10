# Autonomous Completion of Deferred Correction Validation v0.6.0

* **Author:** tamara1031
* **Status:** Proposed
* **Date:** 2026-11-11

---

## Overview

This design finishes every safe, independent v0.6.0 validation branch, then stops only at facts automation cannot create. It does not promise fourteen verified requirements. It produces one of two final states:

* `original_14_of_14_verified`: every original criterion has evidence, including original photo2_2 truth, exact weight scope, and owner iPhone/PC operations.
* `automation_queue_exhausted`: all safe work is done, and each remaining external prerequisite has one revision-keyed resume trigger.

The current baseline is 6/14 verified. Planning does not change that count.

## The Problem

The existing evidence is useful but mixed: six requirements are verified, partial detection/mesh results exist, and several prerequisites are human or publisher facts. Blocking all work on Phase65 wastes independent diagnostics. Treating partial evidence as completion would be worse: source code is not a detector binary, MIT source text is not checkpoint permission, generated truth is not original photo2_2 truth, and automated browser tests are not owner acceptance.

The plan therefore separates task-specific execution gates from formal phase completion. Read-only research, hash checks, retained replay, and product regression may continue from their own exact input hashes. The formal dependency remains Phase65 → Phases66/67 → Phase68.

## Goals

* Exhaust all authorized local validation without new scans, messages, deployment, third-party builds, or invented evidence.
* Preserve all fourteen original requirements and the six verified rows unchanged unless their hashes fail validation.
* Run no more than one heavy model benchmark across the whole descendant tree.
* Leave exact external prerequisites and replay instructions when automation stops.
* Let only the root write canonical ledgers, shared GSD state, requirement checkboxes, or git history.

## Non-Goals

* No custom correction, detector, warp, shadow estimator, training, exporter, Kotlin/Python port, source build, or emulator.
* No model substitution to satisfy an FSENet-specific provenance criterion.
* No external message, new physical scan, owner-content viewing/upload, release, or deployment.
* No claim that a measured quality failure is a capability pass. A completed measurement and a passing result are separate fields.
* No force-add of `.planning`; preserve the existing `.impeccable/` tree.

## Current Requirement Contract

Unchanged verified rows: `ENV-01`, `ENV-02`, `TRUTH-01`, `TRUTH-03`, `VERIFY-03`, `ACCEPT-03`.

| Open ID | Autonomous work | Material completion gate |
|---|---|---|
| TRUTH-02 | Reconcile nine identities, hashes, availability, and restoration | Independent photo2_2 digits/thin lines plus three-document truth |
| VERIFY-01 | Check official binary/source compatibility; reuse mask diagnostics | Supported complete FairScan runtime and original-photo labels |
| VERIFY-02 | Revalidate no-op/DocAligner/synthetic comparisons | Same-input original-photo truth for the original scope |
| VERIFY-04 | Revalidate retained mesh/old output/no-op stage evidence | photo2_2 content truth and evidence-backed cause separation |
| ILL-01 | Build exact artifact matrix once | Mandatory FSENet checkpoint and linked-export terms, identity, and parity |
| ILL-02 | Preflight an eligible artifact, official invocation, and existing metrics | Eligible supported model plus complete existing metric path |
| ACCEPT-01 | Prepare a private-content-safe operation packet | Same-original iPhone input and actual owner iPhone/PC operations |
| ACCEPT-02 | Run existing product invariants and independent qualification join | Completed product decision; only fully qualified candidates may integrate |

## Critical User Journeys

### Continue independent work without lying about Phase65

* **Given:** a branch has exact retained input hashes, but original photo2_2 labels are missing.
* **When:** the coordinator schedules retained replay, runtime checks, or product regression.
* **Then:** the branch writes measured or waiting evidence; formal phase completion remains blocked by its original dependency.

### Resume after external input changes

* **Given:** a receipt is `waiting_external_*` and stores an evidence revision.
* **When:** labels, publisher evidence, an official binary, or owner observations arrive with a changed revision.
* **Then:** only the dependent task resumes. Unrelated diagnostics and successful receipts are not repeated.

### Produce the final audit

* **Given:** every autonomous receipt exists and owner evidence is either observed or honestly unavailable.
* **When:** the root joins the fourteen original IDs.
* **Then:** all rows retain measured-pass, measured-fail, not-run, or waiting state; only all fourteen complete criteria yield `original_14_of_14_verified`.

## Technical Plan

```text
root orchestrator (canonical ledger/state/git only)
└─ plan coordinator (scheduling, validation, no shared-ledger commit)
   ├─ worker A (read/hash/replay; unique output directory; no spawn)
   └─ worker B (independent read/metrics; unique output directory; no spawn)

Task-specific DAG:
65 input reconciliation ───────┐
66 runtime check + replay ─────┼─> 68 product regression/qualification
67 artifact eligibility ─> 67 measurement (global heavy semaphore) ─┘
68 owner packet ─> root provisional fourteen-row audit ─> actual owner operation ─> root audit refresh

Formal completion DAG: 65 -> 66 and 67 -> 68
```

The global live-agent limit is four: root, coordinator, and at most two workers. Grandchildren cannot spawn. The coordinator waits for each child to reach a terminal state and rejects a parent summary that lacks child terminal evidence. Research and file/hash I/O may run in parallel. Model benchmarks, shared-file writes, source edits, and runtime operations are serialized. Only one heavy benchmark may run across all descendants.

Each execution uses a unique output directory. Temporary evidence must pass an availability/hash check and include a restoration recipe before replay. No plan assumes `/tmp` survived. Workers return proposals or private shards; the root persists shared receipts.

## Decision and Retry Limits

* One automatic retry is allowed only for a classified transient read or child-process failure.
* Deterministic missing input, unsupported binary, failed threshold, unresolved rights, or semantic test failure is not retried automatically.
* External surveys are not repeated on later autonomous calls until their evidence revision changes.
* FairScan official Kotlin source gets one read-only compatibility check. Without a supported binary, status is `waiting_external_implementation`; there is no port, build, or emulator fallback.
* ILL-01 completes only when its mandatory FSENet/export predicates are explicitly resolved. A separately eligible artifact may support ILL-02 but cannot complete ILL-01.
* Illumination measurement uses existing standard/installed metrics. Missing SSIM or independent region comparison becomes `waiting_external_metric_implementation`; no metric algorithm is invented.

## Alternatives Considered

### Block all work until original labels arrive

* **Benefit:** simple phase ordering.
* **Failure:** prevents independent runtime, replay, eligibility, and regression evidence.
* **Decision:** Rejected by author instruction; task-specific hashes may unblock unrelated work while formal dependencies remain.

### Complete capabilities from partial or substitute evidence

* **Benefit:** reaches a larger counter quickly.
* **Failure:** changes the original criteria and confuses mask/source/synthetic/browser evidence with full runtime, original truth, rights, or owner observation.
* **Decision:** Rejected by author instruction.

### Build missing runtimes or metric algorithms

* **Benefit:** could create an executable path locally.
* **Failure:** violates the official-distribution and no-custom-implementation boundary and creates unreviewed parity risk.
* **Decision:** Rejected by author instruction; wait for an official implementation or separately reviewed output-only glue contract.

## Non-Functional Requirements

Private owner content stays local and is represented only by hashes or opaque local IDs. No output contains pixels, private paths, credentials, or external uploads. Existing CPU2/30s/temp256MiB limits stay fixed; product RSS1GiB and installed256MiB remain separate from diagnostic RSS2GiB. The current UVDoc installed-size failure is preserved. Existing product builds/tests are allowed; third-party source/runtime builds are not.

## Detailed Implementation Plan

### Existing files read, executed, or root-updated

* `docs/design/research/correction-validation-v060/summary.json` — canonical fourteen-row ledger; root updates only after final audit validation.
* `docs/design/deferred-correction-validation-v060.md` — human-readable canonical report; root projects the same final statuses.
* `docs/design/research/correction-paired-v060/summary.json` — fixed six pairs/six clean controls and no-op baseline.
* `docs/design/research/correction-detection-v060/summary.json` — retained mask/no-op/DocAligner diagnostics.
* `scripts/eval/correction/README.md` and existing runner/processor — execution contract; no second runner.
* `internal/documents/photo_correction_test.go` and `internal/documenthttp/photo_browser_integration_linux_test.go` — existing product invariants with retained runtime paths.

### Proposed evidence files

* `docs/design/research/correction-autonomous-v060/phase65-inputs.json` and `phase65-truth-gate.json` — availability/restoration plus TRUTH-02 decision.
* `docs/design/research/correction-autonomous-v060/phase66-runtime.json`, `phase66-comparison.json`, and `phase66-disposition.json` — supported path, comparisons, and four requirement rows.
* `docs/design/research/correction-autonomous-v060/phase67-eligibility.json`, `phase67-resume.json`, `phase67-measurement-preflight.json`, and `phase67-measurement.json` — exact artifact rights/provenance and conditional measurement.
* `docs/design/research/correction-autonomous-v060/phase68-runtime-preflight.json`, `phase68-browser-receipt.json`, `phase68-regression.json`, `phase68-qualification.json`, and `phase68-candidate-shared-path.json` — current-source product evidence, preserved unique browser receipt, candidate gate, and conditional candidate-bound existing-path verification.
* `docs/design/research/correction-autonomous-v060/phase68-owner-packet.md`, `phase68-owner-evidence.json`, and `final-audit.json` — owner steps, actual observations, and the fourteen-row proposal.

The executable prompts are `65-02-PLAN.md`, `66-01-PLAN.md`, `67-01-PLAN.md`, `67-02-PLAN.md`, `68-01-PLAN.md`, and `68-02-PLAN.md` in their existing phase directories. No duplicate phase directory is created.

## Verification and Handoff

Each plan validates its JSON shape and fail-closed status. The existing focused Go/browser command runs only after retained runtime path/hash preflight. The final audit must contain the fourteen IDs in original order, at least the six unchanged verified rows, and a status consistent with every `complete` field.

Execution can start immediately with Phase65 input reconciliation while the coordinator also schedules independent Phase66 runtime checks, Phase67 eligibility, and Phase68 runtime preflight under the four-agent cap. Material external prerequisites are original photo2_2 truth, a supported FairScan binary, explicit FSENet/export terms or rights-holder evidence, any missing compatible metric implementation after an eligible output exists, and same-original owner iPhone/PC operations.

This document is a draft for fresh-context EGM review. It does not claim that review has occurred.

---

*Drafted with EGM*
