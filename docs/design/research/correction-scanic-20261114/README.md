# Scanic correction evaluation — 2026-11-14

Status: evaluated, **not adopted or deployed**. Product correction remains incomplete. This report adds evidence; it does not change the canonical 7/14 completion count or original/full-image default.

## Actual execution

Unmodified Scanic1.7.1 and optional scanic-ml0.2.0 ran through the existing `scanner-eval benchmark` protocol on three existing public UVDoc independent diagnostic images. Classical and ML detectors each processed all three images, one cold run each. Assets were served locally, external requests blocked. No private documents or GPU services were used. Six completed processes are execution evidence, not six quality passes.

| Input | No-op mean corner error | Classical | ML | ML relative error reduction |
|---|---:|---:|---:|---:|
| 00000 | 11.794% | 21.794% | 13.362% | -13.29% |
| 00001 | 10.378% | 27.338% | 0.623% | 93.99% |
| 00002 | 8.444% | 5.108% | 0.603% | 92.86% |

Percentages are mean Euclidean corner distance divided by source image diagonal, reusing the existing diagnostic definition and independent grid endpoints from `correction-fairscan-numeric-v060`. No new pass threshold is introduced. Three diagnostic images cannot establish household accuracy. This is corner geometry, not proof of curved-page flattening or content preservation.

ML00000 predicts bottom-right y677 where independent truth is y1322; its document-presence score is approximately0.99999955. It therefore produces a seriously truncated proposal despite high confidence. Score thresholding cannot be treated as a crop-safety guarantee. Classical fails on the first two images as well. Keep all six cases in the denominator.

Cold whole-process wall time ranges1.582–13.799seconds, including browser startup, pin validation and PNG IO. Maximum process RSS is about249–256MiB; this is not aggregate browser PSS or phone memory. No warm or iPhone performance claim is made.

## Integration implications

The upstream library already provides a corner editor and extraction API; a new correction algorithm is unnecessary. Inspection of the distributed extraction code shows an eight-parameter projective transform with inverse mapping and bilinear pixel sampling: “bilinear” here describes interpolation, not a substitute for perspective correction.

Production currently has rectangular cropping only. Adding this library requires original-bound four-corner commands, trusted output generation, explicit review/apply/undo, and identical corrected pixels for OCR and save. A browser-uploaded corrected PNG alone does not satisfy provenance. Existing source/revision/owner and worker fencing must remain. Official Node integration additionally needs canvas/jsdom; runtime packaging, license inventory and output reproducibility have not been validated.

The MIT package declarations and model card were inspected locally. Final redistribution inventory remains part of adoption work. Do not add remote CDN dependencies to document handling.

## Evidence and outstanding work

`results.json` binds all source/output hashes, quads, independent truth, no-op comparison, resources and catalog hashes. Each detector/input directory retains the existing runner's benchmark and artifact catalog. Full PNGs, exact pinned assets and evaluation-only adapter remain in the local ignored `.planning/ops/photo-auto-correction-20261114/` directory; they are not production assets.

Still missing: OCR/digits/thin-line retention, representative phone images and real-device acceptance, scan-style color/shadow enhancement, trusted production corner-editing integration and release validation. No implementation or deployment is claimed by this evaluation.
