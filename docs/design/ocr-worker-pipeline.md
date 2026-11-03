# OCR jobs in the executable worker

The worker now dispatches OCR-enabled PDF render jobs through `renderjob`'s `OCRSelector` port. It lazily initializes environment-configured provider factories only when a job requires OCR. Basic PDF/image jobs never initialize optional OCR, and an unavailable OCR installation can fail its job safely without preventing a later ordinary export. Provider/model/version/languages/preset come from the immutable admitted snapshot; there is no provider/model fallback.

The current searchable-PDF path requires the selected model's native `pdf_text_layer` capability. Tesseract supports it. Configured positioned LLM profiles can expose this capability through the local renderer when SCANNER_OCR_LLM_TEXT_LAYER_FONT provides an approved font. Text-only or unconfigured profiles do not advertise PDF layers; selecting an unsupported output fails explicitly. Local adapter/runtime tests do not replace a complete worker-to-published-PDF or real-model acceptance test.

## Processing and ownership

Preparation validates claimed ownership/revision/deadline, input hashes and bounded geometry, then runs recognition with copied request arrays. A shared admitted-byte budget covers OCR scope outputs, native planned staging and final scope output. The entire sealed provider result is bound by pinned version, exact page/artifact coverage, kind and bytes before creating final-layer inputs. Final assembly uses a new scope, fits the remaining/file byte bound, pairs stable page IDs, applies rotation after overlay and validates the merged PDF.

OCR intermediates are deleted and synced before publication; the final closed/synced output remains in its scope. The existing conditional DB publication and read-back handle successful/uncertain commits. Confirmed failures clean all tracked stage scopes before terminalizing the job and releasing its durable reservation. Originals and creation-based retention remain unchanged.

A provider's common reconciliation marker, or an unknown final-scope creation outcome, preserves stage resources and sets execution uncertainty. `Prepared.Close`, `Finish` and `Publish` refuse to treat that as stopped/clean work. The worker stops dispatch and startup rejects active old jobs. This is conservative retention, not automatic native recovery; there is no user-facing endpoint to fabricate stop evidence.

## Evidence

`TestInstalledWorkerOCRCommand` builds the real product command and invokes `worker --once` in an isolated environment. It admits a synthetic two-page OCR document through the real DB service with installed Tesseract metadata, recognizes Japanese/English, rotates the second page, publishes the owned PDF and checks terminal state, reservation deletion, native/intermediate cleanup and unchanged originals. `check-tesseract.py` additionally compares embedded pixels and rendered appearance of that published PDF against the equivalent image-only PDF and extracts text per page independently. No physical scan or HTTP endpoint is involved.

Default race tests inject a layer provider and verify publication, request-array isolation, bad version/page rejection and unknown-execution retention. Another runtime test fails an unavailable OCR installation, then successfully renders the same retained originals without OCR.

Remaining work includes live-model and deployed catalogue/API acceptance, native watchdog overshoot/hard storage acceptance, durable automatic startup/native recovery, scanner readiness/capture, HTTP/auth/session/UI integration, TTL GC, container/release and real mobile/device acceptance. This executable component is not a deployed scanner replacement.

`TestLLMWorkerCommandPublishesPositionedPDF` builds the product command and runs worker --once against isolated real databases and a local HTTP model mock. Two positioned responses become separately searchable pages, with the second page rotated. It checks publication, no reservation retention, original bytes/TTL preservation and that each retained work scope has a ready artifact reference. The initial fixed-one-scope assumption was invalid because final thumbnails also have published scopes. This proves the local mocked command pipeline, not real model quality or deployed UI/catalogue acceptance.

The authenticated HTTP catalogue/admission regression now uses capabilities from an actual configured LLM adapter. It verifies model-version identity in capabilities and the admitted immutable job, endpoint privacy, live font-removal rejection and receipt replay despite changed/stopped catalogue metadata. Catalogue publication in this test is explicit through the real DB store; the worker publisher loop and browser are not simultaneously exercised.
