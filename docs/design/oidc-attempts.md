# One-time OIDC authorization attempts

`internal/oidcattempt` connects the existing `auth_attempts` state table to an
already-persisted SCS browser session. It does not implement an OIDC HTTP endpoint,
exchange a code, verify an ID token or grant an authenticated principal.

`Begin` requires the exact active, non-revoked browser Cookie session. It generates
fresh state and nonce, and uses the pinned official
[oauth2 v0.37.0 PKCE helpers](https://github.com/golang/oauth2/blob/v0.37.0/pkce.go)
for the verifier and S256 challenge. State and browser token are independently
hashed for persistence. Nonce/verifier remain short-lived private protocol state,
not application response data or identity claims. Implicit formatting and JSON of
the returned Material redact its fields; protocol code must select values
explicitly.

The configured attempt lifetime is at most ten minutes. A per-browser pending
attempt cap is checked inside the same IMMEDIATE transaction as insertion. Used
and expired rows do not occupy that pending cap. This is not a global HTTP rate
limit or auth-state garbage collector; those remain required before release.

`Consume` checks that the same browser session is still active and unrevoked,
then conditionally updates exactly one matching, unexpired, unused state. The
nonce/verifier are returned only after commit. Wrong state/browser, expiry,
creation in the future, replay or session revocation returns no protocol material.
Two independent database connections cannot consume the same attempt twice. A
commit acknowledgment failure also returns no material, even if the consume
actually committed; token exchange must not proceed or blindly retry that state.

All repository operations have bounded contexts and return fixed error categories
without raw tokens, SQL errors or protocol response bodies. Existing database
triggers keep attempt identity/material immutable and prevent consumed state
from being rewritten. No database migration was needed for this repository.

## Evidence and remaining integration

Tests use actual SCS bootstrap sessions and the product SQLite migrations. They
cover two active browsers, random wrong state, expiry and clock rollback, pending
limits, revoked/missing/expired sessions, secret redaction, concurrent consumption
from two connections and an injected lost commit acknowledgment.
`scripts/check-session.py` includes these tests without skipped cases.

The callback is a deliberate cross-site top-level GET from the IdP. Its fixed
route must use the browser-bound one-time state checks instead of the guest
handler's generic cross-site metadata rejection; do not broaden that exception
to guest/session mutations. SameSite=Lax is intended for this query callback,
not an unimplemented form_post flow.

The HTTP adapter must still persist/restore the login-start idempotency result,
construct a fixed-provider authorization URL, consume state before exchanging the
code, use the PKCE verifier and explicitly compare the verified ID-token nonce.
The provider must validate signature, issuer, audience and expiry. After network
exchange, the adapter must recheck session/principal state before granting or
rotating authority: consumption is not proof that a browser stayed active during
the exchange. Exact issuer/sub mapping, callback error handling, logout and live
IdP/JWKS behavior remain pending. There is no guest fallback.

OIDC/session expiry is independent of document retention. Restore/export and
cleanup must exclude or invalidate attempts as already required by the state
storage design. These component tests do not prove product OIDC login or IdP
acceptance. No real IdP or production service was contacted.

The [provider/identity components](oidc-provider.md) now supply code exchange,
ID-token/nonce verification and issuer-sub mapping. Their browser HTTP callback
and session grant are still pending.

The [browser OIDC lifecycle](oidc-browser-flow.md) now joins these components to
HTTP start/callback/session grant/logout with request idempotency and grant
revocation fencing. Serving-runtime/env wiring and real IdP/browser acceptance
remain pending; earlier pending-integration paragraphs describe component history.


## Storage port

`Store` owns random state/nonce/PKCE material, request-state derivation and
validation. `Records.Begin` and `Records.Consume` own atomic active-browser and
revocation checks, fixed request tombstones/configuration, pending limits and
one-time consumption. Commands contain hashed browser/request/state bindings;
raw browser tokens never cross the port. `StoredMaterial` contains secret nonce
and verifier fields and deliberately redacts implicit formatting/JSON. Backends
must not log explicit fields. Only a confirmed commit may return a record.

`SQLiteRecords` retains the existing transactions; API composition wires it via
`NewWithRecords`. The service rejects malformed, expired or mismatched returned
records and suppresses material on errors or cancellation after backend return.
A memory fixture proves stable replay, concurrent single consume, corruption
rejection and no material on uncertain commits or post-commit expiry/cancellation.
This fixture is a port test, not a production alternative database implementation.
