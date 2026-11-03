# Quarantined artifact removal and durable completion

`internal/artifactgc` is a Linux worker-only file adapter. It accepts one immutable
manifest input (artifact ID, relative path, size, SHA256) under a pinned private
root. The caller must hold the worker guard and have confirmed cleanup claim/load.
It has no scanner, session, scheduling or DB dependency.

Each path component opens through a directory descriptor with O_NOFOLLOW and is
checked for private permissions and current Unix ownership. Dot-prefixed paths,
worker control paths and document/state SQLite paths are refused. Regular files
must be private, single-link and match expected size; symlinks, hardlinks, devices,
FIFOs and directories are refused. There is no recursive deletion.

The file moves atomically into `.cleanup/<job>/<artifact>` using RENAME_NOREPLACE.
An occupied quarantine is never overwritten. Both directory sides are synced.
The isolated descriptor is verified against the entry, fully hashed with bounded
bytes/context checks, and checked again before unlink. Unlink is followed by a
directory sync before a typed removal acknowledgement is returned. Mismatches
stay quarantined without deleting the unverified bytes. Same-job/artifact retries
resume verified quarantine or confirm/sync absent leaves. Missing parent paths,
unsupported rename/fsync and other uncertainties fail closed. Files are never
copied across filesystems as a fallback.

The private quarantine has one conforming writer under the exclusive worker
guard. Checks are not a security boundary against a malicious process with the
same Unix UID, which can also modify the DB or application executable. The adapter
pins the opened root and does not follow a replacement root path. Guard liveness
and deployment ownership must still be checked by the runtime coordinator.

`RecordCleanupRemoval` binds a trusted post-sync worker report to manifest ID,
size/hash, owner/job/runner and start/time bounds. It records per-file progress
without releasing quota. `CleanupRemovalRecorded` handles lost DB acknowledgements.
`FinishCleanup` requires every file to be acknowledged and a complete unchanged
manifest plus no producer/lease/reservation barrier. It atomically removes pages,
thumbnail/output bindings, edit-result projections, old processing jobs, artifact
records and the private manifest, and marks the cleanup job succeeded. Removed
processing jobs make their earlier request receipts gone. Only the minimal owned
cleanup status/document tombstone and request hashes remain for later retention
purge. Completed physical work can be reported after the IO deadline; starting
new file work still requires a successful current load. Lost completion commits
are reconciled through content-free CleanupFinishedRecorded.

Tests use disposable private temporary files only. They cover real removal and
absent retry, cancellation/resume, hash and source/target replacement rejection,
occupied/symlink/public quarantine, traversal/control files, symlink parents/leaves,
hardlinks/FIFO/public modes, zero-byte artifacts, directory-sync failure before and
after unlink, and pinned-root replacement. DB tests cover partial progress,
misbound/future/pre-start reports, last-file gating, disabled/expired owners and
committed/rolled-back acknowledgement loss. An integration test executes real
file removal followed by per-file DB acknowledgement and final quota release for
synthetic immutable source/output/thumbnail bytes; it does not claim PDF/image
content validation or scanner acquisition.

The coordinator and ordinary worker dispatch are now connected for eligible
withdrawn/expired documents; see [cleanup worker](cleanup-worker.md). Pending are
retroactive legacy/orphan directory sweeping, receipt-hash retirement and actual
production volume/resource acceptance. Completed status retirement and user
controls are now connected as described in document-deletion.md. No production files or old scanner service have been changed.


## Empty directories and absent retries

After a manifest leaf is removed and its directory synced, Remove now retires
empty pinned source ancestors bottom-up, then the job quarantine and .cleanup
when empty. It uses no recursive deletion or filesystem-wide scan. Each ancestor
is opened nofollow/private-UID, retained as a dirfd, and matched against its current
parent entry before AT_REMOVEDIR. A nonempty ancestor stops pruning and preserves
all unrelated content. The root, database/lock control paths, public directories,
symlinks and replaced directory identities are never removed. Ancestor depth is
bounded to 64 so one manifest cannot exhaust descriptors.

