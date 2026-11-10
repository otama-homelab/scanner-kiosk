# Correction algorithm candidates

**Surveyed:** 2026-11-09
**Scope:** evidence review and bounded prototype policy, not production adoption.

The product default remains the full-bed original with no correction. This survey
asks which classical, lightweight AI, browser-side and optional heavyweight
sidecar techniques can compete later. A candidate is not usable merely because
its source exists: the revision, license, model artifact, runtime, resources and
output contract must be reproducible.

The machine-readable matrix is
`scripts/eval/correction/candidates.json`. It is checked by
`scripts/eval/correction/test_candidate_survey.py`.

## Source-pinned survey

| Candidate | Family | Upstream task | License | Artifact evidence | Current status |
| --- | --- | --- | --- | --- | --- |
| OpenCV 4.6.0 perspective crop | classical | contour detection and four-point crop | Apache-2.0 | source tag `4.6.0`; no model weights | eligible; already measured by the Phase 45 harness |
| Leptonica 1.87.0 deskew/background | classical | rotation and background normalization | BSD-2-Clause | source tag `1.87.0`; no model weights | eligible; already measured by the Phase 45 harness |
| DocAligner 1.1.1 `lcnet100` | lightweight AI | document-corner heatmaps | Apache-2.0 | 4,767,987-byte ONNX release asset, SHA256 `f4117b78...e79594e` | eligible; runtime unavailable here |
| UVDoc 4c9b82b | lightweight AI | dense 2D grid unwarping | MIT | 32,158,393-byte checkpoint, SHA256 `7e90861b...f2b7da23`, 8,011,956 parameters | eligible; resources and nonlinear contract incomplete |
| DewarpNet 5f9e216 | heavyweight AI | 3D surface plus 2D backward-map networks | MIT source | final weights only behind Google Drive; no file hash or explicit weight terms | unavailable |
| DocTr 0446489 | heavyweight AI | geometric grid transformer plus illumination transformer | custom non-commercial | pretrained weights only behind Google Drive; no file hash | rejected |

### Classical and embedded

OpenCV and Leptonica are already the two measured classical arms in
`.planning/phases/45-scan/45-CORRECTION-COMPARISON.md`. Both use no model weights,
run offline and can be packaged as CGO libraries or bounded external processes.
That existing evidence is synthetic: it does not establish household-document
quality, OCR improvement or an acceptable resource budget.

OpenCV has a larger measured peak RSS than Leptonica and no correction. Leptonica
is smaller but changes rotation/background without semantic knowledge of the
page. Neither failure mode may silently replace the original.

### Lightweight AI and edge runtimes

DocAligner is the smallest verified AI artifact in this survey. It predicts four
corners; the caller still owns the perspective warp. The pinned ONNX file can be
run by a CPU/GPU provider, and ONNX Runtime Web exposes a possible WebAssembly or
WebGPU path. Browser packaging and operator support have not been proven here.

UVDoc predicts a nonlinear grid and unwraps the complete image. Its weights and
eights-million-parameter model are pinned in the source repository, but the
current `crop_quad` analyzer cannot represent its nonlinear field. Its official
demo also expects the Python OpenCV package, which is absent in this workspace.
Running or comparing UVDoc therefore needs an approved pinned runtime and a
nonlinear correction artifact contract.

### Heavyweight sidecars

DewarpNet has repository source under MIT and an optional PyTorch container path,
but its final model files are published only through Google Drive. Without
immutable file hashes and an explicit weights license/redistribution statement,
the candidate is unavailable and must not run.

DocTr's custom license permits non-commercial use only and requires written
permission for commercial use. Its weights are also unpinned. It is rejected for
this product survey rather than deferred silently.

## Deployment matrix

