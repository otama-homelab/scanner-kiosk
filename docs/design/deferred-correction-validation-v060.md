# Deferred correction validation — v0.6.0

2026-11-10. Evaluation remains **incomplete**. Runtime preparation is verified; independent content/shadow truth and owner-device acceptance remain open. No model is qualified for product integration. Live/public v0.2.5 is unchanged. This report does not reuse the v0.5 conditional non-adoption branch to complete missing capabilities.

Machine-readable results and pinned evidence: [summary](research/correction-validation-v060/summary.json). Local raw public evaluation artifacts remain under `/tmp/scanner-v060`; owner input is outside the repository. Evidence hashes identify exact observations; local temporary environments are not a permanent distribution.

## Runtime and budget

Official binary-only CPU torch2.12.1+cpu, torchvision0.27.1+cpu, Transformers5.12.1 and separate ai-edge-litert2.3.0 run in clean venvs without system/user site packages. Both pip checks pass; CPU torch reports CUDA null, with no NVIDIA or Triton distributions. Wheel sizes/hashes, installed package sizes and license-file identities are retained separately. No source build, exporter, custom correction/detection/shadow algorithm or GPU/service change was used.

Before inference, product CPU2threads/30s/RSS1GiB/temp256MiB/incremental installed packages256MiB and diagnostic RSS2GiB/apply=false budgets were frozen in [preregistration](research/correction-validation-v060/preregistration.json). Existing runner rejects excessive peak RSS **after process exit**; it is not a live memory limiter. Temp disk is sampled, timeouts terminate execution. Product and diagnostic runs have separate configs/directories. Prior CUDA-capable measurements remain historical evidence.

CPU wheels total255,887,521B; installed distributions1,125,589,908B, or1,105,674,590B excluding pip/setuptools, **failing installed256MiB**. Compressed download size is not the installed-size gate. LiteRT wheels46,509,666B; installed166,906,656B. CPU import peak405,176KiB; model-load peak418,856KiB, model load445.957ms. Tiny known RGB control succeeds with retained mesh/output; peak581,705,728B across3 runner processes. LiteRT zero-input forward setup9.368ms; forwards26.281/20.243/18.410ms; peak91,312,128B.

FairScan pinned TFLite SHA256 `96e14d7e610dd0c27b768b228fbc553b4ec119ebe68f3a3594029a25400691d2`,4,921,040B, float32 input[1,256,256,3]/output[1,256,256,1], unquantized. Negative raw outputs are not probabilities. Published app preprocessing uses bilinear resize and normalization(127.5,127.5), with an upstream correctness TODO; published postprocessing clamps0..1 and thresholds0.5. Pillow resize is only a mask diagnostic, without Android ResizeOp parity. Android/Kotlin/OpenCV whole detection has not been ported or verified.

## Independent truth and retained mesh

Original DocUNet9 hashes were rechecked without replacing photo2_2. An additional diagnostic subset was fixed as lexicographic UVDoc benchmark IDs00000/00001/00002 **before its inference**. A bounded ZIP-range reader fetched9,513,629 network bytes from the775,129,594-byte official benchmark, including independent grid2d/seg and references. It did not fetch the full dataset. Dataset repository MIT and third-party texture acknowledgements are recorded separately: local benchmark evaluation only; no raw redistribution, training or product bundling, and no blanket claim about third-party assets. Doc3D metadata advertises MIT but actual card access returned401/gated; no credentials, access request or signup was used.

Dataset-supplied grid/mask is independent of predictions. HDF5 orientation follows official `data_UVDoc.py`; mask shape and binary labels, grid dimensions/finite values and source hashes are asserted. Correspondence metrics interpolate predicted mesh with standard torch bilinear align_corners and compare absolute source-pixel positions, normalized by image diagonal. This is evaluation glue, not a new correction implementation. No-op identity is a geometry baseline. These3 documents do not supply the required transcriptions, thin-line survival labels or photo2_2 truth. No OCR/LLM/model output is promoted to human ground truth.

The existing v0.5 adapter and runner are reused unchanged. Tiny control is tested first: generated JSON is retained/reloaded; float32 little-endian C-order payload hash, source/output/model/pre/post identities, finite shape/range and apply=false are checked. Reapplying the official processor to the reloaded predicted mesh reproduces retained RGB pixels exactly. Existing invalid-mesh and known-color/coordinate tests pass. Fixed9 receive cold1/warm2 **separate processes per budget**; each adapter process separately executes3 same-session image forwards. Runner warm labels must not be mistaken for a persistent loaded model.

## Illumination eligibility

Fresh FSENet README/source MIT/Weights release and author-linked DocShadow ONNX README/release were inspected. General source MIT alone is not newly asserted as checkpoint-specific clearance under this plan. The linked ONNX release predates the FSENet checkpoint release; exact architecture/checkpoint identity and parity are unresolved. SyntheticDoc publishes dataset/generation/training material, which alone does not settle a specific released illumination weight artifact and terms. No ineligible checkpoint was downloaded or executed.

