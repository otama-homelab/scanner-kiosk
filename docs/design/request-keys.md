# Aged document request keys and receipt retirement

Document commands accept the versioned format
`rq1_<13 decimal server-time milliseconds>_<32 lowercase hex random>` alongside
legacy opaque keys. A key is an idempotency identifier, never authentication or
permission to acquire a scanner. It remains bound to the authenticated owner,
operation and canonical command. Changing a key expresses a new intent.

The rq1 new-admission lifetime is exactly 24 hours with at most 30 seconds of
future skew. These constants are part of the protocol version: changing the
window requires a new version and compatible retirement rules, not silently
extending rq1. The browser samples the server's HTTP Date and advances it using
performance.now, independent of the device wall clock. It uses fresh random UUID
bits for each intent and never stores document command keys in client storage.
Missing/unparseable Date never makes it fall back to an untrusted wall clock.

Every scan/render/edit/delete/cancel transaction performs exact receipt lookup
first. A matching retained receipt can replay after the new-key window, without
another acquisition/reservation or current capability resolution. A missing aged
key is validated before device, document or capacity changes; expired keys return
410, malformed/far-future keys 400. Rendering preflight also rejects an expired
missing key before provider resolution. Enabled-owner authorization still precedes
these checks. Receipt deadlines and immutable document/content TTL do not extend.

The UI retries uncertain results with the original key. A 410 shows explicit
English/Japanese reload guidance; capture blocks further POST and exposes Reload
view. Deletion exposes reload instead of retrying an expired key. Reload only
reads the view; it never automatically creates/repeats a native command. Other
operations show reload guidance in the status region. An old idle control can
expire after 24 hours; the user must reload and choose another action.

## SQLite schema 0016

`request_receipts.key_expires_at_ms` stores the parsed rq1 deadline atomically
with the receipt. It is immutable. Upgrade leaves every pre-existing row NULL:
a hash alone cannot prove which protocol generated it. Unknown legacy opaque
keys remain compatible, and their NULL tombstones are not eligible for deletion.
Thus legacy migration/sunset and state/auth receipt cleanup remain pending;
this does not bound all historical or arbitrary legacy-client metadata.

`PurgeExpiredRequestReceipts` selects at most 100 indexed rows whose key deadline
and receipt deadline have both elapsed. It deletes only those rows, cascading
expired edit-result metadata. Before deletion, the same transaction advances the
singleton `request_retirement_fence` to at least the cleanup clock. Neither its
value nor existence can be rolled back through normal updates/deletes. A new rq1
admission whose clock is below that persisted fence fails closed with storage
unavailable. This prevents a purged key becoming fresh again after clock rollback.
No key or user command is regenerated. A failed/uncertain commit acknowledges no
purge count; committed or rolled-back retry remains safe. No job, reservation,
artifact, content deadline or device lease is released by receipt retirement.

Startup drains full 100-row request batches before recovery inspection; idle
maintenance executes one bounded batch. This is a fifth command in the existing
five-second metadata-maintenance context. Completed cleanup status can become
eligible on a subsequent pass after expired edit results are removed. No physical
scanner/filesystem IO belongs in this command. The indexed query bounds results;
context limits database work rather than promising constant engine CPU.

The fence belongs to the same document store. Losing/restoring an older document
store, or replacing it with a fresh volume while reusing live sessions/admission
metadata, is not a supported recovery shortcut. Temporary document stores remain
excluded from configuration recovery/snapshots; volume-loss/session invalidation
and production rollback acceptance are separate unfinished work.

## Evidence

Go tests cover exact age/future boundaries, malformed rq1 and legacy distinction;
all five command metadata inserts; aged-key replay before receipt expiry; expired
preflight rejection; real scan admission, unstarted abort, receipt retirement,
rejection after retirement/clock rollback and acceptance of a separate fresh
intent; both deadline gates; 100-row batches, legacy preservation, edit cascade
and committed/rolled-back uncertainty with atomic fence behavior. Schema upgrade
preserves old tombstones and protects fence/age immutability; a forced table
conflict proves the added column/migration marker roll back together.

Real local HTTP tests cover scan 410, accepted same-key replay, simulated purge
clock advance and storage rejection under a rolled-back serving clock without
creating or releasing the original job/lease/reservation. These are synthetic
readiness fixtures. The compiled worker checker retires 102 aged hashes across
batches for a disabled owner while retaining legacy/live-key/live-receipt rows
and all jobs. Browser checks keep the same rq1 across explicit retry with a
1970 device clock and reject automatic POST on expired-view reload. Real
scanner, mobile, IdP, volume-loss and production retention acceptance remain open.
