# OIDC exchange, verification and identity components

`internal/oidcprovider` uses unmodified coreos/go-oidc v3.21.0 and oauth2 v0.37.0.
`internal/oidcidentity` maps the resulting verified issuer/subject to a durable
principal. They are components; OIDC HTTP start/callback/logout, Cookie grant and
the product serve command are not yet connected.

## Fixed-provider protocol boundary

The provider is constructed from deployment configuration: issuer, client ID,
client auth style/secret, scopes, callback URL, admin groups and resource limits.
Issuer, authorization/token/JWKS and callback URLs must be HTTPS without userinfo,
query or fragment. Discovery must match the configured issuer. Returned endpoints
are validated before use; they are never selected by browser request values.

The independent HTTP client has no proxy, Cookie jar, redirect following,
connection reuse/replay or TLS bypass. Custom trust roots are cloned when supplied;
otherwise system roots apply. Its transport permits only the discovered fixed
GET discovery/JWKS and POST token URLs, caps headers and fully buffers at most the
configured response-byte limit (maximum 1 MiB). Every network phase has a timeout
(maximum 30 seconds). Content encoding must be identity. Public authorization
URLs are constructed by the official OAuth library with state, nonce and S256;
client secrets never enter that URL.

Client authentication is explicitly basic, post or none. OAuth automatic auth-style
negotiation is not used, so an exchange failure does not retry an authorization
code with another style. For a public client, a leftover configured secret is
ignored and never transmitted. Access, refresh and ID tokens are not returned or
persisted by the adapter. Errors expose neither protocol bodies nor library
claim-validation details. Implicit provider/verified-identity formatting is redacted.

## Verification

Only the `Material` returned by a confirmed one-time attempt consume is accepted
for exchange. Begin material can construct an authorization URL but cannot exchange
a code. The official verifier handles signatures, configured issuer, client
audience and expiry. Its documented [nonce responsibility](https://github.com/coreos/go-oidc/blob/v3.21.0/oidc/verify.go)
remains with the caller: this adapter compares the exact expected nonce explicitly.
No `nonce_supported` response field can bypass that comparison.

Additional application policy checks exact issuer spelling, nonempty ASCII
subject (at most 255 characters), non-future issuance beyond one minute, expiry
still in the future, authorized-party consistency and the access-token hash when
present. Multiple audiences require the configured client as azp. Only a
configured allowlist of advertised asymmetric signing algorithms is accepted;
none/HMAC algorithms cannot be enabled through discovery.

Admin membership is an exact comparison against the signed groups string array.
Missing, malformed, oversized or nonmatching groups confer no admin authority;
they do not reject an otherwise valid ordinary identity. The adapter does not use
email or fetch userinfo/distributed group URLs. The ordinary library JWKS cache
is reused. Its shared background key fetch may outlive one cancelled caller, but
the independent client still bounds it; instant cancellation of that shared fetch
is not claimed.

## Identity mapping after exchange

`oidcidentity.Resolve` accepts the unforgeable-by-normal-construction Verified
result, rechecks the exact browser session is active/unrevoked inside the state
transaction, and looks up the exact issuer/sub tuple. It creates a random OIDC
principal only when that tuple is absent. Email never joins accounts; changing
issuer creates a distinct identity. Disabled principals cannot reactivate or
remap themselves. Concurrent connections resolve the same tuple to one principal.
Commit uncertainty returns no principal authority.

This repository does not grant a Cookie. The HTTP adapter must still renew the
session, persist its principal/admin projection and handle commit/lifecycle races.
Subsequent document operations must keep using the enabled-principal state guard.

## Evidence and remaining work

Local TLS IdP fixtures perform real discovery, client authentication, PKCE code
exchange, RSA signing, HTTP JWKS fetching and token verification. Tests cover all
three client auth styles, unconsumed material, invalid signature/issuer/audience/
expiry/issuance/nonce/azp/subject/access hash, malformed groups, redirect/oversize
responses, unsafe discovery and untrusted TLS. Cancellation during token exchange
returns no identity and never retries. Identity tests cover stable remapping,
different issuers with the same subject/email, disabled principals, revocation
after exchange and two-connection concurrent mapping.

These are fixture-issued tokens, not real IdP acceptance. Still pending: HTTP
start idempotency, cross-site GET callback/state integration, session grant/logout,
operator configuration wiring, real discovery/JWKS rotation and outage behavior,
rate/metadata cleanup limits, frontend/browser/mobile acceptance and deployment.
A fixed callback exception must not weaken the guest/session Origin boundary.
No real IdP, Canon or existing scanservjs service was changed.

The [browser OIDC lifecycle](oidc-browser-flow.md) now joins these components to
HTTP start/callback/session grant/logout with request idempotency and grant
revocation fencing. Serving-runtime/env wiring and real IdP/browser acceptance
remain pending; earlier pending-integration paragraphs describe component history.


## Identity storage port

`oidcidentity.Store` validates verified claims/browser format, hashes the browser
binding and generates a proposed principal ID. `Records.Resolve` atomically
checks the active, nonrevoked browser and enabled issuer/subject mapping; it
returns no ID on uncertain commit. `SQLiteRecords` implements the existing SQL
transaction. Admin authority comes only from the verified provider result. No
identity repository issues cookies or grants. `oidcflow` consumes attempt,
provider and identity interfaces; runtime composition wires explicit adapters.

A non-SQL identity fixture receives claims verified through the local TLS IdP
and checks stable mapping, hashed binding, redaction, malformed ID rejection and
no principal on uncertainty/cancellation. Existing SQLite/TLS/browser lifecycle
tests continue to cover revocation, disabled identities and concurrent mapping.
These ports do not implement a second production backend or isolate the entire
document/job database. Named `none` mode remains a separate pending contract.
