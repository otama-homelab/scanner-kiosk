# Practical correction comparison contract — v0.5.0

Frozen 2026-11-10 before new inference. Inputs: [survey](correction-models-and-products-survey-2026-11.md), [previous evaluation](edge-correction-evaluation-v040.md), [nonlinear binding contract](nonlinear-correction-contract.md).

## Scope and invariants

Detection: FairScan v1.2.0 TFLite plus its complete existing postprocessing, against no-op/DocAligner. Geometry: official PaddlePaddle/UVDoc_safetensors with installed Transformers processor/model, against the previous official ONNX on the same nine DocUNet images. Illumination: official FSENet/checkpoint or already-exported author-linked ONNX only if checkpoint terms qualify; SyntheticDoc only if its terms become explicit. No new correction algorithm, detector, shadow estimator, learning, exporter or patched build. Evaluation glue validates/binds artifacts and calls upstream operations unchanged.

Preserve original, normalized photo baseline, source/hash/revision, preview→explicit apply→undo, source-specific Save/reOCR and old OCR. Clean Canon scans use existing no-op without inference/resampling. Public datasets are not owner-device acceptance. No private uploads, physical capture, new GPU/services/cluster changes, SDK trial/account/purchase, release or deployment.

## Frozen corpus and truth

Reuse exactly the v0.4 manifest: DocUNet IDs1–3, two photos each plus three flatbed scans, nine images. Verify all SHA256 and pixel sizes before running; no post-result selection. Paired scans are not aligned pixel/dense-map truth or independent transcription. Do not redistribute raw dataset images. Existing tiny independent sampling fixtures (identity, boundaries, thin lines, known shift and padding) prove library conventions, not predicted geometry.

Independent real-image corner annotations, nonlinear dense truth, numeric/thin-line transcription and paired illumination/color truth are currently missing. Search existing public upstream fixtures for usable paired truth and terms; if absent, report unmeasured. Do not synthesize ground truth from a model, scan OCR or a second arm. No new fixture generator or custom correction is necessary.

## Preregistered qualification thresholds

- Detection: all labelled pages corner error <=2% image diagonal, document IoU >=0.95, zero lost annotated content. Missing annotations means no quality pass; fallback is a distinct unavailable correction outcome.
- Geometry: source-bound finite `(1,2,H,W)` backward mesh, explicit normalized x/y and align_corners=True; image and map/source/model/pre-post hashes retained. Sampling parity against unmodified official postprocessor and independent library oracle must pass. On independently annotated real truth, <=2% diagonal correspondence error and zero lost annotated digits/thin lines; each image must pass. OCR-to-scan similarity is descriptive only. Known photo2_2 must be reported, not excluded.
- Illumination: aligned permissible shadow/reference pairs must improve PSNR by >=1dB per shadow image; clean controls SSIM >=0.99 and per-channel mean absolute error <=1/255. Zero lost annotated digits/thin lines/stamps. Photos and colored-paper controls are required. Missing paired truth or checkpoint terms means no quality pass.
- Resource ceiling per evaluation process: CPU2threads, 30s, sampled process-tree RSS <=1GiB, output/temp <=256MiB, no network. Existing runner owns these limits; no claim of hard address-space isolation. Measure setup, first and two same-session full-image passes, outer cold1/warm2 separately. Acquisition excluded. Keep failures as failures; do not silently relax budgets.
- Product package gate: incremental installed model+runtime <=256MiB; report measured disk and shared-runtime scope rather than wheel/model size alone. Local CUDA-enabled torch being installed is not an edge/CPU package qualification.

All roles require eligibility, independent quality and resource gates before integration. A failed/unknown gate yields deferred/non-adopted, which completes the bounded evaluation branch; it does not satisfy a direct measurement capability that was unavailable. Record requirement dispositions accordingly. No qualified candidate means no production code/dependency changes.