[Concrete upstream questions](research/correction-validation-v060/illumination-clarification-unsent.md) name maintainers, checkpoint scope, commercial/local use, redistribution, conversion provenance and safe inference/parity. They are **unsent**. Website template CC BY-SA is not a dataset license. SD7K external distribution and aligned6pairs/clean3 coverage have not been acquired/verified; illumination quality remains not-run.

## Owner input and product acceptance

Owner explicitly authorized **one** scan of the original already on the Canon. Existing one-shot experimental eSCL client captured A4 color300dpi,2480×3508px in16,908ms, then device returned Idle. It retains a decoder-normalized PNG baseline under `/home/yamato/.local/share/scanner-validation/v060/` with directory0700/files0600. No resampling/correction was applied. The client records wire JPEG size/hash but returns normalized PNG; **wire JPEG bytes were not retained**. No second scan was requested or performed. Receipt and baseline are kept private; raw owner content is not committed, displayed or externally uploaded.

This is direct experimental acquisition, not application admission, identity/lease validation, preview/apply/undo, saving or reOCR acceptance. No new AI was run on this clean Canon input. Actual iPhone photograph of the same original and owner iPhone/PC operations remain missing. Qualified-only product integration has not occurred because weight, quality and installed-size gates are unmet. Existing regression tests establish only their automated scope.

## Remaining work

- Human-checked corners/content on existing6DocUNet photos; mandatory photo2_2 digits/thin lines and diagnosis of crop/geometry/sampling/OCR segmentation. Keep original9 and prior failures.
- At least6 aligned shadow/reference pairs plus3clean controls, with thin text/digits/photos/colored paper/stamps each represented twice; dataset terms and alignment must be verified.
- FairScan full supported path and independent existing6-photo validation; no new mask-to-quad code or Android postprocessor port.
- Official mesh/oldONNX/no-op comparison with independent geometry **and** content/OCR truth. Partial3-document geometry is not full capability verification.
- Explicit checkpoint terms and provenance/parity for an eligible illumination model; send drafted questions only upon instruction.
- Same-original iPhone input and owner PC/iPhone preview→explicit apply→undo/save/reOCR acceptance. The new Canon acquisition does not substitute for these operations.

Milestone remains active; no archive/release/deployment or new acceptance acknowledgement is generated. See the machine ledger for measured-pass, measured-fail, not-run and needs-human distinctions.

## Measured partial quality results

| UVDoc public ID | Mesh mean error / diagonal | No-op mean error / diagonal | Mesh maximum / diagonal | FairScan mask IoU diagnostic |
|---|---:|---:|---:|---:|
| 00000 | 0.4585% | 7.3297% | 1.1314% | 0.9360 |
| 00001 | 0.1944% | 5.7679% | 0.7632% | 0.8400 |
| 00002 | 0.3609% | 5.2354% | 0.7467% | 0.9842 |

Each geometry comparison contains5,429 independent grid points. Mesh geometry meets the2% threshold on these3 cases, including observed maxima; this does not qualify digit/fine-line/OCR retention or photo2_2. FairScan reaches IoU0.95 on1/3cases using the disclosed diagnostic preprocessing;2/3fail. There is no attempt to improve the result with a new sigmoid, threshold search or postprocessor.

Original9 ×3processes ×2budgets yields54 completed runner processes; measured RSS range645,312,512–771,555,328B. Resource process limits pass on this corpus while the installed-package gate fails. The additional3truth documents each have3retained diagnostic runs. All63 retained source/mesh/output bindings reload and reproduce the output exactly through the official processor. An initial verification script used manual float division and produced1-level differences on0.0794%of one image's channels; replacing that duplicate preprocessing with the official processor restored exact equality. The checker was corrected; the adapter, mesh and output were not changed, and no tolerance was relaxed.

Existing complete DocAligner path was also run on the same3additional UVDoc inputs:9/9samples fall back with `unsafe-or-missing-corners`; each output is byte-identical to input. Effective no-op full-image crop IoU is0.6505/0.7216/0.7324 and mean corner error11.79%/10.38%/8.44%of diagonal, failing the geometry target. These resource timings overlapped automated regression and are explicitly excluded from resource qualification. This is a functional fallback/crop comparison, not a new FairScan detector or original6-photo completion.

Focused existing correction/photo-manual/reOCR/shared evaluation tests pass in4Go packages after supplying the established Playwright/Chromium/PDF runtime paths. The first run lacked the required Playwright environment and failed the browser case; that failure and the corrected run are retained. Existing2mesh boundary/color-position tests also pass. These are fixture/local-browser regressions, not owner iPhone/Safari or physical product capture acceptance. No production source code was changed.
