# Threat model and required verification

Status: design and partial component evidence; not a completed security audit.

Assets are scanned images/PDFs/OCR text, owner mappings, server sessions, OIDC protocol state, plugin credentials, device control, and availability of storage/worker resources. A LAN/VPN location is not an authorization decision. An authenticated user may deliberately reference another user’s IDs or submit malformed values. Scanner bytes, PDF/image libraries, model output and remote responses remain untrusted inputs. Administrators and deliberately installed plugin code are trusted; this design is not a sandbox for hostile administrator plugins.

| Boundary / failure | Control | Required evidence |
|---|---|---|
| Alice references Bob’s document/page/job/file/range | Query-level owner check on every route; composite DB FKs; indistinguishable missing/not-owned result | Full two-user HTTP/file/preview/thumbnail/job/Range tests; FK tests alone are insufficient |
| Forged/replayed OIDC result | Fixed issuer/endpoints, code + PKCE, one-time browser-bound state, verified signature/issuer/audience/expiry and nonce, nonempty sub | Existing library spike plus complete callback tests, discovery/JWKS rotation and real IdP/browser acceptance |
| Cookie mutation from another site | Secure/HttpOnly/host-only/Lax sessions; fixed public Origin + standard cross-origin protection; reject missing Origin on mutations | Positive/negative Origin, browser metadata, proxy and guest tests |
| Session fixation or logout race | Renew on login; check principal state; destroy on logout; serialize/reconcile concurrent session updates | Concurrent requests, expired cookie, back/forward cache and two-tab logout tests |
| Document exceeds TTL or deletion races a worker | Creation-based immutable expiry, no-store files, deadline-aware streams, publication CAS, retained cleanup work | Fake-clock boundaries, active range/download abort, late worker result, restart/orphan cleanup |
| Another scanner writer or stale worker | Single replica, Recreate/drain, DB lease + epoch, actual process/device reconciliation | Two-user start race, worker crash, node/upgrade split-brain checks; no heartbeat-only release |
| Path escape, symlink or command injection | Configured storage root, scoped opaque artifact IDs, private permissions, safe file access, explicit argv, no shell | Traversal/symlink/race fixtures and subprocess tests; DB path checks alone are insufficient |
| Memory/disk/output exhaustion | Capability/pixel/file limits, quota reservations, process deadlines, bounded writes, container memory/CPU, concurrency one initially | 300/600dpi, decompression bomb, disk-full, full quota, huge/malformed model response, stalled process |
| Provider URL / redirect / DNS used as SSRF | No normal-user destination settings; admin-configured endpoints, allowed hosts/protocols, validated resolved addresses and redirects | Loopback/internal/metadata targets, redirect and DNS-change fixtures; explicit LAN scanner exception is separate from LLM traffic |
| Document text instructs LLM to call tools | OCR-only requests, no agent tools, output schema/geometry validation, no execution of returned content | Prompt-injection documents, tool-call response rejection and malformed-coordinate fixtures |
| Secrets in logs, capabilities or child environments | Allowlisted process env, stable error codes, log/metric redaction, metadata-only capabilities | Canary secret tests in errors/stdout/stderr, process env and API responses |
| Expired content retained by infrastructure | Dedicated temporary volume outside all snapshot/replica/offsite policies; originals and intermediates included | Live storage-policy evidence and restore tests before production |

Paperless integration, public links, arbitrary user uploads/URLs, camera capture and printing are not initial interfaces. Later integrations need their own boundary review. Existing scanservjs output is not imported into a public/shared owner.

Raw state-store recovery must invalidate sessions and authorization attempts. Normal process restart preserves valid sessions. Log records contain request/job/internal principal IDs, stage, result and timing only; no images, extracted text, tokens, original filenames or full provider request/response bodies.
