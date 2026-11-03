# ADR 0002: Isolated native PDF processing and versioned OCR providers

Status: PDF engine selected for implementation; OCR adapters and positioned-text renderer require implementation/acceptance.

Use unmodified pdfcpu v0.16.4 as the initial PDF assembly/editing engine. An artificial-image spike confirmed exact A4 dimensions, byte-preserving JPEG embedding, pixel-preserving RGB/gray PNG embedding, page selection/reorder/removal, individual rotation, and merging. pikepdf/Pillow independently inspected the Go output; they are verification tools, not a Python application-server dependency. The upstream license is Apache-2.0, and dependencies must be included in release notices/SBOM.

Each invocation uses a stateless configuration and a new job-owned staging output. Import APIs must receive no existing PDF input when creating a fresh document; do not accidentally use the library’s append-existing-output behavior. The application owns revision checks, deadlines, quotas, output validation, and publication. A successful library return is not sufficient to publish a file.

Run heavy PDF operations in an owned child process with bounded memory/output/time. Native context cancellation is useful but does not replace process termination and reap. The spike tested a pre-cancelled operation only; it did not establish mid-operation cancellation, crash recovery, resource limits, or physical scanner behavior.

Tesseract and LLM OCR use the common `pkg/ocrplugin` contract. They are independent of PDF assembly and scanner capture. Output capabilities distinguish plain text, positioned text, and a searchable PDF. A text-only model cannot be offered as a searchable-PDF provider. A positioned-text provider needs a separately installed, verified text-layer renderer before the application can advertise searchable PDF as an effective output.

The first Tesseract adapter will invoke the packaged CLI with installed, versioned Japanese/English data, emit a searchable PDF or positioned output, and retain the captured originals. UI language IDs map explicitly to engine language IDs. Quality presets are per-model mappings, not cross-engine accuracy rankings.

The LLM adapter will call an explicitly configured provider through a protocol-specific adapter. It must transcribe rather than summarize, require bounded structured output when positions are requested, and never execute tool calls or instructions found in the document. The initial protocol is now an explicit Chat Completions vision + strict JSON-schema adapter, verified against local mock HTTP (see the follow-up below). Each deployed model/endpoint and renderer packaging still need acceptance; no generic vision endpoint is declared compatible without it.

Stirling is not a mandatory deployment dependency. Its existing independent service is left running; a future processing adapter can reuse it without moving ownership or retention into that service. Paperless delivery is deferred from the initial release.

## Follow-up feasibility results

A real Tesseract jpn+eng run on a clean artificial 300dpi page produced text-only PDF layers. Go/pdfcpu composition retained the original PNG pixels and JPEG DCT bytes while preserving Japanese/English text extraction. This validates the layer/assembly split for that fixture, not the full OCR quality matrix.

Synthetic positioned Japanese/English results were also rendered into a text-only PDF with unmodified OCRmyPDF 17.13.0/fpdf2 using its supported font-provider interface, then composed by Go. A fixed Japanese font was selected without glyphless fallback, and extracted word-box centers matched the known fixture within 5pt. This is a viable CLI-helper candidate for positioned LLM output, not a live model test. It adds an optional Python processing toolchain while the web/API/worker control remains Go; no Python HTTP service is involved. Packaging and protocol/model selection are still pending.


## LLM protocol decision and executable evidence

The first LLM adapter uses Chat Completions image data URLs and strict
`response_format.json_schema` for compatibility with self-hosted gateways. No cloud
model, endpoint or automatic fallback is selected by default. This is a protocol
choice, not a claim that every "OpenAI-compatible" server supports vision or strict
schemas. An OpenAI-specific new application may prefer Responses; additional
protocols can register their own adapters without changing document services.

Upstream wire references checked 2026-11-02:
[Chat Completions reference](https://developers.openai.com/api/reference/resources/chat/subresources/completions/methods/create),
[image input](https://developers.openai.com/api/docs/guides/images-vision),
[structured output and refusal handling](https://developers.openai.com/api/docs/guides/structured-outputs).

`internal/ocr/llm` now implements this port. Profiles pin request and exact response
model IDs; their non-secret configuration is fingerprinted into the model revision.
Only text and positioned text are native outputs. Mock integration tests verify
image bytes, schema, normalization, bounds, cancellation, refusals, incomplete or
wrong-model responses, secret redaction, and redirect rejection. No live model or
transcription-quality claim is established by those tests.

The product text-layer composition engine is now implemented; see [PDF text layers](../design/pdf-text-layer.md). A real synthetic Tesseract → scoped layer → product image PDF → composition chain passes independent image/render checks. Full worker OCR orchestration and LLM renderer packaging remain pending.
