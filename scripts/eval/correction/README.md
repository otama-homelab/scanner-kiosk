# Correction candidate evaluation

Phase 44 owns candidate execution, normalized artifacts, cold/warm benchmarks and receipts. Phase 45 adds only correction quality analysis; do not add another runner.

## Required arms

| Candidate ID | Runtime | Required pins |
| --- | --- | --- |
| `correction_none` | copy input to PNG | executable SHA256, source/license, version |
| `correction_tesseract_internal` | Tesseract 5.5.4 internal image | pinned binary/models SHA256, Apache-2.0 and model licenses |
| `correction_leptonica` | Leptonica 1.87.0 deskew/background | pinned helper/container SHA256, source/license |
| `correction_opencv` | OpenCV 4.6.0 contour/perspective | pinned helper/container SHA256, Apache-2.0 and build dependency pins |

Each arm is an administrator-supplied executable in a normal `CandidateConfig`. It must use the existing `scanner-eval run` or `benchmark` protocol and emit a `correction` PNG with:

```text
crop_quad, rotation_degrees, output_width, output_height,
confidence_kind, confidence_value, fallback_reason
```

No candidate path, fake executable hash or private document belongs in source.

## Analyze

```sh
go run ./cmd/scanner-eval analyze-corrections \
  --catalog scripts/fixtures/evaluation/catalog.json \
  --oracles scripts/fixtures/evaluation/correction-oracles.json \
  --artifacts /private/eval/runs/none/artifact-catalog.json \
  --artifacts /private/eval/runs/tesseract/artifact-catalog.json \
  --artifacts /private/eval/runs/leptonica/artifact-catalog.json \
  --artifacts /private/eval/runs/opencv/artifact-catalog.json
```

The analyzer reports transform validity, edge/content region retention, foreground counts and measurement completeness. It does not rank provider-specific confidence values or declare adoption.

The current host has OpenCV 4.6.0 libraries/headers, Leptonica 1.82.0 runtime, and no Tesseract CLI. The exact four-arm run remains unavailable until an operator supplies pinned helpers/containers; helper tests prove only the analyzer contract.
