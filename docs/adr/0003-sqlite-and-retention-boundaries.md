# ADR 0003: Two local SQLite stores, durable jobs and explicit device fencing

Status: accepted design; migration/invariant tests pass, application services still need implementation.

Use modernc.org/sqlite v1.61.1 through database/sql, with Go 1.27.3, no external database or Redis initially. One state store keeps identity/session/protocol state; a separate temporary store keeps document/job/artifact metadata beside the corresponding temporary files. This lets content retention and backup exclusion apply to the entire document region without including it in configuration recovery.

Each database records its kind and checksum-verified migration history. WAL, per-connection foreign keys, bounded busy timeouts and IMMEDIATE transactions support the API/worker sharing one local RWO filesystem. Startup retries only actual SQLite lock contention within a deadline. Unknown/newer or changed migrations fail; a wrong store kind cannot be opened as the other database.

Composite foreign keys prevent cross-owner document/page/artifact/job associations. Immutable capture/job/retention fields and revision-bound published output pointers protect historical inputs. These are not row-level authorization: all application queries and streams still require a validated principal and expiry check.

Persistent jobs and request receipts allow browser recovery without rescanning. A duplicate request key/body returns the same result; deleting the result leaves a content-free tombstone during the 72-hour deduplication window. A different body conflicts. Resource reservations are transactional, and physical scanning is admitted only with a reconciled per-device lease.

Each lease retains a monotonically fenced epoch. Heartbeat expiry does not release it. Real process termination and hardware reconciliation must be demonstrated before the application admits another physical scan. Interrupted scans are not automatically retried. This is a single-replica design, not a claim of distributed device fencing under arbitrary split-brain.
