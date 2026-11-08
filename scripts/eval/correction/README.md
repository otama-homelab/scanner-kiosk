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

## Pinned local candidate build

The repository also contains evaluation-only arm implementations:

- `no_correction.py` copies the exact immutable input;
- `tesseract_internal.py` runs the pre-existing pinned Tesseract 5.5.4 musl runtime and extracts its processed image;
- `leptonica_candidate.c` builds Leptonica 1.87.0 from the pinned source archive and runs background normalization/deskew;
- `opencv_candidate.cpp` uses installed OpenCV 4.6.0 contour bounds and a perspective crop.

Build private helpers:

```sh
scripts/eval/correction/build_candidates.sh /private/eval/correction
```

Create a correction-only view of the frozen public catalog:

```sh
scripts/eval/correction/prepare_catalog.py \
  --catalog scripts/fixtures/evaluation/catalog.json \
  --output /private/eval/correction/correction-catalog.json
```

Create candidate configs with the existing Tesseract 5.5.4 runtime:

```sh
scripts/eval/correction/prepare_candidates.py \
  --root /private/eval/correction \
  --tesseract-runtime /absolute/pinned/tesseract-5.5.4 \
  --output /private/eval/correction/candidates
```

The generated configs contain hashes and local absolute paths; do not commit them. Run `benchmark` for every candidate/fixture, then `analyze-corrections` and `compare`.

The 2026-11-08 local synthetic baseline measured 10 correction classes with cold 1/warm 2. All 40 candidate/fixture quality rows retained every oracle edge/content region. Median warm p50 values were approximately 142ms no-correction, 134ms OpenCV, 294ms Leptonica, and 1207ms Tesseract-internal. Peak RSS was approximately 54/219/99/268MiB respectively. Tesseract-internal's processed image reduced foreground pixel count on several cases and is diagnostic only. No private household corpus or OCR improvement comparison exists yet, so this evidence does not adopt a production correction engine.
