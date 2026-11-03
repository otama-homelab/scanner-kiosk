# Job-scoped artifact access

`internal/artifactscope` implements the OCR port's file access boundary on Linux.
It constrains a trusted compiled provider's interface; it does not sandbox arbitrary
plugin code, authenticate a user, or publish a document by itself.

## Coordinator handoff

1. Authorize the principal/document, immutable input revision, expiry and claimed
   job. Build the input allowlist from ready artifacts belonging to that job's
   document and owner. Supply opaque IDs, canonical relative paths, expected sizes
   and SHA256 hashes; never take a path from a browser or provider.
2. Open a scope under the private temporary document volume with byte/count limits
   derived from the admitted reservation. The scope copies this allowlist and
   creates a random private workspace. Local input handles are read-only; each
   opened descriptor's regular-file type, private permissions, size and hash are
   checked before the provider receives bytes. Captured originals must remain
   immutable throughout processing.
3. Run the selected provider with its cancellation/deadline context, then confirm
   its local work has ended. Validate the exact result page set, expected model
   version, output kinds and reference IDs. Reject duplicate, missing, foreign or
   unused output references. This result-set coordinator is still pending.
4. Seal only after every provider input/output handle is closed successfully.
   Sealing returns copied descriptors with ID, private relative path, kind, byte
   size and hash; these are internal records, not API file URLs. Content validators
   still need to check the requested PDF/text/image contract before publication.
5. The final DB transaction checks owner status, current revision, creation-based
   expiry, job cancellation and runner identity before publishing artifact metadata
   and output references. On confirmed failure, Close removes the scope workspace
   and preserves all input originals. On confirmed commit, Release transfers
   cleanup responsibility to document TTL/GC and Close will retain the files.
6. An uncertain DB commit is not a confirmed failure. Read back durable state and
   reconcile before deleting files; transfer responsibility to a durable recovery
   path if necessary. Release itself does not prove DB success or create visibility.
   Startup reconciliation must account for both referenced and orphan workspaces
   after confirming old workers have stopped. The record-side publication predicates and producing-job read-back are now implemented
   in `internal/documents`; the end-to-end coordinator/GC remains pending.

## Implemented bounds and lifecycle

The scope uses Go's rooted filesystem operations to confine relative input paths
and rejects final input symlinks, non-regular files, non-private files, traversal,
and root symlinks. Root and parent/mount configuration remain administrator-owned;
root confinement does not prohibit hostile bind mounts or replace host isolation.
Plugins cannot select output paths: opaque random IDs and filenames are assigned
internally with exclusive creation and private modes.

Writes enforce both per-artifact and shared whole-scope byte budgets under one
lock, with no partial over-budget write. Output count is limited. A write/count
failure poisons the scope so partial results cannot be sealed. Writer Close syncs
and closes its file, then renames the temporary name. Seal syncs directory metadata
before returning descriptors. No further input/output access is accepted after
sealing. Abort closes abandoned descriptors, removes only this workspace and syncs
its parent; cleanup errors remain visible and removal can be retried.

These limits cover writes through ArtifactWriter. Native OCR/PDF scratch files,
scanner capture, input staging, global filesystem capacity/reservations and
concurrent jobs need additional coordinator and deployment limits. A provider's
private scratch watchdog remains an observation bound with possible overshoot.
No SSD erasure, snapshot exclusion or five-minute TTL guarantee is proven here.

## Verification

Race-enabled tests cover allowlist and cross-scope isolation, digest/size mismatch,
symlinks outside the root, unsafe roots/paths, private modes, simultaneous aggregate
budget exhaustion, output count, abandoned descriptors, cancellation, seal/release
and abort preservation of originals. A local LLM mock writes through the real
scope and a failed-publication simulation removes only derived output.

The opt-in real Tesseract checker also runs environment loading → worker factory
composition → registry selection → actual Japanese/English OCR → scoped output →
seal → abort, for text and PDF text layers. It uses only the artificial 300dpi
fixture, and does not establish live LLM quality, final PDF assembly, authenticated
HTTP, durable job recovery, physical scanning or production acceptance.

## Native document outputs

`CreateArtifact` accepts ordinary `pdf`, `jpeg`, and `png` outputs. `CreateOutput` remains the OCR port and rejects these native kinds; OCR searchable PDFs remain distinct. Sealed metadata records the kind as a string without depending on OCR capability types. Both entrances use the same byte limits, sync, seal and cleanup lifecycle. `pdfassemble.GenerateScoped` resolves input IDs and writes a plain PDF through this boundary. The caller must still authorize/map the immutable job snapshot and coordinate conditional publication and uncertain-commit read-back.

## Multiple stages

`NewWithBudget` now optionally enforces a [shared stage output budget](artifact-budget.md). Seal and Release preserve charges; confirmed deletion refunds bytes. `New` preserves the existing independent-scope behavior. Native scratch and durable reservation accounting remain separate.
