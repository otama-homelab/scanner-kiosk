# Read-only eSCL idle observation

`internal/scanner/escl.Observer` implements the idle-observation port independently
from SANE capture, document ownership and device-lease storage. Only registered
opaque device IDs resolve endpoints. It makes one GET of `ScannerStatus`, after a
known local-stop timestamp, and returns a timestamp only after a complete accepted
response. It never starts, cancels, downloads or deletes a scan job.

## Interpretation

Protocol reference points are the upstream
[ScannerStatus model](https://github.com/OpenPrinting/go-mfp/blob/master/proto/escl/scannerstatus.go),
[job states](https://github.com/OpenPrinting/go-mfp/blob/master/proto/escl/jobstate.go)
and [SANE eSCL implementation](https://github.com/alexpevzner/sane-airscan/blob/master/airscan-escl.c).
They identify the overall state, optional job history and the PWG/eSCL namespaces.
The following acceptance rules are this application's conservative reconciliation
policy, not a claim of full eSCL conformance or certification:

- Require the correct root namespace, one direct PWG Version and one direct State.
  Only `Idle` can succeed. Prefix spelling is irrelevant.
- When Jobs is present, inspect every JobInfo. Require JobUri and a single
  JobState. Accept only Completed, Canceled or Aborted history; pending, held,
  processing, missing and unknown job states are unavailable.
- Reject nonzero or malformed ImagesToTransfer, duplicate state/jobs elements,
  malformed XML, DTDs, multiple roots and unknown entries inside Jobs.
- Bound response bytes, XML nodes, depth and token count. No external entity
  resolution is available; XML is parsed with the Go standard library.

An absent Jobs list is permitted by this observer when the overall state is Idle.
This does not prove the absence of external control paths, queued firmware work
which the device failed to report, or device-specific asynchronous activity.
Actual Canon behavior during cancellation/network loss remains an acceptance gate.

## Transport and ownership boundary

Endpoints come from deployment registration, never browser requests. HTTP(S)
URLs reject credentials, query/fragment and control/whitespace characters. The
client has an independent transport with no proxy, cookie jar, credential header,
redirect following, connection reuse/replay or TLS bypass. Responses have a
configured deadline (at most 30 seconds), a byte cap (at most 1 MiB), header cap,
identity encoding and no-cache request headers. A nonzero cache Age is rejected.
Only HTTP 200 and completely read XML are considered. Errors disclose no endpoint
or response body and return no timestamp. Context cancellation propagates during
headers and body reads.

The observation cannot fence the old scanservjs writer or another external
controller. Local-process stop, exclusive writer handoff, registered-device
identity, fresh HTTP observation and matching persisted boot/lease epoch are
separate requirements. The caller must retain the device lease during the GET;
only conditional publication or confirmed cleanup releases it. A generic health
check or cached timestamp cannot substitute for this call.

## Evidence and remaining work

Local HTTP/TLS fixtures exercise GET-only requests, repeated fresh reads,
malformed/active states, timeouts/cancellation, redirection refusal, untrusted TLS,
namespace handling, oversized bodies and configuration rejection. A scan-executor
integration uses the actual HTTP observer with real SQLite admission/claim: Idle
permits publication, Processing retains the bitmap, reservation and held lease.
These are fixture responses, not real physical-idle acceptance.

Production capability resolution, the combined configured SANE/eSCL driver,
worker-command scan dispatch and durable recovery remain pending. No network
request was sent to the Canon for these tests, and existing scanservjs was not
changed. `scripts/check-scanner.py` records this scope alongside the official
SANE test-backend acquisition evidence.