Directory removal is synced in the parent before a progress proof is returned.
Failure or cancellation after unlink/rmdir returns no positive removal proof;
the next same-manifest invocation can recover with absent source ancestors.
It syncs the nearest existing parent and verifies/removes the same job quarantine,
without shortening the source path into that nearest ancestor. Missing parents
are distinct from symlink/public/permission errors. This lets a restart after a
successful rmdir but lost DB acknowledgement complete safely. Tests cover nested
ancestors, unrelated siblings, nearest-ancestor name collisions, pinned-directory
replacement, sync failure after source/job-quarantine/.cleanup removal, and retry.
Compiled worker tests check published scope and quarantine directories disappear
when their last manifest file is removed.

This path handles directories reached by current pending manifest files. A separate
startup sweep now discovers known flat producer and quarantine directories left
empty by already-acknowledged pre-upgrade files, as described below. Nonempty
untracked/native work is not recursively discarded.


## Legacy empty-directory startup sweep

After predecessor metadata withdrawal, startup metadata draining and a confirmed
clear unknown-work barrier, the worker streams the private document root through
`artifactgc.EmptySweep`, before configuring any native device/provider or starting
new scan/render/cleanup work. The exclusive worker guard must remain active and
bound to that same root. Runtime rechecks the guard/root before every sweep step.
A blocked recovery gate performs no sweep; no running/uncertain producer is made
safe by observing its directory empty.

The allowlist matches the actual flat artifactscope/SANE-capture namespaces:
`work_` or `capture_` followed by 48 lowercase hex characters. It also visits valid
opaque job directories directly under `.cleanup`, then attempts to retire that
container when empty. It does not descend into workspaces, `.native`, probe/config
namespaces or arbitrary nested directories, even when nested children are empty.
Those paths can have independent native lifecycle requirements. This is not a
recursive orphan-file collector; untracked bytes remain a separate recovery task.

Root and optional cleanup enumeration use independent pinned nofollow/private-UID
directory descriptors. A batch examines at most 64 entries, including ignored
names, and the cursor continues until EOF without retaining a whole directory
listing or restarting at the first nonempty entry. The public adapter bounds a
batch to 1..256 entries; descriptors/depth remain constant. Startup uses the
existing SCANNER_CLEANUP_TIMEOUT_SECONDS budget and caller cancellation; context
checks are cooperative filesystem deadlines, not an interruptible-IO guarantee.

Each candidate is opened nofollow/private, matched to its current parent entry,
and removed only with AT_REMOVEDIR. Nonempty directories, regular files, symlinks,
public/foreign directories and unknown names are preserved. Cleanup's current
identity is rechecked before touching any pinned child. A replacement or storage
failure stops startup. Parent fsync follows every successful rmdir. Root and
cleanup are synced on enumeration/resumption as well, settling an absent directory
whose previous rmdir acknowledgement was lost. No manifest progress proof, job
completion, quota release, lease change or receipt deletion is fabricated by this
filesystem-only maintenance. Existing pending-manifest absent-parent retry remains
responsible for actual cleanup acknowledgement.

Tests stream 205 old empty work scopes, seven empty capture scopes and 103 empty
job quarantines over multiple batches; retain nonempty originals and all control,
native/probe, nested, public, regular and symlink entries; reject candidate and
cleanup-parent replacements; and resume safely after authority/cancellation/fsync
failure. Another test creates a real sealed/released artifactscope, simulates a
legacy acknowledged leaf unlink, and recovers its actual generated directory.
Normal runtime tests drain 137 directories plus capture/quarantine at startup,
preserve retained bytes, repeat startup, refuse sweeping unknown prior work, and
stop/retry on authority loss, cancellation and deadline. These are local private
filesystem fixtures, not production retention or native crash acceptance.
