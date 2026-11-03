# Worker thumbnail generation

`internal/thumbnail.Build` creates JPEG derivatives from a fully validated PNG or
JPEG original. It preserves source orientation and aspect ratio (integer rounding
within one pixel), never enlarges a small source, and strips source metadata.
Rotation stays in page metadata. Supported source modes match capture validation:
opaque 8-bit color or gray, exact expected geometry, bounded bytes and pixels.

The maximum edge is explicit and bounded to 2048; encoded output is capped at
8 MiB and quality is explicit. The application coordinator must choose the actual
policy and account for all derivatives in its disk reservation. Box averaging
retains alternating thin strokes that nearest-neighbor sampling can drop. This
is a screen preview and must never replace original images used for export/OCR.

`captureimage.Decode` shares all existing Validate checks and returns the verified
bitmap so a consumer needs only one full-size decode. Validate remains compatible.
Image export also consumes that verified bitmap instead of decoding again. Source
decoding still requires a worker with memory/CPU limits: Go decoder checks are not
an OS sandbox or hard processing deadline. Cancellation is checked while reading,
resampling and writing. A decoded bitmap never escapes on validation failure.

The Linux GenerateScoped adapter accepts only a coordinator-supplied allowlisted
input and creates a JPEG in the existing private artifact scope. Its byte, output
count, sync/seal/release and abort lifecycle follow that scope. No filename or path
is accepted from the browser. The caller must seal, commit the owner/original
binding and release; closing an unpublished scope removes its derivatives while
preserving originals. Scope output kind is JPEG (encoding); document record
kind is thumbnail (purpose), not a new OCR plugin output.

Tests decode output independently, verify dimensions and color, prevent upscale,
cover fine stroke preservation, malformed geometry/color/truncated data, byte,
pixel and output limits, cancelled work, input immutability, input allowlisting,
sealed metadata, successful release and abort cleanup. They exercise native image
libraries in-process with synthetic originals, not scanner acquisition.

The worker render path now opts into a 512-pixel, quality-82, at-most-1-MiB JPEG
policy. Missing derivatives are generated from verified immutable originals after
export preparation. Export and derivative scopes share one job byte budget. A
single transaction publishes the output, thumbnail artifacts, source bindings and
producing-job links, terminalizes the job and releases its reservation. Their
aggregate bytes must fit that reservation. Existing source bindings are discovered
within the input query and reused on subsequent renders, including after rotation
or page reordering. A derivative is never used as an OCR/export source.

Migration 0010 adds immutable same-owner/same-document original-to-thumbnail
bindings with ready-kind/media/size constraints. It creates no ready device or
work and preserves existing jobs and leases. ReadPageThumbnail joins a currently
retained live page, original and derivative; deletion/expiry/disabled owners cannot
read it. Missing derivatives return 404 with no original fallback or decoding in
the HTTP process. The HTTP route uses the existing hash-verified private file
stream and repeated owner/session/expiry checks, with no-store and HEAD support.

The client overview and image strip use the small derivative; selecting Page view
uses the original preview. A failed thumbnail request falls back to the authorized
original once per mounted image. Thus retained or newly captured pages without a
derivative remain usable. No background hardware request or browser image cache
is used to fill a missing derivative.

Tests cover bundle aggregate quota and rollback on late invalid inputs, ownership,
current job/runner/revision, cancellation, expiry, disabled owners, metadata edits,
page removal, uncertain commit and no partial publication. Real Go HTTP tests decode
a generated JPEG and cover HEAD, missing/cross-owner/cross-document reads,
revocation, original deletion state and corrupt files. A real network listener is
required because ResponseRecorder deliberately lacks write-deadline support. The
compiled worker checker independently verifies derivative hash/bytes, JPEG size,
gray pixels and unrotated source orientation; browser checks verify switching from
thumbnail to original and back after real worker output.

Remaining integration: independent backfill/repair for retained pages without a
derivative, unknown-native/unpublished crash recovery, and measured real resource
acceptance. Render still rejects a combined export/derivative exceeding its
reservation. Capture preview policy and its optional headroom fallback are below.
No actual scanner capture or production deployment is claimed by these fixtures.


## Capture-time publication

Normal worker scan tasks now wrap their records with a fresh per-scan
`scanthumbnail.Publisher`. After validated native capture and a fresh post-stop
physical-idle observation, it generates a source-oriented 512-edge/quality82 JPEG
in a private artifact scope, before confirming scan publication. A workwatch pulse
and the immutable scan deadline bound generation; owner/revision/cancel/TTL loss
prevents publication. Originals remain the sole export/OCR inputs.

The native original already consumes part of the immutable scan reservation. A
preview has a separate at-most-1-MiB output budget within remaining headroom, and
its source read is capped at min(configured job limit, reserved bytes, 1 GiB).
If the original leaves less than 1 MiB headroom or exceeds that preview input cap,
the valid original can commit alone and the existing authorized original-preview
fallback applies. Pure optional-output limit errors can fall back only after
confirmed scope cleanup and fresh authorization. Joined storage/commit/monitor
or cancellation failures are never downgraded. No new quota reservation or device
acquisition is fabricated to complete a preview.

CommitScanPageWithThumbnails inserts original/page plus optional thumbnail/binding
and both producing-job links in one transaction, checks their aggregate bytes
against the scan reservation, then terminalizes and releases lease/reservation.
The immutable document TTL and input revision stay unchanged. A late invalid field
rolls back the entire bundle. Existing original-only callers remain supported.
No migration is required beyond the existing schema15/source binding schema10.

The coordinator's optional ArtifactLifecycle releases additional scopes only after
confirmed publication. Failure releasing a published derivative leaves Prepared
published/reconciliation-required and cannot call native original cleanup. For
unpublished work, derivative cleanup must finish before the original and its lease
are released. On an unknown commit, CaptureBundleRecorded compares immutable
producing-job metadata, exact original/derivative ID/path/media/bytes/SHA and the
source/geometry binding. A committed-but-inconsistent bundle is an error requiring
reconciliation, never a false result permitting file deletion. Page rotation,
reordering/removal, document state changes and owner disable/TTL cannot turn an
already committed bundle into a negative acknowledgement. CaptureRecorded was
corrected to use immutable original-producing links instead of live page presence.
Unknown/unreadable outcomes retain all scopes for reconciliation; writes and
hardware capture are never automatically repeated.

This synchronous step keeps the scan's lease until the bounded bundle commit,
after hardware has already stopped/been observed idle. It adds a worker bitmap
decode during that interval. It is not a claim that device leases are released
before all CPU work, nor a new independent background thumbnail scheduler. Source
validation already decodes the capture; peak RSS/CPU with large originals and
parallel devices remains a real resource acceptance requirement.

Tests cover bundle aggregate quota/rollback, stale owner/runner/revision/expiry,
metadata mutation after commit, preserved uncertainty and independent failed
cleanup/release. Publisher tests inspect real JPEG geometry/pixels/hash and scope
preservation/abort under committed/rolled-back/unreadable outcomes, tiny and large
reservations, and never hide independent errors with optional limits. Runtime
tests use fixture native executables/local eSCL and explicit fixture readiness to
verify preview publication before any export, original-only low-headroom fallback,
two devices concurrently publishing isolated previews, cancellation and no repeated
capture. A real local HTTP listener serves the capture Publisher's JPEG before any
render and revokes it after page removal. These are synthetic/virtual fixtures,
not Canon/mobile/physical readiness or native crash/resource acceptance.
