# Practical document correction comparison — v0.5.0

2026-11-10. **Bounded evaluation complete; no candidate adopted.** Live/public remains v0.2.5. No custom correction algorithm, training, model exporter or production dependency is introduced. [Frozen contract](practical-correction-comparison-contract.md); [survey and primary sources](correction-models-and-products-survey-2026-11.md).

## Qualification and corpus

The same nine DocUNet images (IDs1–3, six cropped photos and three scans) were verified against v0.4 SHA256 before inference. These are public benchmark images, not iPhone/Canon captures. Scan pairing does not supply aligned dense geometry or independent numeric/thin-line transcription. Official FSENet repositories provide a sample image and links to a9GB dataset, but no immediately usable small aligned reference with established terms/content truth was identified. No private images were accessed/uploaded and no dataset images are redistributed.

Pre-run ceilings remain CPU2threads,30s/process,1GiB measured peak RSS and256MiB temporary output; package increment256MiB. Existing runner measures peak RSS after process completion; **RSS is a rejection ceiling, not a live kill guard**. Time and temporary disk have runtime cancellation. Old v0.4 wording about a sampled RSS kill guard must not be read as evidence of live memory enforcement. No threshold is raised after a result.

Quality requires independent task-specific truth and content preservation. Missing quality truth means unqualified, even if an image looks better or OCR-to-scan similarity improves. Missing runtime/terms branches complete a bounded candidate assessment, not a direct measurement capability.

## FairScan document detection

FairScan v1.2.0 quantized DeepLabV3+/MobileNetV2 artifact **4,921,040bytes**, SHA256 `96e14d7e610dd0c27b768b228fbc553b4ec119ebe68f3a3594029a25400691d2`, was downloaded and verified against publisher digest. Bundled GPLv3 LICENSE.txt SHA256 `3972dc9744f6499f0f9b2dbf76696f2ae7ad8af9b23dde66d6af86c9dfb36986` also matches. GPL permits commercial use subject to its obligations; no GPL code/model is incorporated into the Apache-2.0 application and no license compatibility conclusion is fabricated.

No ai_edge_litert, tflite_runtime or tensorflow is installed. The complete existing path is Android Kotlin segmentation→DocumentDetection.kt→quad orientation/scoring helpers→OpenCV Java. Java alone does not supply that path. Creating a Python mask-to-quad replacement or patched packaging would violate the chosen scope. **Deferred for missing existing complete runtime**,0inferences, no accuracy/latency/fallback-rate result. v0.4 no-op9/9exact copies and DocAligner4transforms/5fallbacks are historical baselines only.

## UVDoc official mesh path

Official `PaddlePaddle/UVDoc_safetensors` revision `bfa6aad03b549c7e2cf89cefd2fb02ba3bef2ae4`, Apache-2.0 model card;31,667,380byte safetensors hash `65f980e3083b4919d962d7b4c8ddb42a926dc8b36f4909944746cf2d7b7325dc` verified against publisher. Installed unmodified Transformers5.12.1 and torch2.12.1 are called CPU-only, offline, with official pre/postprocessing. The model exposes a2channel backward mesh; previous ONNX map unavailability was artifact-specific.

Evaluation glue validates bounded finite mesh/source identity and explicitly keeps apply_eligible=false. Official postprocessor independent tests verify RGB channel order, identity pixels and a known center sample; existing oracle checks boundaries/thin lines/shift/padding. These prove sampling convention, not the model's predicted geometry.

The fixed corpus produced **9benchmarks/27process samples, all rejected for RSS**. All27processes exited0, but the runner rejects over-budget outputs before artifact validation and removes scratch data. Peak RSS **1,080,369,152–1,194,582,016bytes** (~1030–1139MiB), exceeding1GiB for every sample. Whole Python process wall time **6784–8739ms**, including imports/model setup and three same-session full-image passes; this is not inference-only latency. Temporary output max4,855,029bytes. No per-image timing artifact, geometry result or OCR comparison was accepted; the previous ONNX numbers are not reused as new results. Known photo2_2 was included and resource-rejected, so its previous quality regression remains unresolved.

A separately registered follow-up16×16RGB control checked final glue packaging under unchanged limits, not model quality or a replacement benchmark. Two initial preflight attempts rejected0644fixture/catalog permissions without starting inference; correcting both to0600 reached the model, but RSS **1,094,782,976bytes** again rejected output. Thus **28model processes total**,0accepted artifact catalogs. The final adapter registers mesh JSON as an existing generated artifact because unregistered scratch files are discarded; its end-to-end retention still lacks an accepted model run. Independent official sampling tests passed, but there is no accepted predicted source-bound mesh in this milestone.

