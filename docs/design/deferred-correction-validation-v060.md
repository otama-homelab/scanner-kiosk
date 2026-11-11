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


## 2026-11-11 旧DocShadow CPU評価を実施

作者紹介の旧DocShadow SD7K ONNXを現在のFSENetと区別し、公式前処理/run無変更で実測。合成影6+clean6入力を公式512設定で各3回、元900x700を追加3回、計39forward/13process成功。512 forward0.527–0.697秒/RSS476–511MB、native1.560–1.610秒/RSS714.5MB。影PSNR+1dBは4/6、青・桃紙は悪化。clean MAE基準は0/6で、この512構成は不合格・apply不可。既存OpenCV APIで測定、同resize-only baselineも保持。SSIM/全native品質/実写真/OCR/parity未測定、現FSENetの評価とは別。新規依存/補正アルゴリズム/製品変更/公開/配備なし。

実行記録: docs/design/research/correction-docshadow-cpu-v060/report.md。summaryと40件の証拠hashを照合。元14要件の完了数は6/14を維持し、ILL-01/02をこの別候補の部分診断で完了に変えない。前回の「モデル推論なし」はこの追補より前の履歴を示す。


## 2026-11-11 旧DocShadow全件native追補

公開合成6shadow+6cleanを元寸法で評価。新規33forward＋hash/config一致の先行3forward再利用、全12case成功。独自resize/paddingなし。forward1.560–6.171秒、RSS706609152–1356152832 bytes、診断2GiB全件内・製品1GiBは4/12超過。影PSNR+1dB0/6、clean MAE≤1/255は0/6。薄文字2件とblueはno-opより悪化。512縮小を除いても不合格・apply不可で、旧DocShadowのこの構成は採用候補から外す。将来再開は別artifact/公式改訂/独立した新入力等の変更がある場合のみ。同条件の再実行はしない。

証拠: docs/design/research/correction-docshadow-native-v060/report.md・summary.json。30証拠hashを照合。SSIM/実写真/OCR/現FSENet/parity未測定、元要件の完了数6/14を保持。新規依存・製品変更・公開・配備なし。


## 2026-11-11 OCR保持とFairScan公式ビルドの実行

旧DocShadow保存出力をTesseract5.5.4 jpn+engで36件OCR、全件成功。既存AnalyzeOCRPageでrenderer正解がある24件を採点し、色紙12件はrawのみ。native対no-opは改善0/同じ5/悪化3、薄文字table影の文字誤り2→8。512は改善3/同じ2/悪化3。baseline自身もstrict quality不合格で、文字品質の全体合格は主張しない。新モデル推論ゼロ。証拠: docs/design/research/correction-docshadow-ocr-v060/report.md。

前回計画の全面build禁止を改訂し、無変更の公式FairScan JVM評価を試行。公式Temurin21/Gradle9.8.2のdigest固定、source272件無変更。実compileはKotlin設定checkまで到達したが、101.57秒で隔離全tree1GiB監視により停止（観測1116906697 bytes、sampling overshoot、exit−9）。SDK不在やモデル不合格とは断定しない。未完downloadのみ記録後削除、同条件再試行なし。quad評価未実行。証拠: docs/design/research/correction-fairscan-jvm-v060/report.md。次回はtoolchain容量と実行tempを区別した計画へ改訂する必要がある。

現在6/14要件完了を維持。既存画像や失敗を成功として置き換えず、実写真truth/本人操作/現FSENet CPU固定loader等は未解決。製品変更・公開・配備なし。


## 2026-11-11 FairScan公式全経路と標準SSIM評価

FairScan公式source無変更のJVM compile成功(31.26秒)。続いて新しい隔離CPU環境へ公式binary wheel38件をPyPI公開hash照合して導入。無変更generate_masks.py＋tensorflow.lite.Interpreterで公開合成6枚のmask生成成功(3.05秒/RSS640225280B)、同じmaskで公式JVM四隅処理成功(11.08秒/RSS約107MB)、mask6/overlay6/HTML生成。source272件不変、GPU依存なし。これにより「公式実行環境不足」は解消した。setup容量とruntime tempを区別し、前回の1GiB全tree停止を隠さず保持。数値corner精度/独立元実写真truth/Android pixel parityは未測定で、VERIFY-01はwaiting_external_implementationからwaiting_external_inputへ更新、完了数6/14を維持。

旧DocShadowは標準scikit-image SSIMを隔離環境で36件測定。参照対自身6件すべて1、clean≥0.99はnative0/6・5121/6、native影は全6件でno-opよりSSIM低下。既知の画素/OCR/資源不合格に追加し、候補不採用を維持。新DocShadow推論ゼロ。

証拠: docs/design/research/correction-fairscan-full-cpu-v060/report.md、correction-fairscan-jvm-resume-v060/report.md、correction-docshadow-ssim-v060/report.md。各summaryの19/13/90証拠hash照合。残るFairScan評価は数値precisionを出す既存公式経路と独立正解の接続。現FSENet CPU固定loader/独立実写真正解/本人操作は別の残件。製品変更・公開・配備なし。


## 2026-11-11 FairScan公式四隅の数値比較

公式detectDocumentQuadの戻り座標だけを外部invocation adapterでJSON化し、既存corner指標をそのまま再利用。公式source272件不変、未検出を代替しない。合成6枚は検出3/未検出3、既存最大corner誤差2%基準0/6合格、同scope無補正6/6。追加独立UVDoc3文書(元9画像との重複0)は検出2/未検出1、検出2件の平均誤差0.752%/1.649%、無補正10.378%/8.444%。付属grid端点を使う既存diagnosticであり元6実写真annotationの代用ではない。

