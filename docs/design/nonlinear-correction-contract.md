# Nonlinear correction evaluation contract

Frozen before v0.4.0 model inference: 2026-11-10. Evaluation scope, not a new production API.

## Existing behavior

`internal/documents/page_effective.go` resolves immutable original scan or normalized photo.
`correction.go` joins current source/output hashes and checks owner/revision/TTL/idempotency before apply.
Save/reOCR remain source-specific. No-op reuses its current artifact and must not infer or resample.
`internal/correction/transform.go` and the old correction analyzer describe four corners/rotation;
their bilinear source mapping is not a projective or nonlinear ground-truth oracle.

## Nonlinear artifact requirements

A future nonlinear suggestion must bind source kind/page/revision, original and baseline SHA256,
baseline width/height, corrected-image SHA256/dimensions, model file hash/revision,
pre/postprocessor source hashes and runtime versions. A separate map artifact must have its
own SHA256, bytes, encoding and explicit coordinate convention. It must join this exact output
and source, never a mutable path or a four-corner approximation.

For UVDoc grid-based implementations the contract is float32 `[1,2,Gh,Gw]`, output-to-source
backward map in normalized `[-1,1]` x/y order, bilinear grid resize and sampling with
`align_corners=True` and zero padding. This convention belongs to the original UVDoc utilities;
it must not be presumed for another export without verifying that implementation.
Only a versioned, verified existing library/official implementation may produce or apply it.

Validation before sampling: bounded regular non-symlink artifact, actual bytes/hash, no pickle,
declared shape/dtype, finite coordinates, at least2×2 grid and no more than1024×1024 points.
Baseline/output limits: max12000px on either side and40MP. Out-of-bounds or incomplete map
is rejected for application; tests may use out-of-bounds maps only to prove padding behavior.
These checks validate data, never repair points or infer a missing map. Production integration
also needs owner/revision/TTL/cancellation/idempotency and atomic persistence of map+image.

The pinned Paddle UVDoc ONNX exposes only a corrected image. Evaluation may store it as
`correction` with `transform_kind=opaque-nonlinear` and `map_status=unavailable`, without
`crop_quad` or rotation metadata. The existing runner can retain this artifact and resource
receipt; the four-corner analyzer must not be run on it. It is **not apply-eligible** under this
contract. Do not modify upstream graph/source to expose a map as an untracked product patch.

## Library oracle

Use `torch.nn.functional.interpolate` and `grid_sample` for UVDoc coordinate semantics.
Independent tiny fixtures test identity, boundary fine lines, a known shifted sampling map,
zero padding, shape errors and nonfinite coordinates. These prove sampling conventions, not
model-predicted map correctness. A model map must be compared to independent geometric
ground truth; an output image or self-produced map is not its own correctness oracle.

## Frozen evaluation protocol

- Local evaluation only; preserve originals; no private document uploads or image redistribution.
- First choice of real-photo corpus: DocUNet cropped photos and corresponding flatbed scans,
  as provided for benchmark evaluation at https://www3.cs.stonybrook.edu/~cvl/docunet.html .
  Select first3 document IDs in numeric filename order, both capture variants if available,
  and corresponding scans. No selection based on model results. Record exact members/hashes.
  Record transport and dataset terms; public availability is not a redistribution license.
  HTTP-only official download has no publisher checksum: record this limitation, do not disable TLS.
- Public synthetic existing10 fixtures remain a separate diagnostic set, not real-photo evidence.
  Public photos must not be relabeled public-synthetic to bypass catalog validation; use the
  existing local private-catalog ingestion boundary and label actual provenance separately.
- Before first inference record corpus hashes and this contract hash. Same immutable PNG inputs
  for no-op, DocAligner and UVDoc. Any input normalization is lossless conversion only and recorded.
- Existing Phase44 benchmark: cold1/warm2 per fixture/arm (separate processes). For UVDoc add
  a same-process first+warm measurement inside the candidate to distinguish session reuse.
  This is candidate timing instrumentation, not a second process runner.
- CPU2 intra-op threads/inter-op1,30s request wall budget,1GiB sampled peak RSS,256MiB output/temp.
  Download/setup excluded and separately reported. Use network namespace isolation, RO input,
  existing runner resource guard and minimal environment. Do not describe sampled RSS as hard AS isolation.
- Resource adoption targets: all successful requests within above limits, warm full image
  processing median≤2s at actual frozen input resolutions; report p95 only with sample count.
  Original-resolution copies, CPU/runtime and model-only timings must remain distinct.

## Adoption gates (freeze before seeing output)

All gates required, separately for geometry and illumination:

1. Exact model/pre/post/runtime identity, usage/redistribution terms and offline packaging known.
2. At least one actual corrected image; all-fallback is non-adoption. No missing-content,
   out-of-bounds or blanking failures in the evaluated sample. Visual review supplements
   quantitative truth and never substitutes for it.
3. Independent geometry truth shows no loss of page content; numeric transcription and
   thin-line retention are exact on annotated regions. Corpus lacks these annotations → unknown,
   not pass. A small first3-document smoke cannot establish general household quality.
4. Fixed OCR engine/language/PSM; manually supplied independent transcription allows CER
   comparison. Median CER improves over no-op, no numeric-token regression, no per-page
   catastrophic regression (>0.02 absolute CER). Without transcription, text differences and
   character counts are descriptive only. Scanner scan OCR is a proxy, not transcription truth.
5. Illumination additionally needs paired/reference shading or annotated uniform background;
   no erased faint text, altered colored paper/photos or changed numbers. UVDoc alone does
   not establish illumination correction.
6. Resource gates above pass and a hash-bound map satisfying the artifact contract exists.
7. Real owner phone/Canon evidence remains unaccepted until supplied and tested. Prior owner
   acceptance of general UX does not accept a new model's image fidelity.

If any gate is unknown/fails: keep no-correction/manual defaults, record unavailable evidence
and non-adoption. Evaluation completion is not automatic correction delivery. No new GPU service,
native client, physical scan, private upload, public release or deployment is implied.
