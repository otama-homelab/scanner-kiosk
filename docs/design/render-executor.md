# Single claimed-render execution

`renderjob.ExecuteClaimed` drives one already-confirmed claimed render through authorization monitoring, actual PDF generation, conditional publication and confirmed failure cleanup. The caller must dispatch it once after a successful claim, retain any returned recovery object, and prevent overlapping work. It is not an executable worker, scheduler, retry queue or claim-outcome reconciler.

Preparation errors now carry private evidence of whether this invocation's synchronous work has ended and its workspace was removed. Errors before workspace allocation can report no owned files. Once scope creation is attempted, cleanup is considered unknown until confirmed; an unknown creation outcome retains the reservation for reconciliation. A failed scope cleanup returns a discard-only Prepared object so cleanup can be retried. Files are never treated as cleaned merely because the work context was cancelled.

When cleanup is confirmed, the executor completes the job using the existing stop-report transaction. Persisted cancellation becomes cancelled; context cancellation/deadline becomes interrupted; ordinary failures become failed. The original processing error remains visible even after successful cleanup. Generation/publication is never automatically restarted.

On uncertain publication, the returned Prepared object must be retained for `Publish` read-back; `Finish` refuses to delete unresolved publication. On cleanup or uncertain cleanup errors, the object is retained for `Finish` retry/read-back. Confirmed publication may also return an object if the final scope release reports an error; retrying `Publish` only repeats release. A nil object and nonnil error can mean an unknown scope-creation outcome: do not assume reservation release or start another invocation of the job.

## Verification and remaining work

Actual SQLite and synthetic-image integration tests cover successful PDF publication, corrupt input, pixel limit failure, persisted and parent cancellation, committed-but-error publication, rollback, unavailable publication/cleanup read-back, and inaccessible scope roots. They assert terminal state/reservation behavior, original retention, and no repeated publication. Earlier tests cover scope budgets, monitor joining and exact-runner stop predicates.

Executable worker startup/dispatch, photo export and compatible text-layer OCR now exist. Durable recovery-object tracking/startup reconciliation, native process isolation/resource acceptance, positioned LLM rendering, scanner adapter, HTTP/session integration, cleanup scheduling and real-device acceptance remain pending. No live service or replacement deployment is implied by these component tests.
