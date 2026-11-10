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

Create candidate configs. Pass the already materialized Tesseract 5.5.4 runtime when it is available; `--tesseract-runtime` accepts either an extracted runtime root or the executable `tools/scanner-native-tesseract` from a verified onefile cache. A onefile must report a version beginning with `tesseract 5.5.4` before it is used. Without `--tesseract-runtime`, the script emits only the three locally buildable arms and does not fabricate the unavailable arm:

```sh
# Include the runtime flag only when the pinned Tesseract 5.5.4 runtime exists.
scripts/eval/correction/prepare_candidates.py \
  --root /private/eval/correction \
  --tesseract-runtime /absolute/pinned/runtime/tools/scanner-native-tesseract \
  --output /private/eval/correction/candidates
```

The generated configs contain hashes and local absolute paths; do not commit them. Run cold 1/warm 2 `benchmark` for every available candidate/fixture. Retain each run's `artifact-catalog.json`, repetition receipts and `benchmark.json` under the private root, then pass one cold artifact catalog and every `benchmark.json` to `analyze-corrections`. Do not copy private catalog/config/run paths into source receipts.

The former geometry-only 40/40 result is superseded. The decoded-pixel run and explicit unavailable arm are recorded in `.planning/phases/45-scan/45-CORRECTION-COMPARISON.md`. No private household corpus or OCR improvement comparison exists, so this evidence does not adopt a production correction engine.

## Algorithm expansion survey

Phase 45-09 adds a source-pinned survey for classical, lightweight AI,
browser/WASM/WebGPU and optional heavyweight sidecar candidates:

- `candidates.json` is the machine-readable matrix.
- `test_candidate_survey.py` rejects missing source/license/weight/resource
  evidence, public-ineligible prototype paths and more than two non-heavy or one
  heavy selected prototype.
- `docs/design/correction-algorithm-candidates.md` records primary sources and
  proof limits.
- `.planning/phases/45-scan/45-CORRECTION-CANDIDATES.md` records the bounded
  prototype result.

The selected OpenCV arm is rerun through the existing benchmark and analyzer:

```sh
go run ./cmd/scanner-eval benchmark \
  --catalog "$CORRECTION_CATALOG" \
  --candidate "$OPENCV_CANDIDATE_CONFIG" \
  --fixture fixture_skew_001 \
  --output "$RUN_ROOT/opencv-skew" \
  --cold 1 \
  --warm 2

go run ./cmd/scanner-eval analyze-corrections \
  --catalog "$CORRECTION_CATALOG" \
  --oracles scripts/fixtures/evaluation/correction-oracles.json \
  --artifacts "$RUN_ROOT/opencv-skew/repetitions/001-cold/artifact-catalog.json" \
  --benchmark "$RUN_ROOT/opencv-skew/benchmark.json"
```

DocAligner is not run in this workspace because `onnxruntime`,
`capybara_docsaid` and the `onnxruntime-web` browser package are absent.
DewarpNet is not run because its final weights have no immutable hash or explicit
weight redistribution terms. DocTr is rejected by its non-commercial license.
Record these as unavailable/rejected; do not install a package, substitute a
similarly named model or invent measurements.

## v0.3.0 bounded DocAligner follow-up

The missing-runtime statement above describes the historical Phase45 run. A later disposable CPU environment ran `docaligner_candidate.py` using DocAligner1.1.1 official preprocess/postprocess (source revision3275b0f07f8e99d8c01cb0774dea2549be1416b6), capybara-docsaid0.12.0, onnxruntime1.23.2, NumPy2.5.0, Pillow12.2.0 and opencv-python-headless4.12.0.88. No production dependency was added. The adapter validates pinned upstream-module/model hashes before inference and calls the library's perspective transform; it does not implement a detection or warp algorithm. Bounds validation uses OpenCV contour APIs.

