# Reserved PDF byte intake over HTTP

`POST /api/documents/import` accepts a raw `application/pdf` body, a known positive
Content-Length, one Idempotency-Key and one X-Content-SHA256 header containing the
lowercase hex SHA-256 of the complete body. Query strings, If-Match, content
encoding, duplicate control headers and media parameters are refused. Existing
session, Host/Origin, rate and concurrency checks apply before filesystem work.
The HTTP adapter sets a bounded read deadline before admission. It does not parse
PDF content or accept client filesystem paths, filenames, owners or stop evidence.

The global body limiter retains 64 KiB for existing JSON/control routes. Only the
exact import POST receives the bounded job-byte ceiling (at most 1 GiB); database
policy still reserves the full job ceiling and rejects inputs that leave no work
budget. Rejected header/authorization requests create no import job or file.

`importupload.Service` owns the filesystem producer and uses the existing intake
DB protocol. It admits/reserves first, starts the private producer fence, creates
its generated work directory and source file exclusively under an os.Root, and
streams at most the pinned length through a 64 KiB buffer while computing SHA-256.
The source file is created with mode0600 in a mode0700 directory. It never adopts
an existing directory/file or follows a final-file symlink, truncates input or
uses browser-provided paths. Registered staging metadata already protects the
work directory from recovery GC before it is created.

After the bounded copy it syncs and closes the source and syncs both its directory
and root. Only then does it seal the intake against the pinned size/digest. Failed
or cancelled copies retain their actual bytes/hash and finalize the stopped
producer before releasing reservation. A complete, synced intake may finish its
metadata commit under a short cleanup context even if the response was lost;
that does not bypass document cancellation/owner/deadline checks. Uncertain
metadata acknowledgements are read back against the exact producer/artifact.
Unknown IO or an unexpected preexisting path retains authority for reconciliation.
No caller treats an uncertain file as safe to unlink or overwrite.

An accepted/replayed request returns 202 and the owned minimal queued import job
with its Location. Replay does not read the body or create another producer. This
accepts sealed bytes for later processing, not valid PDF pages or a ready output.
The uploaded original is not a page or published download. Worker claim must still
require intake ready and invoke bounded PDF preparation before page publication.

Tests use real private files and SQLite plus a loopback TLS API. They verify an
upload over64KiB, identical stored bytes/private mode, replay, computed-checksum
refusal, partial/long/broken inputs, lost metadata acknowledgement, preexisting
file preservation, cancellation with no retained source FD, route-specific body
ceilings, authorization/header refusal and cross-owner job isolation. These tests
do not prove the hosted standalone executable or the complete upload UI.

The worker now performs bounded same-executable splitting and atomic page
publication (see `pdf-import-child.md`). Normal metadata maintenance terminalizes
withdrawn/expired/disabled-owner imports only when intake is pending or ready and
the processing worker never started. This serializes with intake start/worker
claim; receiving intake is never treated as stopped from a null worker start.
Known unstarted cancellation releases the reservation and leaves source ownership
with normal cleanup. Ready source bytes/hash are unchanged.

`worker recover --previous-runtime-stopped` also covers PDF intake and processing.
The assertion includes the entire previous API, worker and child processes.
Recovery reads the actual bounded partial source through a confined verified
descriptor, syncs/closes it, records its size/hash, and terminalizes the old job.
Missing never-created source is accounted as empty. Unsafe/unknown IO retains the
reservation. A ready never-claimed intake stays eligible for normal execution;
an already-started worker is interrupted after unpublished scratch cleanup.
This extension is source-tested, not hosted-candidate acceptance. Upload UI,
PDF preview/render integration and the hosted end-to-end tracer remain pending.

The job view now accepts the `import` kind in its existing status/cancellation
screen. This small validator extension is source-reviewed; the complete imported
PDF browser flow still requires the later hosted tracer and preview wiring.
