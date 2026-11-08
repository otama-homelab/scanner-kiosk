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
  --catalog /private/eval/correction/scripts/fixtures/evaluation/correction-catalog.json \
  --oracles scripts/fixtures/evaluation/correction-oracles.json \
  --artifacts /private/eval/correction/runs/none/fixture_a4_ja_en_001/repetitions/001-cold/artifact-catalog.json \
  --benchmark /private/eval/correction/runs/none/fixture_a4_ja_en_001/benchmark.json \
  # Repeat --artifacts once per fixture/candidate and --benchmark for every run.
```

The analyzer maps every foreground pixel in each oracle region through the declared crop and rotation into the decoded correction PNG. It reports the source region, original/mapped/retained pixel counts and retention ratio. Quality remains under `quality`; cold/warm timings, peak RSS, sampled disk, failure, source/license pins and operator steps appear only under `resources`.

Every resource row must match fixture ID/SHA256, candidate ID/version, executable SHA256, candidate-config SHA256, original SHA256 and correction SHA256. The benchmark sample's artifact catalog must resolve beneath the summary directory. A missing, mismatched, traversed or hash-conflicting receipt makes the comparison incomplete. It does not rank provider-specific confidence values or declare adoption.

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

Create a self-contained correction-only view of the frozen public catalog. The script copies the one file-backed public recipe into the private run tree so generated fixture resolution does not depend on the source checkout:

```sh
scripts/eval/correction/prepare_catalog.py \
  --catalog scripts/fixtures/evaluation/catalog.json \
  --output /private/eval/correction/scripts/fixtures/evaluation/correction-catalog.json
```

Create candidate configs. Pass the already materialized Tesseract 5.5.4 runtime when it is available; without `--tesseract-runtime`, the script emits only the three locally buildable arms and does not fabricate the unavailable arm:

```sh
# Include the runtime flag only when the pinned Tesseract 5.5.4 runtime exists.
scripts/eval/correction/prepare_candidates.py \
  --root /private/eval/correction \
  --tesseract-runtime /absolute/pinned/tesseract-5.5.4 \
  --output /private/eval/correction/candidates
```

The generated configs contain hashes and local absolute paths; do not commit them. Run cold 1/warm 2 `benchmark` for every available candidate/fixture. Retain each run's `artifact-catalog.json`, repetition receipts and `benchmark.json` under the private root, then pass one cold artifact catalog and every `benchmark.json` to `analyze-corrections`. Do not copy private catalog/config/run paths into source receipts.

The former geometry-only 40/40 result is superseded. The decoded-pixel run and explicit unavailable arm are recorded in `.planning/phases/45-scan/45-CORRECTION-COMPARISON.md`. No private household corpus or OCR improvement comparison exists, so this evidence does not adopt a production correction engine.
