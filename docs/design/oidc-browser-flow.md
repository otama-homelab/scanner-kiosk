# Browser OIDC lifecycle

`websession.Handler` now connects the OIDC component ports to:

- `POST /auth/oidc/start`: exact public Origin, empty body and Idempotency-Key;
  only an existing anonymous browser session can begin login. Return 200 JSON
  with authorization_url; frontend navigation is a separate browser action.
- `GET /auth/oidc/callback`: fixed query callback, one-time state and browser
  binding, code/PKCE exchange, token verification, identity mapping and session
  renewal; successful login redirects only to `/`.
- `POST /auth/logout`: revoke this OIDC-mode browser or pending login and its
  single login successor; retain the owner, jobs and document expiry.

This is a complete component HTTP path against fixture IdPs. The executable web
server, operator env wiring, frontend and real IdP acceptance are still pending.

## Protocol and HTTP separation

`authport.OIDC` is the HTTP-facing interface. `oidcflow.Flow` composes the attempt,
provider and identity repositories without importing HTTP/session handling. A
missing/unavailable provider does not enable guest mode. The fixed callback GET
alone bypasses generic cross-site metadata rejection; configured Host and
browser-bound state remain required. Guest/session mutations retain exact-Origin
and standard Go cross-origin protection.

The callback rejects ambiguous state/code/error/issuer parameters, malformed or
oversized query strings, code plus error, and a response issuer different from
the configured provider. Standard provider errors consume the attempt but never
exchange a token; only a fixed `/?auth=cancelled` destination is emitted. Provider
error text and error URI are ignored. Codes, state, tokens and claims never enter
application error bodies or redirect destinations. Auth/session responses use
no-store and no-referrer. Non-query form_post callbacks are not supported.

The start response is JSON rather than a redirect because the SPA sends a custom
Idempotency-Key header with same-origin fetch. Fetch's manual redirects expose an
[opaque redirect response](https://fetch.spec.whatwg.org/#concept-filtered-response-opaque-redirect),
so the frontend reads authorization_url then initiates top-level navigation.
The callback remains a normal browser GET/303 flow. Integrated browser acceptance
is still pending.

## Start idempotency without raw persisted state

State migration 0003 adds immutable `oidc_start_requests` receipts, keyed by hashes
of the browser session and operation key. Each receipt binds the provider/client/
callback/scopes authorization configuration and the original attempt hash.

For this path, state is a domain-separated HMAC-SHA256 PRF output keyed by the
fresh PKCE verifier, including browser hash, request-key hash and configuration
hash. The random verifier remains private protocol state; only the state hash is
persisted. A retry can deterministically reconstruct the same state, nonce and
PKCE challenge without storing raw state or changing the authorization URL.
The ordinary non-idempotent Begin API remains available to component tests.

Repeated pending requests return their original material. Changed configuration
conflicts; used, expired or removed protocol material returns gone and is not
recreated. A restart with the same configuration preserves the pending request.
Receipts are independent of secret-material cleanup: removing an expired attempt
can leave a content-free receipt that still prevents re-execution. The eventual
collector must retain receipts for the API retention policy and active-session
bounds; cleanup and global rate limits remain release work.

## Session grant and logout ordering

A verified token is not itself a browser session. After identity mapping, callback
renewal compares and deletes the exact still-active source session inside a state
transaction and records a fresh private grant nonce. Only one callback can rotate
that source. The final SCS commit checks the matching uncancelled grant and an
enabled OIDC principal, then inserts the new session and successor hash atomically.
It issues no success Cookie or Location before commit succeeds.

A normal revocation cancels a pending grant. If the login session already
committed, the old source Cookie still revokes that successor too. Thus logout
between source rotation and session commit cancels the grant, while logout just
after commit invalidates a potentially late Cookie. A late response may carry an
already-revoked token but cannot recover authority. Existing loaded requests also
remain fenced by the earlier session-revocation triggers.

Authenticated sessions cannot start a new OIDC login in this interface; account
switching requires logout first. Successor lineage therefore has one hop. Future
reauthentication/step-up support must extend that model explicitly. Pending and
completed grant metadata needs bounded cleanup before release. OIDC logout does
not disable the principal or cancel its owned jobs; guest end remains a separate
operation with disabled-owner cleanup semantics.

## Evidence and remaining work

Local TLS tests perform actual browser-session bootstrap, stable start retries,
IdP authorization redirects, PKCE token exchange, signed ID tokens, JWKS fetch,
Cookie renewal, session projection and logout. IdP navigation is performed with
a separate local client because both TLS fixtures use loopback hostnames/ports;
this is not real browser/domain Cookie acceptance. Other tests cover a second
browser, replay, provider denial, invalid callback input and expired start reuse.
Forced session-store interleavings cover logout before commit, revocation of a
committed successor, disabled principal at commit and concurrent grants.

No real IdP, scanner or existing scanservjs service was changed. Still required:
serving runtime and env composition, protected document routes, integrated Astro
navigation/recovery, real IdP/browser/mobile tests, state/rate/cleanup resource
limits, live proxy/Cookie behavior, restore/export policy, deployment and cutover.
