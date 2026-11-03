# Document records boundary

The document HTTP adapter consumes `documenthttp.Records`, `RenderAdmission` and
`ScanAdmission`. It parses requests and projects responses; it does not open a
SQL connection, assert a concrete store, perform storage CRUD or reserve quota.
Runtime composition currently supplies `documents.Store` (the SQLite adapter)
and the existing scan/render admission services.

Records expose authorized document/history/job projections, atomic edit,
cancel/delete intent and internal sealed file references. Each implementation
must check the enabled owner and retention inside its operation. Mutations must
preserve revision, idempotency, fixed request retention and commit uncertainty.
A delete/cancel result is intent, not proof of process termination, file removal
or released capacity. There is no HTTP method for an unguarded artifact path.
Every new file request and repeated check during a transfer resolves the same
reference through Records. A reference change or lost authority denies content.

Backend atomic commands keep the state→documents lock order and principal guard,
quota reservation, device fencing and artifact publication invariants. The HTTP
port does not split these into individual CRUD calls. The existing admission
ports still perform replay lookup and trusted capability resolution before an
atomic admission command; HTTP supplies no reserved-byte or worker-boot authority.

## Evidence

`TestDocumentHTTPRecordsInjectionCommandsAndFileReauthorization` uses a local TLS
HTTP listener, real session lifecycle and rooted immutable file streaming with a
non-SQL document/job backend and injectable admission services. It covers owner
and anonymous denial, history cursor/detail projection, edit/delete/cancel
command arguments, scan/render intent, malformed revision rejection, error
projection without unconfirmed results, GET/HEAD/Range and revocation between
initial file lookup and post-hash authorization. Session records use SQLite in
this fixture; the document implementation does not access it. The synthetic
file bytes test streaming, not PDF/image decoding or native capture quality.
This fixture proves HTTP injection; it is not a complete backend implementation
or a substitute for SQLite authorization/concurrency/migration contract tests.

`TestHistoryQueryCountIndependentOfDocumentsAndPages` wraps the actual SQLite
SQL driver, counting executed queries (including prepared-statement fallback).
It seeds 101 owned documents, 202 pages and current outputs, alongside another
owner. History pages of 1, 20 and 100 documents and the final one-document page
use one state principal query and three document queries: roots, pages, outputs.
Detail uses the same count. Empty history and cross-owner missing detail use
one state and one document query; disabled owners issue no document query.
Results retain pages, current output revision, owner isolation and pagination.
This bounds round trips, not database CPU: correlated scalar metadata and scans
still perform work proportional to the selected records. It does not prove the
query count or execution plan of every other catalog/listing.

## Remaining boundary work

`documents.Store` still owns SQL transactions and domain validation together.
Its worker commands are consumed through existing scan/render/cleanup job ports,
and runtime startup/maintenance/cleanup dispatch plus the OCR/render wrapper now
consume ports as described in [worker startup](worker-startup.md). Runtime
composition still constructs the SQLite adapters.
Separate backend contracts and common mutation/concurrency/migration tests remain
necessary before enabling an alternative production database. No PostgreSQL
backend, cross-store atomic commit or automatic scanner failover is claimed.
