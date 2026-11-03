# Confirmed-stop render completion

`documents.FinishStoppedRender` is an internal worker cleanup operation. It accepts trusted runner identity, processing-stopped time and workspace-cleaned time after immutable job start. Times must be ordered and no later than the current clock. This is a report contract; the database cannot stop a process or inspect files and must never accept these fields from a browser.

The transaction requires a claimed running/cancelling/reconciling render, its exact runner and outstanding reservation. Any producing-job artifact or device lease rejects this cleanup path. A successful export cannot be converted into a failure. Cancellation intent yields `cancelled`; other accepted outcomes are `failed` and `interrupted`. Reservation deletion and the terminal job state are atomic. Original artifacts/pages remain untouched.

Expired/deleting documents remain inaccessible. An unexpired current revision returns to review when original pages remain; a newer revision is not overwritten. This worker-only operation remains usable after a principal is disabled. `RenderStoppedRecorded` supplies boolean read-back for an uncertain cleanup commit; it grants no content access and is not proof of resource termination.

`renderjob.Prepared.Finish` now connects this transition to synchronous PDF work that has already ended. It refuses published or unresolved-publication results, removes the unpublished scope first, and only then reports completion/cleanup times. An uncertain transition is read back before retry. Confirmed cleanup is idempotent at the coordinator. A cancelled context does not prevent the bounded, independent read-back.

## Verification and remaining boundaries

SQLite/race tests cover failure/interruption/cancellation, disabled/expired/deleting owners/documents, stale revision, foreign identity, pre-start/future/reversed timestamps, unstarted/published jobs and uncertain commit read-back. Coordinator tests generate actual synthetic PDFs, cancel/abandon them, remove generated files, release reservations and preserve originals; fault injection covers rollback and unavailable cleanup read-back.

`ExecuteClaimed` now consumes preparation stop/cleanup evidence and reports failures before a normal Prepared object exists; unknown cleanup retains reservations. No automatic retry, scheduler, cancellation monitor, native process termination, startup reconciliation or TTL collector is implemented here. This path does not claim physical scanner idle or release scanner leases.
