# v0.4.0 edge correction evaluation

2026-11-10. **Evaluation complete; automatic correction is not enabled.**
Live/public remains v0.2.5. No production dependency, image, schema or UI changed.
No custom correction algorithm or model training was introduced.

## Decision

UVDoc is a useful next candidate: an offline CPU can produce visibly flattened document
images within the evaluation budget. It is not qualified for automatic product use:
one of six photo OCR proxies regressed, independent numeric/thin-line/geometric truth
is missing, and this official ONNX export exposes no source-bound nonlinear map.
Keep no-correction/manual defaults and the existing shared photo/scan workflow.
Geometry and illumination are deferred separately; no candidate was adopted.

The [source survey](correction-edge-survey-2026-11.md) and
[preregistered contract](nonlinear-correction-contract.md) describe candidate identity and gates.

## Real-photo CPU comparison

DocUNet document IDs1–3, selected before inference:6cropped camera photos and3paired
flatbed scans. Lossless PNG conversion only. Each input is shared by no-op, DocAligner
and UVDoc. Existing Phase44 runner, cold1/warm2: **27benchmarks,81process samples, all complete**.
Repeated outputs are resource samples, not independent documents.

Host: Intel Core i5-12400, Linux, CPU2threads. Per request:30seconds,1GiB sampled RSS,
256MiB output/temp; network namespace isolation. These are bounded evaluation processes,
not a claim of a hard-address-space sandbox. Model/setup acquisition excluded.

| Arm | Distinct output outcomes | Cold process range | Outer warm process range | Peak RSS |
| --- | --- | --- | --- | --- |
| No correction | 9/9 exact original copies | 82–151ms | 88–164ms | 31.8MiB |
| DocAligner1.1.1 | 4/9 transformed,5/9 unsafe/missing-corner fallback | 1087–1519ms | 1068–1371ms | 272.5MiB |
| Official UVDoc ONNX | 9/9 changed images,0fallback; geometry correctness unproven | 1473–3066ms | 1401–3367ms | 525.7MiB |

UVDoc candidate performs **3same-session passes per process** to expose reuse cost.
Its process total is not directly comparable to the one-pass arms as a latency ranking.
Warm full image decode→tensor→inference/graph-owned warp→PNG encode:
**median603.697ms**, range361.113–985.604ms,54warm passes across27processes.
This excludes session/model setup. First full-image and setup timings are retained separately
per artifact. UVDoc maximum sampled temp/output was4,836,568bytes.
All requests stayed inside the fixed resource budgets; same-session outputs were deterministic.
No separate raw-inference stopwatch was added: official graph inference includes its warp,
and timings here must not be compared to upstream inference-only values as equal quantities.

## OCR and fidelity limits

Fixed local Tesseract5.5.4, eng, PSM3, CPU2threads:27OCR runs, one cold output per arm/image.
No scan OCR text is treated as independent transcription truth. The following is
whitespace-normalized `difflib.SequenceMatcher` similarity to the corresponding **no-op scan OCR**,
not CER, accuracy, or a quantitative geometric metric.

| Photo | No correction | DocAligner | UVDoc |
| --- | --- | --- | --- |
| 1_1 | 0.6755 | 0.6755 | 0.9858 |
| 1_2 | 0.6791 | 0.6791 | 0.9261 |
| 2_1 | 0.6254 | 0.0447 | 0.9476 |
| 2_2 | 0.6067 | 0.0545 | **0.4811** |
| 3_1 | 0.5422 | 0.3922 | 0.9121 |
| 3_2 | 0.6995 | 0.2476 | 0.8423 |

UVDoc improves this descriptive proxy on5/6photos but worsens one. In photo2_2 its
whitespace-normalized OCR length falls553→278characters. This does not prove the model
erased pixels; segmentation/geometry/recognition may all affect OCR. It is a regression
signal requiring inspection, not an accepted image result.
Visual inspection of photo1_1 before/after showed flatter text lines. Photo2_2 output still
shows receipt text and substantial folds/shadows. Inspection was limited, not a full content-retention audit.
Digit-token sequences and output hashes are retained locally, but numbers and fine-line
retention have no independent annotated truth. No fidelity pass is claimed.

The old four-corner analyzer was deliberately not used for nonlinear outputs. The adapter
declares `opaque-nonlinear`, `map_status=unavailable`, `apply_eligible=false`; it invents
neither crop_quad nor mapping data. All resampling occurs in the unmodified official graph.
Tensor/output conversion matches official PaddleX conventions; exact output-postprocessor
parity passed against two fixed tensors from the pinned official source.

## Illumination and edge feasibility

| Candidate/path | Observed evidence | Decision |
| --- | --- | --- |
| SyntheticDoc | Official source MIT, two external checkpoint links, CPU flag and crop requirement. Inspected docs/headers do not explicitly bind external checkpoint usage/redistribution terms; original checkpoint header101,737,297bytes | Weight eligibility unknown, not a finding that use is prohibited. No download/load/inference or illumination comparison; clarify terms before another evaluation |
| UVDoc ORT-Web1.23.2 WASM | Disposable official npm package; Node24.22.2 host, CPU2threads, offline zero tensor `[1,3,712,488]`;3finite outputs; forward607/578/566ms, setup326ms, sampled peak356.3MiB | CPU-WASM operator compatibility demonstrated on this Node host only. No browser/WebGPU/mobile/photo-pipeline claim |
| DewarpNet-LiteRT | Community export188,852,928bytes; upstream device records; conversion uses patched development tooling; no LiteRT runtime installed here | Do not custom-build a patched production image. Device figures remain upstream evidence; no local inference/adoption |

Initial WASM probe could not locate the Node shim with minimal PATH (exit127). The corrected
probe uses the exact interpreter path and succeeded; both receipts are retained. npm was
installed with lifecycle scripts disabled into `/tmp` only. Runtime network was isolated and
the probe had30s/1GiB sampled kill guards plus a256MiB V8heap limit. This is a compatibility
probe, not a second photograph benchmark runner or a production browser feature.

## Evidence and remaining work

Canonical local planning evidence is under `.planning/phases/55–58-*` (or the v0.4.0 milestone
archive after closeout). Corpus/weight/source/config/runtime hashes,81process receipts,
27OCR receipts and3WASM forwards are retained. Full public image/OCR outputs remain in
`/tmp/scanner-v040`; they are not committed or redistributed. Temporary storage is not a
permanent dataset archive; restoring a run requires the recorded upstream sources.

DocUNet was acquired from the original benchmark's HTTP download links, with local hashes
but no authenticated publisher checksum. Intended local benchmark use does not establish
redistribution permission. Existing protected-file ingestion is used with explicit public
provenance, never by pretending the images are synthetic. The small3-document subset is
not representative of Japanese household documents, iPhone captures or Canon TS8130.

Before any model activation: resolve source-bound nonlinear map availability; supply independent
geometry/numeric/fine-line/OCR truth; investigate the observed regressions; verify actual owner
photo/scan behavior and package/redistribution conditions. Illumination additionally needs
eligible checkpoint terms and paired/reference or annotated shading evidence. These are
future prerequisites, not new owner acceptance inferred from earlier general approval.

Existing source-aware baseline, owner/hash/revision/TTL/idempotency guards, preview/apply/undo
and Save/reOCR remain the product behavior. No production integration was attempted because
no model passed the frozen gates. No physical scan, private upload, GPU service, release,
push or deployment was performed for this milestone.
