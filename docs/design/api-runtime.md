# HTTP runtime

`scanner serve` composes the configured SQLite stores, server sessions and optional OIDC flow. [Document history and detail](document-http.md) are connected; page editing and [source previews/current-export downloads](image-delivery.md) are also connected. [UI-enabled builds](product-web.md) serve the partial Astro frontend; backend-only builds return 404 at `/`. [Render admission](render-http.md) is connected; scan admission, OCR capability resolution, whole-document deletion and thumbnail routes remain pending.

[Job status and cancellation intent](job-http.md) are served at `/api/jobs/{job_id}` and `/api/jobs/{job_id}/cancel`. A 202 cancellation response records intent and does not certify worker or hardware termination.

Build with `go build -o /tmp/scanner ./cmd/scanner`. Create separate private directories first, then run behind a trusted HTTPS reverse proxy preserving the configured public Host:

```sh
install -d -m 0700 /tmp/scanner-state /tmp/scanner-documents
SCANNER_PUBLIC_URL=https://scanner.example.test \
SCANNER_AUTH_MODE=guest \
SCANNER_STATE_DIR=/tmp/scanner-state \
SCANNER_DOCUMENT_DIR=/tmp/scanner-documents \
SCANNER_LISTEN_ADDR=127.0.0.1:8080 /tmp/scanner serve
```

Secure host-only cookies require HTTPS at the browser. The backend does not trust forwarded headers or relax cookie security for local HTTP. The listener itself serves HTTP; restrict its exposure to the trusted proxy. Set `SCANNER_AUTHENTICATE_TYPE=oidc` and the OIDC environment variables in `.env.example` for OIDC. Discovery must succeed before the listener starts; failure exits without guest fallback. The callback is the configured public URL plus `/auth/oidc/callback`. Worker configuration does not read OIDC secrets or API-only limits.

`GET /health/live` reports process availability. `GET /health/ready` checks both SQLite connections and shutdown state; it does not certify scanner readiness, cleanup health, or complete product functionality. Health paths permit probe Hosts and bypass user traffic admission. Session and authentication paths enforce their public Host and mutation Origin checks.

API/auth requests share a per-process token bucket; all non-health traffic shares a concurrency bound. Static assets do not consume the API rate budget. Defaults: `SCANNER_API_MAX_INFLIGHT=32`, `SCANNER_API_REQUESTS_PER_MINUTE=600`, `SCANNER_API_BURST=32`. Rate exhaustion returns 429; concurrency exhaustion returns 503; both include Retry-After. Limits reset on restart and do not replace persistent metadata quotas or per-principal job admission. Request bodies are bounded to 64 KiB, headers to the Go server's 16 KiB limit, with read/write/idle deadlines and a 30-second handler context for ordinary API requests. GET/HEAD preview/download paths use SCANNER_TRANSFER_TIMEOUT_SECONDS (default 300, range 1–900), with the stream deadline also capped by document expiry. SIGTERM/SIGINT stop admission through listener shutdown and allow up to ten seconds to drain before closing connections and stores.

Reproduce compiled command verification:

```sh
python3 scripts/check-api.py --receipt /tmp/scanner-api-check.json
```

The checker uses private temporary databases, actual process restart, guest lifecycle, local TLS OIDC discovery/start, unknown-cookie reset and unavailable-provider rejection. Cookies are manually relayed over a loopback HTTP backend to model a trusted TLS proxy. This does not prove real browser/proxy/IdP acceptance, compiled token callback completion, scanning, or deployment. Protocol callback coverage lives in the separate session/OIDC suite. An explicit logout can clear an unknown cookie after database recovery without creating an identity; ordinary session reads never overwrite a stale supplied cookie with a new session.


Local password login is available with SCANNER_AUTHENTICATE_TYPE=simple after provisioning a user through the stdin-only user command. It skips OIDC configuration/provider setup. GET /api/session returns authenticate_type; the old auth_mode JSON property is removed. SCANNER_AUTH_MODE is accepted only when the canonical key is absent, with guest retained as a legacy isolated mode. Both keys together fail. Named none remains pending and never falls back to guest. See [local authentication](simple-authentication.md) for rate bounds, shared session grants, reset invalidation and the browser proof.
