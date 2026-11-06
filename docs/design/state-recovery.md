# Logical identity recovery

`scanner state export PRIVATE_ARCHIVE_PATH` and `scanner state restore
PRIVATE_ARCHIVE_PATH` recover durable authentication records without restoring
sessions or temporary documents. This is one recovery component; scanner/device
reconciliation, storage placement, production cutover, complete configuration
recovery and upgrade/rollback acceptance remain separate gates.

## Recovery boundary

Format `scanner-kiosk-state`, version 1, currently requires the exact checked
state schema 5. Export verifies every embedded migration name/checksum without
creating or migrating the source DB. It reads one SQLite snapshot containing:

| Included | Preserved |
|---|---|
| OIDC/simple principals | Opaque ID, kind, creation time, disabled time |
| OIDC identities | Exact issuer/subject to principal mapping |
| Local credentials | Canonical username, bounded Argon2id verifier, credential version/timestamps |

Guest principals, sessions/revocations, authorization attempts/state/nonce/PKCE,
guest/OIDC request receipts, login grants, session credential bindings and
principal cleanup outbox rows are excluded. No document database, original,
preview, OCR, output, job, reservation, worker nonce or device lease is opened
or copied by these commands. There is no backup of administrator environment
variables, OIDC client secrets, LLM keys or external IdP state. Keep those in
their separately managed configuration/secret recovery system.

Canonical named-none is not implemented yet. An unsupported non-guest principal
kind makes export fail instead of silently omitting a durable identity. Future
schema changes require an explicit archive compatibility review; these commands
do not downgrade a newer DB or skip a changed migration checksum.

The JSON file contains password verifiers and exact OIDC subjects. Treat it as
secret backup material even though it contains no plaintext password or scans.
It is not encrypted by this command. Keep its parent private and protect any
off-host copy through the operator's secret-aware backup mechanism.

## Command procedure

Run as the application OS user. In the candidate image that is UID/GID 10001.
The state root and archive parent must already exist as private directories
(0700); archive inputs must be private regular files (0600, no symlinks). All
paths must be absolute and clean. Do not loosen existing directory permissions
or point recovery at the old shared scanservjs output volume.

Illustrative paths, prepared by the operator before running the commands:

```sh
SCANNER_STATE_DIR=/private/scanner/state-current \
  scanner state export /private/scanner-backups/identities-v1.json

SCANNER_STATE_DIR=/private/scanner/state-new \
  scanner state restore /private/scanner-backups/identities-v1.json
```

The export filename must not exist. The restore state root must be empty. The
commands only read `SCANNER_STATE_DIR`, with the usual `/var/lib/scanner/state`
default; no HTTP/OIDC/scanner/plugin configuration is required. Their status
messages never print the archive, usernames, hashes or protocol material.

Export can run while the source API is active. It uses an existing database
opened with SQLite `mode=ro`, `query_only` and a read transaction; it does not
invalidate the source's sessions or change account records. Choose a new backup
filename for each export rather than replacing an earlier recovery point.

Restore creates a private staging directory inside the empty destination,
initializes the current checked schema, validates/imports all durable records
in one transaction, checkpoints WAL, closes the DB and syncs the completed main
file. Atomic hard-link publication fails if any API/operator has already created
`state.sqlite3`. A concurrent restore has at most one winner. Existing state is
never merged, cleared, overwritten or made writable to force acceptance. The
destination directory is synced after publication and staging cleanup.

This requires a supported local SQLite filesystem with hard links/fsync. A
restore to an existing mount can use its empty private application directory;
the command does not replace a mount or rename a PVC. NFS/shared independent
writers remain unsupported. If publication succeeds but its final durability
confirmation fails, the command reports that the file was published. Inspect
the destination before any retry; the command never removes that published DB.

Before exposing the recovered deployment, stop/drain the old API/worker using
the deployment procedure and use a separately prepared temporary document root.
Do not relabel old outputs as recovered records. Reconcile physical scanner
activity independently before enabling acquisition. Session absence or a new
state/document volume never proves a scanner or native child has stopped.

Recovery replays account state at the export point. Password changes and account
disables made after that point are absent. Review current disables and rotate
local passwords as needed before exposing the recovered API. The existing
`scanner user set-password USERNAME` command reads the new password on stdin;
do not put it in an argument or an environment variable. OIDC roles are obtained
from new verified claims, not recovered archive fields.

## Validation and failure behavior

The archive is bounded to 32 MiB and 10,000 records per collection. The decoder
rejects duplicate/unknown/case-aliased/non-ASCII field names, trailing JSON,
invalid UTF-8, excessive nesting/token counts, unsupported versions/kinds,
duplicate or missing principal bindings, unnormalized usernames and unsupported
password-hash cost profiles. Referential/kind/uniqueness constraints are checked
again by the fresh database transaction. Failed staged restores publish no DB.
The archive DTO formats as redacted text, and database errors become fixed
recovery errors rather than exposing SQL values or credentials.

Targeted race tests cover source read-only enforcement, protocol canaries,
credential login/reset/disabled preservation, exact issuer/subject mapping,
old-token lookup refusal, malformed archives, SQL rollback, schema mismatch,
private modes, symlinks, missing source, cancellation and concurrent publication.

```sh
go test -race ./internal/staterecovery ./internal/storage
python3 scripts/check-state-recovery.py --receipt /tmp/state-recovery.json
```

The compiled checker creates only synthetic private roots. It logs in through
real local product HTTP, exports while that API is active, restores to a fresh
root, verifies no sessions/protocol rows, rejects the old authenticated cookie
and logs in again with the preserved password. The loopback HTTP fixture manually
transports the normal Secure cookie; it does not enable an insecure cookie mode
or prove a real TLS proxy/mobile/IdP deployment. It checks overwrite refusal,
failure cleanup and no implicit initialization of a missing source, then stops
its owned APIs. An initial checker failure caused by an unclosed Python SQLite
fixture connection was corrected with explicit connection closure and retained
as a failed historical receipt.

The checker also registers a synthetic PNG/page in Alice's existing document
store before export. After restoring authentication state, it starts the API
against that preserved document root, verifies stable IDs for both Alice and
Bob, and requires Alice's complete document projection (including revision,
creation/expiry and page metadata) and original image bytes to remain identical.
The old authenticated cookie is rejected on document and preview routes. A fresh
Bob login sees empty history and receives 404 for Alice's document and preview.

Finally it restarts the API with the recovered authentication state and a
separate empty document root. Its fresh post-recovery session remains valid,
while history is empty, old document/preview requests return 404 and no old
document/artifact/page/job/reservation rows appear. The original fixture root is
retained for hash checks until normal temporary-directory cleanup; no actual
volume is destroyed. This proves the two local state/document-root scenarios,
not full infrastructure loss, scanner stop/fencing, configuration/secret restore,
real IdP behavior or backup placement. The source CI candidate runs this check
without accessing external services or uploading its synthetic recovery archive.
