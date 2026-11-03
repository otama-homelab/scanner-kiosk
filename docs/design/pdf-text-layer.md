# OCR text-layer composition engine

`internal/pdftextlayer.Overlay` composes one unrotated image PDF page with one trusted OCR text-only PDF using upstream pdfcpu v0.16.4's stream-based watermark API and a stateless configuration. It accepts byte buffers and bitmap geometry, not paths or provider IDs. Provider selection, model/result validation, ownership and publication belong to the coordinator.

Both PDFs must contain exactly one page with zero rotation, zero-origin MediaBox and matching CropBox. The base must contain one embedded image; the OCR layer must contain no images and its aspect ratio must match the recognized bitmap within 0.1 percent. The layer is centered and proportionally fitted using the same placement policy as the product image import. Apply page rotation after composition so image and text rotate together. Input/output byte limits and context cancellation are enforced; final PDF structure/page count are checked. Run within the limited processing worker because PDF parsing/optimization can allocate beyond encoded bytes.

Image-free structure does not prove that an arbitrary PDF contains only invisible text. Layers must come from trusted renderer implementations: the local Tesseract adapter requests text-only PDF output, while positioned LLM text still needs its renderer adapter. This engine is not an arbitrary PDF upload sanitizer.

## Product integration evidence

The opt-in `ocrcomposition.TestInstalledTesseractPDFComposition` exercises environment loading → worker OCR factory/registry → actual Japanese/English Tesseract → sealed OCR scope → verified layer/original inputs in a new scope → product image PDF generation → product overlay → sealed final PDF → abort cleanup. It checks expected extracted text and original preservation. This is an artificial component chain; no DB admission/publication or executable-worker OCR dispatch occurs in it.

`check-tesseract.py` now also independently inspects the resulting PDFs with pikepdf/Pillow and renders both base/composed PDFs with pdftoppm. It verifies an image-free layer, unchanged original image pixels and identical rendered appearance. The expected Japanese/English text is extracted from the final PDF by the Go test. Unit tests reject images inside layers, multipage/rotated/corrupt/wrong-aspect input and byte-limit violations, including overflow during the final write.

The evidence covers clean artificial 300dpi horizontal RGB PNG pages and the synthetic JPEG/lossless fixtures described below. LLM positioned rendering, OCR intermediate disk quota accounting, worker provider-result orchestration, durable failure recovery, real scanner quality and production resource acceptance remain pending. The executable coordinator now connects compatible native text-layer providers; see [OCR worker pipeline](ocr-worker-pipeline.md).

## Ordered multi-page composition

`pdfassemble.BuildWithLayers` now requires exactly one layer per stable page ID, rejects missing/duplicate/foreign bindings before opening originals, creates each unrotated image page, overlays its own layer, applies the complete page's rotation, and merges in the image snapshot order. Layer result order is independent of output order. Input bytes, per-page/aggregate intermediate bytes and final output are bounded; a failure returns no partial PDF. [Provider result binding](ocr-result-binding.md) is now implemented as a separate coordinator boundary; shared intermediate/final disk reservation accounting remains pending.

The independent PDF checker now verifies distinct ALPHA/BETA layer text on the correct pages with reversed result order, mixed A5/A4 sizes, 180/90-degree rotation and JPEG/lossless encoding. Existing JPEG streams and decoded lossless pixels remain unchanged. The real Tesseract scoped fixture now recognizes two page IDs, composes letter/A4 output in reversed order with 90/0-degree rotation, extracts Japanese/English from each final page, and checks original pixels and rendered appearance against the equivalent image-only PDF. Both OCR pages intentionally use the same artificial bitmap; the distinct synthetic text fixture separately proves ID association.

The earlier one-page-only limitation is superseded by these component proofs. Executable-worker OCR dispatch, provider-result binding and shared planned stage budgets are now connected. Positioned LLM rendering, native scratch overshoot/storage acceptance, automatic recovery, real-scanner quality and deployment acceptance remain unfinished.

## No user configuration directory

The watermark source reader in pdfcpu v0.16.4 internally passes a nil
configuration, even when Overlay supplies an explicit stateless destination
configuration. With an unwritable HOME/config root, this attempted default
configuration creation and failed composition. The package now calls the
upstream `api.DisableConfigDir` once during package initialization, before
concurrent processing. All product PDF operations use application-controlled
configuration/fonts; user-installed pdfcpu configuration/fonts are not inputs.
This uses the official upstream API without modifying the dependency. The API
is deprecated in favor of per-operation stateless configuration, but that alone
does not cover this nested source read in the current dependency version.
A subprocess regression composes a synthetic layer with HOME and
XDG_CONFIG_HOME pointing to a regular file, so writable developer configuration
cannot conceal this requirement.

A retained physical A4/color/300 original also passed local Tesseract 5.5.4,
scoped result binding and lossless PDF assembly after this fix. Independent
pikepdf/Pillow/poppler checks confirmed one A4 page, original decoded pixels,
an image-free layer, unchanged rendering, and preservation of nonempty OCR
text through composition. No transcription content is retained in source.
Observed processing took about six seconds with a sampled process-tree RSS
maximum of approximately 247 MiB. This is a single-page observation, excluding
API memory and unobserved short peaks; it does not establish a deployment RAM
limit or transcription accuracy. Parent evidence:
`tasks/43/real-device-local-ocr-20261103/current-evidence.json`.
