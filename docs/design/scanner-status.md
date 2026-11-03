# Scanner state and OCR-independent discovery

`GET /api/scanners` now provides registered scanner labels, verified complete
capture tuples, advisory operational state, the standard capture preference and
public limits. It is authenticated, no-store and independent of OCR catalog
availability. Existing `GET /api/capabilities` retains its OCR contract and uses the
same scanner projection when its OCR catalog is available. A missing OCR provider
must not prevent a capture screen from obtaining the separate scanner list.

The production readiness/physical handoff producer is still absent. Therefore this
work does not make real scanners idle/ready automatically. Native capabilities,
environment opt-in, a coordinator flock or an Idle response alone remain insufficient.

## Repository projection

`scannerreadiness.Source` returns internal metadata/status through a backend-neutral
port. `documents.Store.ReadScannerReadiness` pins an enabled principal and reads
coordinator, metadata, readiness, leases and aggregate scan/recovery barriers in
one document transaction. It validates exact boot/generation, freshness and native
metadata, then reads all requested registered devices in one bounded query. At
most 32 opaque IDs enter parameter placeholders; neither browser SQL nor per-device
SQL loops are used. The aggregate slot query is shared with admission/claim.

- `unknown`: missing, stale, consumed or mismatched evidence, absent lease, no
  resource-approved native choice, or unavailable current authority.
- `idle`: fresh current readiness at the exact idle/unowned lease epoch, at least
  one supported choice within the API pixel cap, consistent worker scan capacity
  and no aggregate scan/recovery barrier.
- `busy`: a valid current-boot held queued/running scan, or an otherwise-ready
  device when shared scan slots are full.
- `unavailable`: a live aggregate recovery/processing barrier or inconsistent
  worker capacity. Normal render work is still exclusive with scans in the current
  scheduler, so it can also make capture unavailable.

These are operational hints, not raw hardware diagnostics, per-user quota promises
or permission to acquire. Another command can win immediately after a read.
`POST` still validates ownership, receipt, revision, readiness, exact tuple, epoch,
capacity and resources transactionally. Completing a scan does not revive the old
readiness epoch. An expired device proof is downgraded independently; stale native
metadata clears choices and defaults rather than being extended by status reads.

Internal expiry bounds are checked again while forming the HTTP response. Hardware
IDs, handoff IDs, ownership/stop/idle times, native geometry, boot/generation, lease
epochs, worker capacity counters and job/owner IDs are never serialized. Public
content revisions change with selectable content/state, not proof/native timestamps.
Status errors fall back to verified metadata with unknown availability, never idle.
Reading state does not create evidence, initialize leases, cancel or contact a device.

## Verification and limits

Real API/repository tests prove metadata-only unknown → fixture-ready idle → held
busy → completed/consumed unknown → fresh proof idle → expiry unknown. Timestamp
refresh keeps the public revision stable; state changes update it. They cover
shared-slot exhaustion across two devices, terminal-held recovery barriers,
privacy, anonymous/query rejection and discovery without an OCR catalog. A
compiled serve test verifies authenticated discovery with absent OCR/readiness,
empty selectable choices and no fabricated idle state.

A test-only `database/sql/driver` wrapper delegates to the actual SQLite driver and
counts document-query calls: both 1 and 32 devices require exactly three calls,
plus the constant enabled-principal check in the separate state DB. This proves
the new scanner list avoids N+1; it does not claim every existing repository list
has completed a query-count audit. No product driver is patched or instrumented.

Still required: real ownership/readiness production and renewal, capture/scanner
selection UI, actual API→daemon→device/mobile acceptance, concurrent resource
headroom, cleanup/recovery, release/deployment and legacy writer cutover.
