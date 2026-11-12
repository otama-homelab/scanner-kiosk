# Correction-first document workspace

The owner confirmed OCR recognition itself works, but the comparison/source previews were duplicated and the interface still looked generic and required excessive eye movement. The confirmed normal task is **import → correction → OCR → save**. Correction and OCR remain optional; ordinary PDF saving stays available.

## References

- [Paperless-ngx document detail](https://github.com/paperless-ngx/paperless-ngx/blob/dev/src-ui/src/app/components/document-detail/document-detail.component.html) and its [official editor screenshot](https://github.com/paperless-ngx/paperless-ngx/blob/dev/docs/assets/screenshots/editing.png): one stable original beside the work and source/version context. This is an interaction/craft reference, not a pixel comp or a feature-parity claim.
- [Scanner Pro crop/rotate](https://support.readdle.com/scannerpro/enhance-and-edit-your-scans/how-to-crop-scans): finish framing/rotation before text recognition and export.
- [HP scan preview/edit](https://support.hp.com/sk-en/document/ish_2275901-2074064-16) and [Canon preview-before-save/send](https://oip.manual.canon/USRMA-9996-zz-SSM-660-enUV/contents/devu-scan-set.html): inspect/edit acquired pages before output; some devices expose OCR as an output-format setting rather than a separate mandatory screen.
- [Acrobat recognition review](https://helpx.adobe.com/acrobat/desktop/create-documents/scan-documents-to-pdfs/fix-scanned-text.html): relate recognized words to original image positions.
- [Nanonets review](https://docs.nanonets.com/docs/file-review): an explicit review/approval decision. No multistage review service is introduced.

## Implementation

The unreleased v0.2.23 tab-only candidate was cancelled after the owner requested stronger professional craft and correction-first flow; its tag was not moved and nothing was deployed from it.

The document route now uses compact neutral chrome, an Imported → Adjust → OCR workflow row and a stable Save action. One selected original and its local edit controls replace the multi-card overview. The same selected page is preserved across correction/text views, reordering and reload. Other pages remain reachable through the page control.

OCR review owns one exact-result source viewer and an adjacent text/decision pane. Saved/New/Compare switches keep ordinary review focused on one result; comparison is explicit. Source changes always use the matching immutable result artifact, rotation and coordinates. Narrow screens open the source on request. Desktop transcript content scrolls inside the working area so zoom and candidate decisions remain reachable. Adopting/discarding retains the current work context; uncertainty checks and idempotency remain intact.

Document-specific CSS lives in `web/public/document-review.css`; common appearance SVGs and existing global styling remain in the base assets. Paperless delivery retains its own component class, separated from OCR styling. No correction algorithm, model, backend/schema, dependency, network destination or physical scan is changed.

## Verification and limits

Preserve all existing source identity/hash, bounded artifact reads, cancellation, retry/uncertainty, retention and explicit whole-document adoption/discard checks. Adapted UI regressions exercise one-page selection without losing page edits, multi-page PDF access, correction/undo, OCR source/geometry/order, responsive Japanese/English, light/dark, enabled/selected control contrast, and actual API/worker output hashes. Independent Impeccable finish review and exact stable-source/CI/publication/production checks are required separately.

Synthetic Chromium and public fixtures do not constitute owner iPhone/PC acceptance, correction-model qualification or completion of the original scanner milestone. The owner statement that OCR results work is recorded at that narrower scope.

## Publication follow-up

The v0.2.24 candidate passed all seven hosted required checks and independent staged OCR/PDF/source/license/advisory verification. Its publisher stopped after making the 48 source and inspection assets public, before either version image appeared. The incomplete v0.2.24 release and tag are retained; latest and production remain v0.2.22. Read-only hosted diagnostics load both exact candidate images successfully and reproduce the expected absent-version manifest response, so the original transfer failure's cause remains unproven. The writer previously discarded its command failure reason; bounded diagnostics now retain the command and exit reason while removing its credential and URLs. No publication gate, overwrite protection or retry behavior is weakened. The next unused version is v0.2.25 and requires fresh exact-source hosted checks and publication proof; the unchanged UI proof can be retained only while every recorded input hash still matches.
