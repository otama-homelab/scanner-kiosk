# Shared selective correction

Owner-approved2026-11-10: photo and Canon scan use one correction workflow. Do not develop correction algorithms, train models or invent a detector. Reuse established libraries and the pinned upstream model implementation. New application code owns only validation, bounded execution and revision-bound workflow integration.

## Existing ownership and callers

| Stage | Photo | Scanner | Shared owner |
|---|---|---|---|
| Acquisition | import + photonormalize EXIF | SANE capture | document pages/original artifacts |
| Immutable correction baseline | normalized PNG in photo_sources | captured original pixels | documents.RenderSource; never replace OriginalArtifactID |
| Transform validation | normalized coordinates | original coordinates | internal/correction.Validate |
| Preview/apply/undo | manual crop render job, existing undo | page_corrections suggestion/apply/undo | owner/revision/job guards in documents |
| Save | normalized or explicitly applied photo pixels | existing plain Save uses original; applied pixels are available to explicit reOCR | existing render admission/snapshot |
| ReOCR | selected effective artifact | selected effective artifact | loadReOCRSources/RenderWork |

Callers: documenthttp/correction.go -> RenderService or ApplyCorrection/UndoCorrection; documents/render_work.go resolves baseline/effective artifacts; workerruntime and renderjob execute frozen work. web/src/client/correction.ts and app.ts expose only available operations. Evaluation uses internal/evaluation and scripts/eval/correction; it is not runtime authority.

## Decision contract

| Evidence available | Action | Pixel work |
|---|---|---|
| Correction not requested or no eligible engine | keep baseline/manual controls | no detection/inference/warp/new artifact |
| Existing library reliably reports no correction needed | keep baseline | no inference/warp/new artifact |
| Proven library reports safe skew/perspective and meets frozen quality/resource criteria | offer optional light correction | use library transform; explicit preview/apply |
| Existing reliable signal requests a model; approved pinned model meets criteria | offer optional model correction | official preprocess/decode + library warp |
| Unknown signal, missing model, ambiguous geometry, threshold/resource failure | keep baseline/manual; unavailable status | no automatic escalation or apply |

Input provenance may inform an engine but never substitutes for geometric evidence. No home-grown sharpness/edge/brightness classifier or hand-picked threshold pipeline is introduced. The old evaluation-only OpenCV helper contains a custom threshold/crop heuristic: keep it as a historical comparison arm, do not port it into production or improve its detection algorithm. No-correction cannot prove corner detection quality.

## Required invariants

- Preserve original bytes/hash, normalized photo baseline, page order/rotation, owner and TTL. Geometry references the baseline, not the current preview or cropped intermediate.
- Apply and undo remain explicit, revision/idempotency protected and blocked during active jobs. Viewing never starts correction. Unknown commands are reconciled without automatic replay.
- Repeated edits start from immutable baseline; compose metadata rather than repeatedly resampling. Existing library owns pixel transformations. No-op creates no new image, inference or resampling.
- Save/reOCR selection stays explicit and source-aware. Do not silently change scanner plain Save or rasterize imported PDF text/vectors. The same selected revision must supply preview/export/reOCR pixels where correction is selected.
- Validate finite ordered convex in-bounds corners, minimum area and bounded output/resources before any library warp. Failed/unsafe output is not a suggestion. Validation is application trust-boundary code, not a replacement correction engine.
- Model adapter must call pinned upstream preprocess/postprocess and a standard perspective API. Weight bytes/license and runtime receipts precede candidate testing. No private documents in source/logs/receipts or external uploads.

## Integration gate

Phase52 freezes thresholds before integration and records candidate adoption separately from successful execution. Public synthetic fixtures are not representative phone/scanner quality proof. Missing real fixtures or OCR-benefit evidence defers model adoption; common plumbing and existing manual/no-op workflow can still ship without claiming automatic correction. No new runtime/service/GPU/model distribution until bounded execution and corresponding terms/security/packaging requirements pass.

Curved-page, illumination/shadow correction and LLM repainting are outside this milestone. Current live v0.2.5 is unchanged by this design.

## 2026-11-10 measured candidate decision

Three arms used the same10 public synthetic fixtures through existing benchmark/analyzer (cold1/warm2,90 process samples). All30 pixel-retention rows passed, but **all10 DocAligner rows were unsafe/missing-corner fallbacks, zero actual corrections**. Copying the original preserves content; it does not demonstrate geometric/OCR improvement. Nine declared failure combinations were pre-invocation catalog validations, not inference samples.

| Arm | Cold wall ms range | Max sampled RSS bytes | Decision |
|---|---|---|---|
| No correction | 64–133 | 55,140,352 | Keep default |
| Historical OpenCV helper | 48–234 | 218,075,136 | Comparison only; do not port custom detector |
| Official DocAligner + capybara | 951–1212 | 402,706,432 | Defer; all fallbacks |

Separate-process warm DocAligner samples were952–1195ms, including import/session setup. This is not persistent-service or phone performance. Model-only timing omits substantial runtime overhead. Experiment ceilings30s/1GiBRSS/256MiBtemporary were observational runner bounds, not production SLOs. No private real-phone/Canon corpus, numeric truth or OCR-benefit evidence was supplied; do not infer those acceptances. Existing pixel oracle uses bilinear source mapping; future true projective outputs need library-backed mapping alignment before acceptance, without a new custom correction algorithm.

Production remains without an automatic engine. Shared baseline/apply/undo integration can proceed using existing proven manual/no-op paths; do not install Python/weights or add a service to product images from this result. Capybara's disposable installation imported a visualization font downloader (public asset fetched during setup); measured candidate runs had network isolation. Packaging must avoid that side effect before any future deployment. Local GSD52 receipts contain hash-bound measurements.

## Shared workflow implementation and verification

`documents.loadCorrectionBaseline` now supplies scan originals or normalized immutable photo baselines to existing manual/render source resolution and generic suggestion application. `ApplyCorrection` accepts either image source with exact current original/output hashes, correct baseline geometry, owner/revision/TTL/idempotency and active-job guards. Imported PDFs remain outside image correction. No database migration, runtime model/dependency, automatic engine, classifier or new image algorithm was added. Existing photo/manual and scan/suggestion controls remain; no unavailable automatic-model UI is shown. This is backend workflow integration, not newly enabled automatic distortion correction.

Full Go regression passed55 test packages/2952 test and subtest pass events, including actual synthetic-browser and pinned native OCR/PDF paths. Eleven packages had no tests; ten pre-existing optional physical/orientation/independent-fixture tests remained outside scope. Targeted shared correction race checks passed. Corrupt raw/normalized/corrected identity, stale revision, foreign owner, expiry and invalid transform are rejected without revision changes. Original and photo baseline survive apply/replay/undo; existing scanner plain Save and selected corrected reOCR retain their respective contracts.

The model is deferred, so default no-correction/manual is already the recovery state and incurs no model inference or image warp. No live rollout, new physical scan or model distribution was performed. Future activation requires representative real-photo/scan/OCR evidence, full dependency/license/source/isolation/resource packaging checks and explicit release/deployment scope. Existing v0.2.5 runtime remains unchanged.