| Deployment | Evidence | Decision |
| --- | --- | --- |
| Pure Go | no source-backed nonlinear dewarp implementation was found in scope | deferred; a new implementation would need its own algorithm and tests |
| CGO / native library | OpenCV and Leptonica are pinned and already measured externally | only viable as an optional bounded process until packaging benefit is proven |
| ONNX Runtime | DocAligner publishes a pinned 4.7 MB ONNX asset | candidate for a later runtime test |
| WASM / WebGPU | DocAligner's ONNX export and ONNX Runtime Web make this technically plausible | unverified; no package, browser export or WebGPU fallback exists in this workspace |
| Optional container sidecar | UVDoc, DewarpNet and DocTr require Python/PyTorch | sidecar only; no production import or ambient network/provider call |

No package is installed by this plan. Missing runtimes remain unavailable.

## Prototype policy

The matrix selects at most two non-heavy candidates and one heavyweight
candidate. Every prototype must use the existing Phase 44 `scanner-eval run` or
`benchmark` protocol and emit a normalized `correction` PNG. The selected set for
this plan is:

1. OpenCV perspective crop - already measured; rerun one bounded fixture.
2. DocAligner `lcnet100` - attempt only if a pinned ONNX Runtime is already
   supplied; otherwise record the exact missing runtime.
3. DewarpNet - unselected for execution until immutable weights and weight terms
   are supplied; record why it cannot run.

The Phase 44 analyzer can compare linear crop/rotation transforms. Nonlinear
grid candidates cannot claim quality through that analyzer until the artifact
schema is extended through a later explicit design decision.

## Decision

The only candidate advanced for follow-up is the measured OpenCV perspective
crop. This is not a production adoption and does not change the default. The
follow-up must first compare multiple edge/content fixtures, a private household
corpus when supplied, OCR text/reading-order benefit, and a documented resource
budget.

Leptonica remains a deferred classical option. DocAligner is deferred until a
pinned ONNX Runtime/browser runtime exists. UVDoc is deferred until a nonlinear
correction contract and runtime are available. DewarpNet remains unusable until
its final weights have an immutable hash and explicit redistribution terms.
DocTr is rejected because its non-commercial license does not fit a product whose
later monetization remains an open milestone decision.

## Resource and proof limits

- Upstream model sizes and parameter counts are recorded only when published or
  measured. Unknown RAM, disk and latency values remain `null` with a note.
- Existing OpenCV/Leptonica resource values are synthetic-corpus measurements,
  not household budgets.
- DocAligner's runtime and browser paths are unmeasured in this workspace.
- UVDoc's nonlinear quality, CPU/GPU latency and memory are unmeasured.
- No private household bytes or OCR improvement evidence were supplied.
- Synthetic fixture success is harness evidence only.
- The one-fixture OpenCV rerun does not establish broad quality or edge safety.

## Sources

- OpenCV 4.6.0: <https://github.com/opencv/opencv/tree/4.6.0>
- Leptonica 1.87.0: <https://github.com/DanBloomberg/leptonica/tree/1.87.0>
- DocAligner 1.1.1: <https://github.com/DocsaidLab/DocAligner/tree/1.1.1>
- DocAligner release assets: <https://github.com/DocsaidLab/DocAligner/releases/tag/1.1.1>
- UVDoc: <https://github.com/tanguymagne/UVDoc/tree/4c9b82b537057aff2526e6dd118a847cdd072e82>
- UVDoc paper page: <https://igl.ethz.ch/projects/uvdoc/>
- DewarpNet: <https://github.com/cvlab-stonybrook/DewarpNet/tree/5f9e216535f048d98ca44bbfb5f6ea55f8cdfe13>
- DocTr: <https://github.com/fh2019ustc/DocTr/tree/044648919b6b28f2cb1b288db6d8fd97749db537>
- ONNX Runtime Web package: <https://www.npmjs.com/package/onnxruntime-web>

## Follow-up — 2026-11-10

After the owner requested continuation, the fixed DocAligner model completed a CPU-only ONNX Runtime1.23.2 availability smoke in a disposable, network-isolated environment. This supersedes only the current missing-runtime assumption; the2026-11-09 survey and failed/unavailable prototype records remain historical. See [phone photo correction next work](photo-correction-next.md) for exact observations, scope limits and the next same-corpus experiment. No geometric quality, OCR benefit, phone performance or production adoption is asserted.
