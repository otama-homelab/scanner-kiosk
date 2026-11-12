# Scanner

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

The owner uses a smartphone and PC to import photographs/images or acquire pages from a Canon scanner, check documents and save an output file.

## Product Purpose

Turn scanned or imported originals into usable saved documents. The owner confirmed the intended primary sequence: import → correction → OCR → save. Correction and OCR remain optional when they are unnecessary; saving an ordinary PDF must remain available.

## Operating Context

The same web app supports physical scanner acquisition, image import and camera capture. Documents may have multiple pages. Current correction controls include existing manual crop/rotation and undo. Recognition can produce a candidate that is compared with the saved result and explicitly adopted or discarded. The prior saved file remains available during optional processing.

## Capabilities and Constraints

Keep the existing Go/Astro/TypeScript application, deployed services and native runtimes. Reuse existing correction and image-processing implementations; the owner explicitly disallows inventing custom correction algorithms. Do not introduce a model, training, external data destination, physical scan or private-document replay as part of UI design. Preserve exact result/source bindings, revision checks, retention, bounded artifact reads, cancellation, uncertainty recovery, idempotency and explicit publication of a recognition candidate.

## Brand Commitments

Preserve Scanner's product name and recognizable document logo. The owner requests professional, modern design that is not excessively sparse or recognizably generic AI output. Design the layout around the actual work sequence and minimize eye travel. Paperless-ngx, similar OSS, commercial document tools and scanner/kiosk control panels are requested references; their visual/task quality is the bar, not a request to copy every feature.

## Evidence on Hand

The owner confirmed current OCR results work. Synthetic browser/API/worker fixtures and public example documents exist. This observation is narrower than complete per-device or correction-model acceptance. Unqualified automatic correction and automatic crop detection must not be implied by the interface.

## Product Principles

1. Keep the original visible as the stable reference; relate each recognition result to its own actual source.
2. Arrange acquisition, correction, recognition and save in the order they are used.
3. Keep related controls and decisions near the content they affect, with stable positions.
4. Preserve the original and usable prior output through optional operations and failures.
5. State unsupported or unavailable behavior plainly; do not invent confidence or imply verified accuracy.

## Accessibility & Inclusion

Support responsive smartphone and PC use, Japanese/English, keyboard navigation, visible focus, touch controls and light/dark appearance. Real-device and screen-reader acceptance remains separate from synthetic Chromium checks.
