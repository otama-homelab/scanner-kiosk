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

## Resumed gap closure

A further public reinspection identified SyntheticDoc official `training/model/README.md` links for `original_31x45.ckpt` and `highres_121x177.ckpt`; the repository MIT text was confirmed. Exact weight-specific terms remain unresolved under the planned eligibility rule, so no checkpoint was fetched. FSENet and linked ONNX license issue searches returned no matches; this is not permission. Hugging Face SD7K name matches were text/tabular datasets from another publisher and were not assumed to be the document-shadow benchmark. No requirement was newly completed.

The [human input packet](correction-validation-v060-human-inputs.md) now links all6existing original photos and3scan references, binds source hashes, lists pixel-coordinate/corner/content/transcription requirements, and identifies photo2_2 comparison outputs for use only after independent labels are fixed. It also identifies the already captured local Canon baseline, so no repeated scan is needed. Missing labels, eligible shadow pairs and owner-device evidence remain open.

## Owner-authorized generated-input validation completed

本人「お任せ、生成画像でもよい」に基づく[生成画像追補](correction-synthetic-validation-v060.md)。既存renderer6種類の既知truthで72process／24caseの補正・OCR・罫線比較と18mesh再読込を完了。TRUTH-01はこの代替入力範囲で実証し、現在5/14要件完了。元の6実写真への注釈、photo2_2、影paired truth、重み条件、実本人端末の受入は未実証のまま。cleanに対しUVDoc新旧6/6MAE不合格、DocAligner表の寸法変更により不採用を維持。実原稿や権利者回答を生成データで代用していない。

## Synthetic paired truth supplement — 2026-11-10

Owner-authorized generated-input route now supplies6aligned shadow/reference pairs and6clean controls. Existing `evaluation.ResolveFixture` pattern/faint and pattern/shadow are reused unchanged, with no coordinate warp or new synthesis algorithm. Repeated generation is byte-identical; hashes, generator source identity, dimensions, budgets and thresholds are fixed before any illumination-model inference. Coverage: faint text2, digits4, photograph2, colored paper2, seals2. The two generated reference pages are retained at `scripts/fixtures/generated-correction/`; prompts/provenance are in its README. Printed-photo lighting and paper variation are reference content, not artifacts to flatten.

Evidence: [paired manifest and replay helpers](research/correction-paired-v060/summary.json). No-op shadow PSNR16.91–19.72dB; improvement0dB, so it fails the1dB shadow-removal target. Clean identity baseline has MAE0 and exact SSIM1 by identity. No eligible illumination model was executed; these baseline checks are not a model comparison or real-shadow validation.

TRUTH-03 is complete only within the explicitly authorized synthetic alternative. Current ledger6/14complete,8open. Phase65 still lacks mandatoryphoto2_2 content truth. FairScan full detection, original-photo comparisons, eligible illumination weights and owner-device acceptance remain unresolved. No milestone archive, deployment, external message or second physical scan.

## Synthetic detection supplement

[Evidence and replay](research/correction-detection-v060/summary.json): the previously fixed6renderer-known full-canvas controls were measured with the existing FairScan LiteRT normalization/clamp/threshold path. IoU>=0.95:0/6. Pillow resize remains unverified against Android; no mask-to-quad or custom detection is added. One diagnostic process peakRSS634,896,384B; forward-only times are not complete detector timing or cold/warm product qualification.

Existing no-op and DocAligner results were reused with source/catalog hashes checked (18samples/arm). Against known page corners, no-op passes6/6; DocAligner passes5/6 but all15passing samples are source-identical fallback. The table's3samples crop inward and fail2%corner error. This does not supply original6real-photo labels or qualify FairScan's complete path. Requirements remain6/14complete,8open. No release/deployment/model adoption.

## Latest upstream eligibility recheck

[Metadata receipts](research/correction-eligibility-recheck-v060/summary.json) verify that FSENet's official repository is archived. The drafted GitHub issue destination cannot accept a new issue; no message was sent. SyntheticDoc remains MIT at repository level, without resolving the planned checkpoint-specific gate.

Correction to the broad unsupported-path description: FairScan **does include** an official JVM/Kotlin `QuadDetectionEvaluator`, calling the real `detectDocumentQuad` with local OpenCV. Pinned tree `2c5230b12f69f3cee2cc5a970013da20848d1272`. It is source, not an executed or distributed desktop evaluation binary. Latest releasev2.3.0 contains only ARM Android APKs. Official Python mask generation uses OpenCV INTER_LINEAR; existing Pillow diagnostic preprocessing is not that exact path. No Kotlin port, new postprocessor, source build, emulator, APK installation or TensorFlow dependency was added. VERIFY01 remains unverified for full execution; the existence of official evaluation source must not be confused with runtime availability or its absence.

Delegation does not supply absent independent real-photo annotations, owner device operation or a rights-holder answer. Current6/14complete; no requirement is checked merely to reach the requested count. Original14requirements and prohibitions remain in force.


## 2026-11-11 自律実行の最終状態

EGMレビュー済み6計画の自動14タスクを実施し、独立検証も自動処理の証拠をPASSと判定。状態は `automation_queue_exhausted`。元の能力要件は6/14完了・8未完で、Phase64のみ完了。追加6計画のSUMMARYはpartial、最後の68-02-T3はblocking-human。milestoneの完成・archive・公開・デプロイは行わない。常用はv0.2.5。

実行記録: [自律実行結果](correction-autonomous-execution-v060.md)。正本: docs/design/research/correction-validation-v060/summary.json。独立検証: docs/design/research/correction-autonomous-v060/independent-verification.md。

再開は変更された証拠revisionがある枝のみ: 元photo2_2の独立数字/細線正解と元実写真注釈、互換性のある公式FairScan完成binary/起動経路、checkpoint固有の権利/変換同一性/parity、全gate合格候補、同一原稿のiPhone入力と本人PC/iPhone操作。条件未変更で調査・試験・質問を反復しない。既存receiptを保持し、新revisionで再開する。候補未適格とclean Canon無推論保証の未証明はACCEPT-02未完に保持する。


## 2026-11-11 公式実行経路・利用条件の訂正

本人の続行指示により追加一次資料調査と公式FSENet CPU preflightを実施。FairScanには公式の非Android Kotlin/JVM evaluatorがあり、「Linux binaryなし＝公式経路なし」は訂正する。ただし初回compile/classpath/OpenCV native loadは未実施で、現計画の無build境界では未検証。公式Python mask単独は完成四隅検出を満たさない。

FSENetはMIT repoが推論用として重みを直接配布し、確認資料に重み除外はない。「専用許諾文がないため必ず権利者回答待ち」は一律の停止理由から外す。元PyTorch評価にはONNX parity不要。ただし公式load_checkpointがstorage.cuda(0)を固定し、既存CPU環境で無変更実行できないことをsource確認。accelerate/torchmetrics/yacs/einops/albumentationsも不足。旧DocShadow ONNXは現在のFSENetと別候補に分離する。重み取得や依存導入だけではCUDA固定を解消しないので未実施。

証拠: docs/design/research/correction-runtime-followup-v060/summary.json、fairscan.md、illumination.md、fsenet-preflight.md。過去のimmutable監査は当時の判定として保持し、本訂正を現在の根拠とする。6/14の能力達成数は変わらない。再開条件はFairScanの公式JVM評価を許すbuild方針、FSENetの公式CPU対応経路（または別候補としての旧DocShadow評価計画）、独立正解・本人操作証拠。単なる専用許諾文の欠如を再び一律の停止理由にしない。
