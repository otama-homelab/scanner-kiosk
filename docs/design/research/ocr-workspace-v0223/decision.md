# OCR review workspace

The owner confirmed OCR recognition itself works. The requested change removes repeated source images from result comparison and the document preview. Keep the existing Impeccable Operate theme.

## References checked on 2026-11-12

- [Paperless-ngx document-detail template](https://github.com/paperless-ngx/paperless-ngx/blob/dev/src-ui/src/app/components/document-detail/document-detail.component.html): Content and metadata navigation, a dedicated desktop preview, and a mobile Preview tab. Adopt its separation of content and preview, adapted to this app's explicit old/new OCR review. This implementation uses a consistent two-tab view at every size; it does not claim to reproduce Paperless's desktop layout.
- [Adobe Acrobat recognition review](https://helpx.adobe.com/acrobat/desktop/create-documents/scan-documents-to-pdfs/fix-scanned-text.html): relate recognized words to their original image positions. Retain exact-source highlights and zoom on demand. Editing OCR words or estimating suspect words is outside this change.
- [Nanonets File Review](https://docs.nanonets.com/docs/file-review): explicit approval as a separate review action. Retain explicit whole-document adoption/discard and usable prior download. No multistage workflow is added.

The public Paperless usage page returned HTTP403 to the page reader; its current repository template was inspected instead. These references guide interaction, not a new framework or copied source/assets.

## Design and invariants

- Preview and recognized text are mutually exclusive document views. Preview remains the default without a new candidate; a new candidate opens text review. Remember the chosen view only for the same document and candidate identity.
- Text is primary in recognition review. Show saved/new results side by side on desktop, stacked on mobile. Show model identity and keep full provenance collapsed.
- Source images open only on request. Native exclusive disclosures allow at most one exact-result source image at a time. The main document preview stays hidden while reviewing text.
- Preserve source revision/hash, PDF rotation, coordinate validation, artifact limits, unavailable-source behavior, expiry, abort/reload/locale handling, keyboard focus, idempotency and explicit adoption/discard. Never substitute the current document bitmap for an older OCR result.
- Recognition commands stay available in either document view. No recognition request or source mutation occurs when switching views.
- No OCR provider/model, image-processing algorithm, backend API, dependency, network destination, schema or retention change.

## Verification

The focused comparison regression checks 320/390/1280/1440px, exclusive source disclosure, rotated highlight geometry, tab selection/keyboard and preservation of exact result sources. Full browser and actual API/worker save/recovery checks remain required before release. Owner-device acceptance remains separate.