Installed distribution inventory: torch alone **1,180,071,505bytes**, Transformers95,224,515bytes, torchvision23,722,159bytes, before31.7MB model and further dependencies. This exceeds the256MiB incremental package gate if shipping this installed path. These are current installed distribution files including a CUDA-enabled torch build; they are not the size of a minimal CPU wheel/dependency closure and do not prove all possible official CPU runtimes fail. No custom conversion or replacement build was attempted.

**Geometry deferred:** official mesh API and tiny sampling conventions verified; new full-image runtime measured and failed; independent real geometry/content truth missing; accepted map/image/OCR comparison unavailable. No production nonlinear apply or detector was added.

## Illumination eligibility

FSENet original source is MIT, checkpoint release `Weights` links `SD7K.pth`117,980,483bytes. Original README acknowledges/links the community ONNX port, whose releasev1.0.0 has `docshadow_sd7k.onnx`120,041,674bytes. Neither release publishes an asset digest; neither inspected README/release/license explicitly binds the separately distributed trained checkpoint's usage/redistribution terms. This is an unknown-eligibility finding, not a finding of prohibited use. No FSENet checkpoint or converted weight is downloaded or executed.

The community's unchanged DocShadowRunner supports selecting CPUExecutionProvider and official float32 RGB/255 preprocessing; static source support is not a local model-forward/parity proof. Original→ONNX output fidelity is unmeasured. SyntheticDoc's external checkpoint conditions remain unresolved after README recheck, so no optional substitution is made. Illumination remains deferred independently of geometry. Paired shadow/color/content truth is also missing.

## Alternatives and commercial reference

The [survey](correction-models-and-products-survey-2026-11.md) is reused as the bounded watchlist, reviewed on2026-11-10. ReWarp/BookNet/AADD/ArbDR/TADoc/MatteViT/DR-Mamba are revisited when usable local checkpoints, applicable terms and total CPU/edge-path measurements become available. DocRes needs all required model weights and full resource eligibility; DvD is coordinate diffusion, retained under its actual AGPL/resource constraints. MODNet is portrait matting; U²NetP's use in DocScanner does not make a generic checkpoint document-trained. Neither is promoted or retrained here. Noncommercial D2Dewarp/MMDIR remain ineligible for unrestricted product distribution.

Genius Scan's documented MobileNetV2 corner model, Readdle's staged detection and2026 NN lighting, MLKit/VisionKit native SDK adoption are source-backed references, not publicly reusable weights or scanner benchmark results. Adobe/CamScanner exact correction models remain unknown. Existing OpenCV-based OSS Document Scanner/jscanify and ScanTailor remain whole-pipeline references, not an invitation to create a custom algorithm.

Scanbot Linux/Web documents CPU/offline straightening and quality capabilities. Commercial fees, license/distribution conditions, available target-platform package and faithful preservation settings require a separate documentary/product choice. No quote, trial signup, SDK acquisition, contract, private upload or purchase was performed; no local SDK quality or timing is claimed. Revisit only if OSS candidates fail requirements and an explicitly authorized commercial evaluation is wanted.


## Final shared-path decision

Detection deferred for missing complete existing runtime; geometry deferred for measured RSS/package failure and missing quality truth; illumination deferred for checkpoint eligibility and paired truth. The user-approved bounded non-adoption branches close all five assessment phases; they do **not** deliver the six runtime/quality capabilities recorded as conditional-unavailable in the requirement disposition. No candidate is integrated, enabled or represented as a quality pass. The original/manual/no-correction path remains the product behavior.

Only evaluation glue, its tests, README and design/evidence reports change. Existing photo/Canon baseline, clean-scan no-op, source/hash/revision/owner/TTL/idempotency guards, explicit preview/apply/undo, Save/reOCR and old OCR preservation are retained. Actual iPhone/browser/Canon model quality is unaccepted; previous owner approvals are not reused as new correction evidence. No physical scan, private upload, new service/GPU/cluster operation, release, push or deployment.

Verification: **18Python tests**, **167Go test/subtest pass events** across evaluation/CLI/correction and **117race-enabled pass events** across selected document/HTTP correction/reOCR guards,0failures/0skips. Top-level test counts and exact raw-log hashes are in the [machine-readable summary](research/correction-comparison-v050/evaluation-summary.json). Existing full repository/browser/native-service suites were not rerun; no UI or production code changed.

The final mesh dtype boundary was tightened to float32 via a failing test and a passing final18-test suite; it was not rebenchmarked. Run configs retain the exact adapter hashes that produced resource failures. The follow-up diagnostic and the nine-image measurements are distinct evidence arms. Local raw receipts/planning phase verifications are retained and archived with a hashed evidence index. Raw dataset/model payloads and images remain in temporary local evaluation storage; no permanent dataset-retention guarantee is made.
