# Correction review and recovery

Correction is an optional derived view of an immutable scan page. The full-bed original remains the page's authoritative source for preview, rotation, reorder, deletion, PDF/image export and OCR input.

## Capability boundary

The API exposes correction only when `page_corrections` contains a ready suggestion bound to the current page original. No suggestion means no correction control. This phase proves the review/apply/undo/reprocess contract with synthetic artifacts; it does not enable OpenCV, Leptonica or Tesseract as a production correction engine.

The artifact table stores the suggested PNG as an immutable `intermediate` artifact because its legacy closed kind enum predates correction. The correction role and source binding are owned by `page_corrections`, which requires:

- owner/document/page;
- the page's current original artifact and SHA256;
- a ready PNG intermediate artifact and SHA256;
- bounded transform JSON;
- `suggested`, `applied` or `discarded` state.

A partial unique index permits at most one applied correction per page.

## State transition

```text
suggested -> applied -> discarded
     |          |
     +----------+-> suggested (another suggestion after undo/reload)
```

Opening a document or the comparison dialog sends no processing request. The dialog loads the existing authorized original and suggestion previews and shows dimensions, rotation and provider-specific confidence.

Apply uses:

- `POST /api/documents/{document}/pages/{page}/correction`
- `If-Match: "<revision>"`
- `Idempotency-Key`
- `{"suggestion_id":"...","source_sha256":"..."}`

Undo uses `DELETE` with the same revision/key rules. Both operations:

- reject stale revision/source and cross-owner access;
- invalidate the current published output because the selected source view changed;
- increment the document revision;
- never change `pages.original_artifact_id`, original bytes, page order or page rotation.

Replaying the exact intent returns the stored projection. A different body under the same key conflicts. Unknown network/storage outcomes are not replayed automatically; the browser disables the command and requires reload/reconciliation.

## Fallback

`page_corrections.risk_flags` is the authoritative policy source. The document store rejects apply whenever that list is non-empty, including `edge-risk`, `low-confidence`, `fallback` and unknown future values; UI state cannot override the rejection. Malformed or oversized risk evidence also fails closed. The full original remains available. A failed or unavailable preview and a correction capability outage never disable ordinary image/PDF save, OCR on the original, rotation, reorder or deletion.

After undo the projection may expose another suggested correction. Applying that new suggestion is the reprocess path; it does not rescan the physical page.

## Verification

- Go store tests prove owner/revision/source binding, one applied row, undo and reprocess without changing the original.
- document HTTP tests prove explicit commands, conflict mapping and content-free unknown errors.
- `scripts/check-web-correction.cjs` proves no request on view/dialog open, two-image comparison, apply/undo/reprocess, unknown-result lock, bilingual labels and responsive widths.

These checks prove the optional correction workflow contract only. They do not measure or accept a correction algorithm, household-document quality, scanner input behavior or production resource budgets.