証拠: docs/design/research/correction-fairscan-numeric-v060/report.md、summary.jsonの19件hash照合。公開追加画像のモデル推論3回、既存合成maskは再利用。Androidと公式評価用mask値域のparityは未証明で、結果をモデル単体の限界へ帰属しない。VERIFY-01/02の元実写真truthとphoto2_2内容正解は未解決、要件完了6/14を維持。製品採用・公開・配備なし。


## 2026-11-11 FairScan相対改善・内容・未検出の分離評価

本人の2%妥当性への指摘を反映し、既定2%はpilot判定履歴として保持するがユーザー検証済み有用性境界とは扱わない。事後的な新しい合格閾値は追加せず、既存座標結果の対無補正平均/最大誤差、未検出、同条件OCRを分離して評価。合成6枚は改善0/悪化3/未検出3、追加独立3文書は改善2/未検出1で分母を維持。

公式DatasetEvaluatorの透視切出しを合成6枚へ適用し、同条件JPEG baselineとTesseract12件/既存AnalyzeOCRPageを比較。改善0/悪化2/誤り数同等4（うち公式未検出fallback3件は補正成功に数えない）。同じ誤り数でも文字列一致とは限らず、baseline自身の誤りも残る。既存画素保持metricはinverse-bilinear、公式warpはperspectiveで同一mapでないため位置/画素保持は未検証と明示、独自metricを作らない。相対改善例は認めるが内容回帰/未検出/不足証拠からpreview適格性falseを維持。

証拠: docs/design/research/correction-fairscan-relative-v060/report.md、summary.jsonの36件hash照合。追加モデル推論0。元実写真truth/本人受入は未解決、元要件6/14のまま。製品組込・公開・配備なし。


## 2026-11-11 FairScan mask入力契約差の診断

公式Android Mask.toMatはfloat Matではなく、clampedfloatを0.5で二値化したCV_8UC1の0/255であることをsource確認し、以前のfloatMat推測を訂正。desktop公式evaluatorは濃淡PNGをそのままcloneする。公式閾値を計算前に固定し、既存PNGへOpenCV標準threshold127で別枝の二値契約診断を実施（事後閾値調整なし）。quantization前raw/Bitmap resize/Android classの完全parityは未成立。

合成6枚の検出3→5、未検出3→1に改善したが2%pilot誤差基準0/6は変わらない。公式透視切出しと同条件JPEG baselineのOCRは改善2/悪化1/誤り数同等3。誤り数同等でも文字列変化や偽数字があり、内容保持を保証しない。5件新規OCR＋画像hash一致1件再利用、追加補正モデル推論0。前回の不利な結果はdesktop契約の履歴として保持し、モデル単体の性能へ一律帰属しない。

証拠: docs/design/research/correction-fairscan-mask-parity-v060/report.md、27件hash照合。preview適格性false、元実写真truth/厳密Android parity/位置内容保持/本人操作は未解決。元要件6/14完了を維持。製品変更・公開・配備なし。


## 2026-11-11 progress照合・正式検証更新

19計画/19SUMMARYはすべて結果記録ありだが、partial/不合格/待機を含むため能力完了率ではない。完了Phase1/5(64のみ)、元要件6/14を維持。Phase66/67の正式VERIFICATIONをgaps_foundで保存し、missing report状態を解消。独立検証は390索引証拠/66公開raw binding/11公式source本文をhash再照合し不一致なし。

台帳の古いILL01/02一律rights-holder待ちと「照明推論なし」を訂正しhistorical_evidence_scopeを保持。現FSENetはCPUloader/storage.cuda(0)とartifact/safe-loading未解決、旧DocShadowは72uniqueforward/OCR/SSIMの評価済み不採用として分離。FairScanruntimeは解消済み、元実写真独立正解/photo2_2内容原因/厳密Androidparity/透視内容保持は残る。本人操作は別のPhase68checkpoint。次回は変化した独立正解または公式CPU対応artifact/loaderに対してのみ再開し、同じ不合格推論/SSIM/閾値の再試行はしない。

正本検証: docs/design/research/correction-evaluation-status-v060/verification.md。今回新規推論/テスト/build/downloadなし、製品変更/公開/配備なし。


## 2026-11-11 現FSENetのCPU portability fork実測

反復する自律評価指示に沿い、無変更公式CPU経路の不存在を繰り返す代わりに計画上のsourcepatch全面禁止を限定改訂。公式pinnedsourceをprivate local Git forkとし、checkpoint読み込みのCUDA固定→cpu/weights_only=Trueだけ1行変更。モデル本体/前処理/推論source無変更、差分commitを保存。公式SD7K重み117980483B/hash固定、安全load991entries/strict model load29398130parameters成功。元公式CUDAと同じruntimeとの主張はしない。

現モデル22completedforward: native10/12出力保存後にaggregatePSS2180001792B>2GiBでプロセス群停止（正確な内部停止stage不明、2件未完）。公式ORI=Falseの512経路は別runで12/12成功、forward0.591–1.014秒、parentRSS814MB/aggregatePSS1.147GBを区別。nativeは1.808–6.091秒。512影PSNR+1dB4/6、cleanMAE0/6/SSIM3/6、native影0/5/cleanMAE0/5/SSIM0/5で適用不可。旧ONNXと同一native入力tensor10件はhash一致、固定allclose4/10、最大差5.85e-5。配布lineage/CUDAparity証明ではない。

証拠: docs/design/research/correction-fsenet-cpu-v060/report.md・summary.jsonの108件hash照合。台帳ILL02をmeasured-partialへ更新し「現FSENet推論なし」は過去の状態として明確化。ILL01はconversion出所/parity等の証拠待ち、ILL02は内容欠落/細線/印章と全体条件未完。元要件6/14を維持、製品変更/配備/公開なし。次の独立検証ではこのCPUfork追補を前回verificationより新しい証拠として読む。
