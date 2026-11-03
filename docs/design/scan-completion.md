# Captured page publication and confirmed-stop cleanup

`internal/documents/scan_completion.go` now implements record-side completion.
It does not read a scanner, validate image contents, stop a process or observe a
physical device. Its evidence comes only from the trusted worker coordinator,
never from a browser or public completion endpoint.

## Successful capture

`CommitScanPage` takes a validated closed/synced PNG/JPEG original and a stop report
bound to device ID, worker boot nonce and lease epoch. The report must identify
joined local work and a subsequent hardware-idle observation, with timestamps after
the immutable job start and no later than the current clock. The coordinator must
also have removed scratch files and validated actual image geometry/color/DPI/hash.
The repository validates only internal metadata, reservation size and report binding.

The transaction rechecks enabled principal, owner, running scan job, runner,
current revision, job deadline, document TTL and held lease. It inserts the ready
original, producing-job link and next ordered page with the capture snapshot's
paper/color/DPI. Photo remains one page. It then clears the owned device lease,
releases the reservation, marks the job succeeded and returns the document to
review, all in the same commit. Creation/expiry never change. A final clock check
rolls everything back if the job/document expired before publication.

Cancelled, stale, wrong-boot/epoch, expired and over-budget results cannot publish
or unlock hardware. On uncertain commit, worker-only `CaptureRecorded` checks the
producing-job/page identity before file ownership is retained or discarded.
Actual filesystem/image validation and the orchestration of this handoff are pending.

## Failed or cancelled work

`FinishStoppedScan` is internal cleanup authority and may operate for disabled or
expired owners, since revocation must not strand hardware. It requires the same
job/device/boot/epoch, valid local-stop and subsequent idle timestamps, and evidence
that unpublished workspace cleanup finished after local work stopped. The caller
must join the entire execution path, not merely find no current child PID while
another launch could still occur. These are trusted reports, not cryptographic
proof or device observations made by the database service.

It commits terminal job state, reservation release and conditional lease clearing
together. Cancellation remains cancelled; other outcomes are failed/interrupted.
Existing pages return to review, an empty failed capture remains retryable, and
expired/deleting documents are never revived. Another document revision is not
overwritten. A stale completion cannot clear a lease now belonging to another job.
`ScanStoppedRecorded` gives content-free read-back after an uncertain cleanup commit.
If stop/idle/cleanup is unknown, retain the lease and reservation for reconciliation;
heartbeat age or an expired document is not termination evidence.

## Immutable start identity

Document migration 0003 adds `jobs.started_at_ms`. Claim sets it once, independently
of heartbeat updates; the database refuses to rewrite or clear it. Both scan and
render claim require it to be null, so resetting a started job to queued cannot
execute the same work again. Physical retry is a new explicit request/key. The
start timestamp complements worker boot/lease fencing, not an OS process identity.
Owned PID/start-time tracking and restart reconciliation are still pending.

## Verification boundary

Race-enabled tests cover original/page publication and atomic resource release,
two pages with independent capture settings and fixed TTL, stale/absent/future
reports, wrong owner/revision/boot/epoch, cancellation and expiry, budget/media type,
heartbeat changes after actual stop, immutable start identity, disabled-owner cleanup,
retention of earlier pages, old cleanup replay while a new job holds the device,
and commit-then-error read-back. Opaque metadata fixtures are not real PNG content
or physical stop evidence. Native adapter, image validation, scratch handoff,
ready/idle initialization, worker loop, HTTP/streaming and actual Canon acceptance
remain required before the application can use this boundary in production.

Actual staged JPEG/PNG validation is now available in
[the capture image validator](captured-image-validation.md), including a real PNG →
verified metadata → DB page-publication integration test. Native device capture,
physical stop/idle evidence and orchestration remain pending.

An explicit never-started branch, `AbortUnstartedScan`, now terminates queued or
pre-launch-cancelled work only while its exact lease is still held and immutable
start/heartbeat remain null. It races against claim in one transaction. It refuses
started or reconciling work; those still require actual stop/idle evidence. This
supports execution-preflight rejection without inventing a process-stop timestamp.


Capture preview extension: CommitScanPageWithThumbnails atomically publishes an
optional source-bound derivative under the same reservation; CaptureBundleRecorded
reconciles the full bundle without depending on mutable live-page presence. See
[thumbnails](thumbnails.md) for scope lifecycle, optional headroom and remaining
resource/native recovery acceptance. Original-only CommitScanPage remains supported.
