# Original-criteria audit: ILL-01 / ILL-02

Verified 2026-11-11T13:21:29Z. Independent read/hash review only; no model, numeric metric, build, download or external-message execution. Scope is the original REQUIREMENTS.md and Phase67 roadmap, not later SUMMARY assertions about additional gates.

**Conclusion: SD7K artifact provenance/terms are now proved sufficiently for ILL-01's determination; ONNX output equivalence fails its unchanged test. ILL-02 remains incomplete because the required content comparisons are absent. Neither conclusion implies quality pass or product qualification.**

## Original contract and evidence

ILL-01: “FSENet公式checkpointと変換済みONNX、任意SyntheticDocについて、利用・取得・実行・再配布の条件と変換出所を証拠付きで確定できる。”

ILL-02: “適格な既存照明モデルをpaired truthで実行し、影低減・文字／写真／色／印影保持とCPU資源を比較できる。”

Phase67 SC2 additionally calls for author-linked conversion output parity, color order and dimensions, and safe isolated loading. SC3 specifies paired6/clean3, PSNR+1dB, clean SSIM≥0.99/channel MAE≤1/255, annotated digit/thin-line/seal missingness0, photograph/color cases, and separate resource budgets. This is a validation milestone: an observed measured failure is a result; absent comparisons are unfinished work.

| Criterion | Verdict | Actual evidence and consequence |
|---|---|---|
| ILL-01 publisher/license/release scope | PROVED for selected SD7K artifacts | Pinned official MIT source, same official repository's named pretrained FSENet Weights release/asset and MIT author-linked exporter provide artifact-specific distribution context. Record notice obligations; no captured exclusion. This is more than an unrelated generic MIT string. A bespoke permission letter is not an additional prerequisite. |
| Exact checkpoint and ONNX identities | PROVED | Official SD7K.pth117980483B SHA2566a514e34cf2d3aff12eebd0c4f7b04466f47c1c0c41019d81091cc4d2defdc56; ONNX SHA2568c09b9320a0fb3c53806cdf9cb8410b3706c77caed81f4ec1cb0bf2cbd14b049. |
| Conversion-source parameter correspondence | PROVED | All991 state parameters map to all992 ONNX initializers:990 exact name/shape/dtype/raw-byte matches; remaining source-defined coefficient row/broadcast maps to two constants.117592520 parameter bytes match, no unmatched entries. Source/graph Mul/Add evidence is explicit, not parameter fitting. |
| Safe CPU loading and scope of the fork | PROVED | weights_only=True/map_location=cpu succeeds; official strict state load succeeds. Fork patch changes only this loader line; architecture/forward/data/infer remain bound to baseline. This is an explicitly identified CPU portability fork, not an unmodified official CUDA run. |
| Matched CPU input/color/shape contract | PROVED for ten native cases | Exact RGB input-tensor byte hashes and NCHW shapes match; output dimensions match.512 is correctly excluded because preprocessing differs. No claim covering all possible inputs. |
| ONNX numeric output equivalence at the fixed tolerances | CONTRADICTED | Existing comparison is4/10 allclose pass and6/10 fail at atol1e-5/rtol1e-4; maximum absolute difference5.8531761169433594e-5. Keep that measured-fail. Parameter identity does not make runtime arithmetic identical. |
| ILL-02 actual eligible existing-model execution | PROVED | Current official SD7K weights safely run in identified CPU fork:22 forwards, native10/12 then resource stop, full51212/12 spanning six shadow/clean pairs. “No current FSENet CPU execution” is obsolete. |
| Global shadow/clean quality | CONTRADICTED as a quality pass; measurement performed | Native PSNR0/5,clean MAE0/5,SSIM0/5.512 PSNR4/6,clean MAE0/6,SSIM3/6. Clean failures already reject these tested configurations; do not tune thresholds. |
| CPU resources | PROVED as measured failure/partial native run | Native aggregate sampled PSS2180001792B exceeds2GiB after ten saved predictions.512 completes with parent RSS814211072B and aggregate PSS1147172864B. Preserve different RSS/PSS quantities; do not claim a hard RSS cap or native12/12 completion. |
| Required digit/thin-line/seal and photograph/color content comparisons | INCOMPLETE | Global PSNR/SSIM/channel MAE do not establish region-level preservation or missingness. Dedicated current-output results for these annotated features are not supplied. This is the concrete remaining ILL-02 comparison, even though existing global failures already reject adoption. |
| SyntheticDoc | Not required as a replacement | Explicitly optional second candidate if its own conditions resolve. It need not be downloaded or tested to determine the selected SD7K path. |

