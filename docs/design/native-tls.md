# Native scanner HTTPS boundary

## Current unpublished candidate

The b85a7dae onefile and fef2bd85 distroless image contain the local verifying,
HTTPS-downgrade-refusing sane-airscan fork based on 0.99.38. The actual backend
hash is 480e1bbd…; the older dbef/48692705 candidate verified peers but still
followed HTTPS redirects to plaintext. That earlier failure is retained in the
parent plan's redirect-policy evidence. Both remain unpublished artifacts.

Onefile defaults native `SSL_CERT_FILE` to its integrity-checked bundled public
CA file. An explicit PEM override is preserved. The fork verifies each current
HTTPS peer before sending HTTP. After a query uses HTTPS, its final redirect
URI must remain HTTPS. The check runs after the existing eSCL normalization hook
and before changing URI ownership/submitting the next connection. HTTP devices,
HTTP-to-HTTPS upgrades, relative HTTPS and authenticated cross-authority HTTPS
remain supported. This adds no general same-origin/SSRF policy.

The new image's actual cached scanimage and parameter-helper wrappers passed
40 loopback cases: 26 direct peer/trust cases and 14 redirect cases. Explicit
trusted IPv4/DNS+SNI/IPv6/scopedIPv6 succeeded; privateCA under the bundled default,
unknownCA, wrong-name and expired certificates were rejected. Missing/empty native
CA settings rejected HTTPS, with paired HTTP controls working. Empty worker
configuration remains separately rejected by the application. Redirect cases
covered the plaintext downgrade, HTTP compatibility, upgrades, valid/wrong-peer
HTTPS targets, relative HTTPS, and an HTTP-to-HTTPS-to-HTTP chain.

Fixtures generated private CA/leaf keys in memory, served only redirects or
GET 503, counted other methods and required ordinary positive nonzero native
exits (no signal/timeout). The backend was witnessed in child process maps at
ClientHello/GET; image/onefile/backend/CA hashes matched the selected candidate.
The original runtime layers were preserved; only a static CGO0 Go fixture was
added, with no Python, extra C libraries or host mounts. The container ran with
network none, read-only root, UID10001, 2 CPUs, 768 MiB/swap disabled, 128 PIDs and 512 MiB
private exec tmpfs; exit 0/OOM false. No scanner, acquisition or private document
was involved. The default ENTRYPOINT/version fresh-cache smoke was checked
separately; the TLS fixture supervises native wrappers through its own entrypoint.

The new library, source/patch/build metadata and three changed native entries are
pinned in the product. APK file bytes and the retained Go app/frontend did not
change. Source/receipt history elsewhere in the notice set remains explicitly
historical. The earlier system-root-shadowing case was not repeated in this
container run. Real certificate provisioning, full product scan job/storage,
physical fencing/readiness, real providers/mobile and complete source/license/
security/release/deployment acceptance remain separate gates.

Evidence: `tasks/43/onefile-redirect-guard-20261104/current-evidence.json` in the
parent plan. The fork's SDK policy/peer suites and upstream URI/zeroconf tests are
separate component evidence, not additional cases counted in the 40 above.

## Historical unauthenticated-backend diagnostic

The following diagnostic describes the earlier unmodified backend and source
observation on 2026-11-03; its failures and receipts remain historical evidence.

The experimental onefile candidate `c284a3a7…` and distroless image `57ae9fe2…`
include sane-airscan 0.99.36. A bounded loopback diagnostic observed an HTTPS GET
from the bundled `scanimage` wrapper despite an untrusted self-signed certificate
whose DNS name did not match the endpoint IP. Encryption works in this fixture;
peer authentication is not enforced by this native transport. Native TLS
acceptance remains open.

The diagnostic inherited the product image layers, used its unchanged executable,
and ran with network none, read-only root, UID10001, one CPU, 768MiB, 128 PIDs,
and bounded private tmpfs directories. A synthetic TLS server returned HTTP503
to GET and rejected every other method. Exactly one native GET and zero non-GET
requests were observed; no scan was started. The options query failed because the
fixture deliberately supplied no scanner capabilities. That failure does not undo
the fact that a request reached the unauthenticated peer.

Three Go standard-library controls checked the fixture independently: a trusted
certificate with an explicitly matching expected DNS name reached the server;
an unknown CA and a trusted certificate with the wrong endpoint name were each
rejected. These controls are not an end-to-end test of the product's OIDC or LLM
configuration.

Device enumeration alone did not make an HTTPS request. The diagnostic therefore
used the enumerated device name for `scanimage --all-options`, which opens the
device without acquiring a page. Earlier plugin `dlopen`/initialization checks
must not be interpreted as TLS or device-readiness proofs.

## Separate transports

The Go eSCL observer has its own certificate-verifying HTTP transport. Its
successful status observation cannot authenticate a later, separate native
connection. Likewise, browser HTTPS termination and the Go OIDC/LLM clients do
not change sane-airscan's certificate policy.

Source inspection found that `airscan-http.c` allocates GnuTLS credentials and
performs a handshake without loading trust roots or configuring peer/name
verification. The observed upstream 0.99.38 tag points to commit
`9da18d88c88f542671b24fc0433dd7d69dcb0132`; its transport file also has no trust or
verification calls. This is source evidence only: 0.99.38 was not built or tested
here and must not be advertised as a verified remedy. The GitHub latest-release
API returned 404 while the tags API returned versions; this does not mean the
project or tag is unavailable.

No certificate checks were disabled in the Go clients, no registered endpoint
was changed, and no upstream patch/fork or deployment was made. If native code
changes become necessary, the workspace requires managing them in an upstream
fork. A verified remedy or an explicitly accepted network-trust boundary is
still needed for native HTTPS distribution acceptance. Existing plain-HTTP
scanner registration is not evidence of authenticated transport, and is not a
secure fallback for HTTPS.

The source extracts, runtime report, controls, tool observations and cleanup
receipts are indexed by the parent plan at
`tasks/43/standalone-native-tls-20261103/current-evidence.json`.
