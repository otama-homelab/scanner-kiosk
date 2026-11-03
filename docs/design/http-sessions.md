# HTTP session and guest lifecycle components

`internal/websession.Handler` implements `GET /api/session`, `POST /auth/guest`
and `POST /auth/guest/end` against the existing state database. This is an HTTP
component, not the executable web server or integrated Astro application. OIDC
start/callback/logout, document routes and deployment are still pending.

## Session management

Use the unmodified [SCS v2.9.0](https://github.com/alexedwards/scs/tree/v2.9.0)
library (MIT) for session tokens, codec, renewal, lifetime, idle timeout and Cookie
construction. `HashTokenInStore` is always enabled. Only the token hash enters the
SQLite adapter. The host-only `__Host-scanner` Cookie uses Secure, HttpOnly,
SameSite=Lax and Path=/; its origin is configured, never inferred from forwarded
headers. Normal database reopen preserves valid sessions.

The small application SQLite store implements SCS's context-aware interface.
Session deletion and a revocation tombstone commit atomically. Database triggers
reject subsequent INSERT/UPDATE of a revoked hash, including from a second API
connection. A loaded request racing logout cannot recreate its old session by
committing stale data. Tombstones retain metadata for at least the configured
maximum session lifetime plus five minutes. Their bounded cleanup remains a
release requirement; no collector is implemented by this component.

Auth responses commit session changes before any success bytes or Cookie are
sent. A failed commit produces only a generic error response. No raw database
error, token, principal ID or protocol state appears in the session projection.
The projection matches the four fields of the OpenAPI Session schema.

## Explicit guest lifecycle

Safe bootstrap can establish an anonymous browser binding but never creates a
guest principal or starts a scan. A supplied stale/rotated Cookie does not cause
an anonymous replacement Cookie: a late response must not overwrite a newer
browser session. Disabled principals return an unauthenticated projection. An
explicit guest-end operation can clear an expired/ended Cookie without creating
a principal or claiming immediate content deletion.

Guest creation requires that browser binding, a valid Idempotency-Key and an
empty body. A transaction binds one independent random guest principal to a
hashed browser binding and request key. A concurrent/repeated start with the same
binding/key returns that principal; changed intent conflicts. An expired or ended
result is never recreated. Session renewal precedes granting guest authority.
A lost response after renewal cannot replay the revoked pre-authentication token
to mint another principal; a deliberate end/reset and new bootstrap is required.

Guest end disables the principal and persists `principal_cleanup_requests` in one
transaction, then destroys the session. It returns 202 for accepted intent. All
other sessions for that guest lose authorization through the shared principal
state guard; already-existing document records do not bypass that guard. Workers
must still stop owned processes and reconcile physical idle before lease release.
The cleanup worker and actual file removal remain pending; 202 is not completion.

## Request boundary

The handler checks the configured public Host, exact Origin on mutations and
standard Go CrossOriginProtection. Missing, foreign, scheme-mismatched, duplicate
Origin or cross-site browser metadata is denied before session loading. Forwarded
host/origin headers cannot grant trust. Session responses and errors are no-store
JSON; ambiguous duplicate session Cookies are rejected. These routes accept no
user payload or arbitrary redirect destination.

Deployment chooses guest or OIDC mode. OIDC mode rejects guest creation/end and
never authenticates a stored guest session. This does not implement OIDC login:
its verifier/state/nonce/PKCE/callback and live-IdP acceptance remain separate.

## Evidence and remaining work

`scripts/check-session.py` records real SCS/SQLite component tests and an actual
local TLS HTTP client/Cookie-jar flow. Other cases cover independent browsers,
creation replay/concurrency, database reopen, old-token rejection, logout races,
commit error responses, Origin/mode checks, Cookie attributes, stale bootstrap and
ended-guest access rejection by the existing document repository.

State migration 0002 adds session revocations and immutable guest-start receipts.
It does not change existing principal/session identities or touch document files.
The test database is isolated; no real service or IdP was modified.

Still required: serving-runtime timeouts/rate and anonymous-session limits,
authentication middleware for document routes, OIDC integration, invalid/expired
session UI recovery, metadata/guest cleanup, integrated browser/mobile acceptance,
recovery/export procedures and production deployment. Configuration recovery must
invalidate live sessions/attempts and include these new short-lived auth tables
in its exclusion/cleanup policy. Normal process restart is different and retains
sessions. No product-acceptance claim follows from these component tests alone.

The [OIDC attempt repository](oidc-attempts.md) now supplies browser-bound,
one-time state/nonce/PKCE persistence. OIDC HTTP/token verification and identity
grant remain pending.

The [browser OIDC lifecycle](oidc-browser-flow.md) now joins these components to
HTTP start/callback/session grant/logout with request idempotency and grant
revocation fencing. Serving-runtime/env wiring and real IdP/browser acceptance
remain pending; earlier pending-integration paragraphs describe component history.


## Replaceable session records

`websession.NewWithRecords` now accepts the backend-neutral `Records` command port. Handler, authorization checks, guest identity orchestration, OIDC logout and the SCS bridge no longer issue SQL or inspect a concrete store type. `SQLiteRecords` in records_sqlite.go contains the explicit SQL and borrows the state DB; the API composition root constructs it and injects it explicitly. `New(db, options)` remains a convenience constructor for existing callers, not a requirement imposed on alternate backends. No ORM was introduced.

The port exposes atomic session commit, login rotation, login grant commit, revocation including the linked successor, guest creation/replay, guest disable plus cleanup intent, principal lookup and session/principal authorization. It does not expose database/sql, transaction handles or per-row CRUD to HTTP handlers. SCS token hashing, bounded store calls, validation, cookies and request-local login grant state remain in the session layer. The adapter receives hash values and immutable grant data; it cannot set the in-request rotated flag. That flag is set only after the rotation command succeeds. The SQLite implementation retains all existing transactions and database constraints/triggers against stale resurrection and callback/logout/disable races.

Tests inject a non-SQL memory fixture through the actual HTTP/SCS handler and exercise bootstrap, guest identity creation, token renewal, protected access, stream authority, end and rejection of a stale session commit. This fixture deliberately does not implement OIDC persistence and is not a production backend. A separate wrapped SQLite Records implementation exercises OIDC rotation and logout through an old source cookie, proving the handler no longer requires concrete SQLite/SCS types. Existing local TLS OIDC browser protocol/concurrency tests remain authoritative for those contracts. Failed record commits still issue no login/session cookie.

This is one step in the requested DB/authentication abstraction. Simple authentication and the configuration/API rename to authenticate_type are now connected; named none, OIDC attempt/identity repository boundaries, document/job repository boundaries, shared backend contract tests and another production database implementation remain incomplete. Guest remains an explicit legacy mode and is not reinterpreted as named none. See [simple authentication](simple-authentication.md) for the shared grant/version fencing and browser proof.