## Completion recommendation without new gates

**ILL-01:** the provenance/conditions determination can be marked complete for the selected official SD7K checkpoint and parameter-identical author-linked ONNX, recording the numerical conversion check as **measured-fail**, never quality_pass. The roadmap's parity check has been performed and failed; the ONNX must not be represented as a passing interchangeable implementation. If a status scheme requires positive equivalence for ONNX eligibility, classify that candidate as rejected, rather than endlessly leaving its provenance unknown. Original Phase67 still cannot pass while ILL-02 comparisons are absent.

Exact exporter build host, historical CUDA environment, the historical checkpoint container-file hash, byte equality between PTH and ONNX containers, and a fresh official CUDA run are **not explicit original ILL-01 requirements**. The measured parameter mapping and source/export evidence establish the selected weights' origin without reconstructing an entire historical build. A CUDA parity result would be required for a claim of CUDA equivalence, which is not the chosen CPU-fork claim. The fixed CPU allclose failure remains fully visible; this recommendation does not relax it.

The prior label “old ONNX versus unrelated current model” must be corrected: this SD7K pair has identical mapped parameter values. Distinguish artifact containers, execution graphs/runtime arithmetic and preprocessing configurations. Dates, URLs or model nicknames cannot override direct parameter evidence. Do not generalize this proof to Jung/Kligler or every export.

**ILL-02:** retain incomplete/partial with exact missing content rows, not blanket rights-holder/CUDA-loader waiting. The minimal remaining action is to compare the already retained outputs and existing paired6 annotations for digits, thin lines, seals and photograph/color regions using an existing applicable analyzer and an explicit native/512 coordinate contract. No fresh model run is needed to close that comparison. If no existing analyzer supports the required transform, identify that specific measurement implementation gap. Retain every observed content failure as measured-fail; zero missingness is not assumed.

The full512paired6/clean6 run already covers the required sample count. Native's two missing peach cases and its measured resource failure must be reported, but a native rerun is not an extra universal prerequisite for evaluating the complete512 configuration. Owner-device acceptance is ACCEPT-01 in Phase68, not an ILL-02 completion prerequisite. Product adoption and clean-scan integration remain ACCEPT-02.

## Original-photo truth follow-up

The separate follow-up now proves six original-photo/scan pair mappings, including photo2_2→scan2; the flatbed reference is available. Canonical9 hashes are unchanged; archive bytes differ from canonical reencoding, while decoded RGB/shape match. Scan alpha is nonopaque, so visual compositing equivalence is not proved. Independent original-photo digits/thin-line coordinates/transcription and reviewer/date/source binding remain absent. These are concrete TRUTH-02/VERIFY-04 gaps; they do not require new photographs, repeated corpus discovery, or substitution of OCR/LLM truth, and must not be conflated with the already fixed paired-synthetic ILL-02 comparison.

## Evidence checked

Read source loader patch, strict-load stdout, exact-input output-comparison rows, coefficient/source/graph mapping, primary release/license/export sources and original-truth bindings/selfchecks. Rehashed108 CPU-fork indexed artifacts,30 identity indexed artifacts and8 original-truth artifacts:146/146 identities match. No checkpoint was deserialized and no metric or model was rerun in this audit.

The summaries' demands for “exact export build lineage” and “official CUDA parity” as automatically remaining original ILL-01 gates are interpretations, not the text of the original requirement. Their parameter-identity and allclose measurements remain valid evidence. Their owner-evidence omission belongs to Phase68; the legitimate unresolved ILL-02 content measurement remains explicit above.

Skipped: inference, numeric recomputation, new tests/builds/downloads, private pixels, root state/ledger updates and Git changes.
Risk: closing provenance does not reverse the measured parity/quality/resource failures or authorize model adoption.