Invoke with the evaluation Python interpreter plus this script as argv, in a normal CandidateConfig, with `EVAL_DOCALIGNER_MODEL` pointing to the verified lcnet100 artifact. Run in the existing bounded, network-isolated evaluation environment. Upstream Linux dependencies can request GPU runtime and visualization downloads; never install them blindly into the product. Keep exact installed versions/licenses in local receipts. `EVAL_CORRECTION_MODE=none` explicitly skips model loading/inference/warp and copies the immutable PNG only because the evaluation protocol requires an output artifact. Production no-op must reuse its artifact instead.

Tests in that disposable environment: `python -m unittest discover -s scripts/eval/correction -p test_docaligner_candidate.py`. The geometry boundary test needs the evaluation OpenCV package.

Measured result: all10 synthetic model rows fell back with `unsafe-or-missing-corners`; no geometric/OCR quality or adoption proven. See [shared correction decision](../../../docs/design/shared-selective-correction.md). No-correction and the historical OpenCV arm used identical corpus/benchmark conditions. Expected failures stay separate from successful fixture rows.

## v0.4.0 official UVDoc / edge evaluation

`uvdoc_candidate.py` uses the unmodified official PaddlePaddle UVDoc ONNX export,
revision `3bcf535371727d11e783101f79a504c68848aae3`, SHA256
`54cab30dc2cf347b4f3d6971c833b1c3d84f5ef17280f1f6d15796bff20e63d6`.
Supply `EVAL_UVDOC_MODEL`, `EVAL_CANDIDATE_ID` and the normal Phase44 candidate
environment; the evaluation-only interpreter needs pinned CPU ORT, NumPy, OpenCV
and Pillow. No production package was added. Use the existing offline bounded
runner; do not run arbitrary document paths or install dependencies into production.

The graph returns a corrected image with no map. Artifact parameters explicitly
mark `transform_kind=opaque-nonlinear`, `map_status=unavailable` and
`apply_eligible=false`. **Do not pass these outputs to the four-corner correction
analyzer or production apply.** No crop_quad is fabricated. This adapter measures
three same-session full-image passes; external cold/warm labels still mean separate
processes, so compare timings with that distinction.

Focused tests in the disposable evaluation environment:
`python -m unittest discover -s scripts/eval/correction -p 'test_*.py'`.
The new sampling convention tests additionally use installed PyTorch, CPU only.
See [evaluation and non-adoption decision](../../../docs/design/edge-correction-evaluation-v040.md)
and [nonlinear contract](../../../docs/design/nonlinear-correction-contract.md).


## v0.5.0 official UVDoc mesh comparison

`uvdoc_mesh_candidate.py` calls the unmodified installed Transformers5.12.1
UVDoc model/processor with the official safetensors revision
`bfa6aad03b549c7e2cf89cefd2fb02ba3bef2ae4`. Model/config/preprocessor and
installed upstream source hashes are checked before use. Supply
`EVAL_UVDOC_MESH_MODEL` as a local directory containing the three pinned model
files, plus the normal candidate environment. CPU2threads and offline mode
are required; no production package or model is installed by this script.

The existing `generated` artifact role retains a JSON mesh with normalized
backward x/y coordinates, shape, little-endian float32 payload hash, source and
corrected-image hashes, model/source identities, bilinear interpolation,
`align_corners=True` and zero padding. The official processor owns all sampling.
`apply_eligible=false` remains mandatory. This is evaluation provenance, not a
production nonlinear-apply schema or a four-corner approximation.

Run the same focused Python discovery command above. New tests reject invalid
mesh shape/nonfinite/extreme values and verify official RGB/identity/known-point
sampling against independent tiny fixtures. These do not establish model quality.
The nine-image run exceeded the frozen1GiB RSS ceiling for all27process samples;
outputs were rejected before validation. See the
[v0.5.0 decision](../../../docs/design/practical-correction-evaluation-v050.md)
for the complete evidence and limits. The runner's RSS limit rejects after exit,
not a live memory kill guard; wall-time/temp-disk cancellation are separate.
