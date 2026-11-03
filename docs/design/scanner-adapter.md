# Scanner adapter and SANE boundary

Status: a product capture port and Linux SANE adapter exist. The official SANE
test backend verifies acquisition and local lifecycle. Worker scan dispatch,
Canon capture and physical cancellation acceptance remain pending.

## Initial backend

Use unmodified `scanimage` + `sane-airscan` for network eSCL devices. Keep scanner
control independent from OCR/PDF processing. The administrator registers opaque
scanner IDs/endpoints in `SCANNER_SCANNERS_JSON`; user requests select those IDs,
never a device URI, arbitrary endpoint, backend option string or command line.

The upstream [manual](https://github.com/alexpevzner/sane-airscan/blob/master/sane-airscan.5.md)
describes manual devices and discovery control. Its current environment-variable
interface differs from the tested Ubuntu Noble 0.99.29 package: the packaged
manual does not document `SANE_AIRSCAN_DEVICE`, and its shared library contains no
such variable string. Do not base initial packaging on that latest-only feature.
Generate a private, process-specific `dll.conf` (airscan only) and `airscan.conf`
from validated deployment environment settings, with discovery disabled and debug
tracing off. These are generated adapter inputs, never a second editable
configuration source. The image's standard SANE configuration must also disable
unwanted backend discovery, because SANE_CONFIG_DIR can fall back to standard
paths. Verify this against the final image. Device names used in generated files
must be internal safe IDs, not free-form display names. Reject config syntax
characters/newlines in generated values and use a tested serializer.

Probe supported modes, source, area and DPI without initiating a scan. The actual
SANE backend's option constraints are authoritative for capture; eSCL advertisement
alone is insufficient. Canon's previously fetched advertisement is Platen-only,
RGB24/Grayscale8, 75–1200dpi with 300dpi default. Initially expose only measured,
resource-approved resolutions; 1200dpi remains unavailable. Probe failure must not
cause fallback to a different device or start a diagnostic scan.

## Capture command

The worker resolves a validated immutable request into explicit source, mode,
resolution and geometry arguments and captures exactly one flatbed page. No batch
scan, interactive prompt, shell interpolation, inherited authentication secrets
or automatic retry. One API command owns one persisted job/lease epoch. Sequential
pages require another explicit user action after exchanging the original.

Capture into an unpublished file under a job-private directory, with a deadline,
byte/growth bounds and process-group supervision. Validate actual image dimensions,
mode and decodability before publishing. CLI warnings about adjusted parameters
must not silently change the recorded request; reject incompatible geometry/DPI,
or explicitly record the actual accepted capture before publication. Retain
immutable capture metadata per page and keep native tool stderr private/bounded.

PNG capture is an application bitmap format, not a promise of an uncompressed
optical source. The Canon advertisement offers JPEG/PDF on the wire; a lossless PNG
or PDF image embedding prevents another lossy output step, but cannot reverse
compression already performed by the device. User photo/output encoding is
independent from capture and OCR. Real Canon transport/encoding still needs proof.

## Stop and recovery

TERM, bounded grace, escalation, direct-child wait and process-group disappearance
are local process evidence only. Neither successful exit nor lease heartbeat
expiry proves the physical scanner is idle. Persist cancellation, prevent late
publication, reconcile hardware status after stop, and retain the lease while
activity is uncertain. Recovery never repeats a physical scan automatically.

Before invoking the real scanner, stop the old scanservjs writer under the parent
migration procedure; do not run two control paths. Probing supported capabilities
and image acquisition are distinct tests. The test backend cannot prove Canon
cancel, network interruption, scanner power cycles or device recovery.

## Native CLI evidence

The plan's `sane-cli-spike-20261102.json` records official Ubuntu packages extracted
under `/tmp`, without installation or patches. Only the SANE test backend was
loaded. It generated RGB/gray PNG at 300/600dpi for a synthetic 100×150mm region,
returned an injected busy error, and stopped a deliberately slow read on TERM with
no surviving local process group. Temporary scan outputs were removed. This is
not A4 accuracy, production resource budgeting or a product adapter acceptance.

The record-side [scan admission/claim](scan-admission.md) now freezes capture,
worker boot and lease epoch, with capacity/idempotency guards. Native capture is implemented behind `internal/scanner.Capturer`; connecting it
to scan jobs and hardware reconciliation remains pending.

## Product adapter evidence

`internal/scanner/sane` accepts a typed immutable plan with the registered opaque
device ID, preset geometry, color, DPI and expected pixel dimensions. It invokes
one explicit flatbed command with a restricted environment and validates the full
PNG, dimensions, mode, hash and byte size before exposing an artifact. The caller
receives an owned attempt on partial failure as well as success. After confirmed
publication/read-back, `Release` retains the original; an aborted attempt removes
only its own directory and syncs the parent. Unknown process-group stop refuses
cleanup and transfer. Local stop timestamps never establish physical idle.

The file-growth watchdog observes image plus diagnostics every 25 ms and checks
again after exit. It can overshoot between observations; it is not a hard disk
quota. Backend capability resolution, final-image configuration isolation,
fresh physical-idle evidence and durable scan dispatch/recovery are still needed.

`scripts/check-scanner.py` exercises the actual upstream binary with only `test:0`
enabled. Tests cover RGB/gray 300/600 DPI at 100×150 mm, A4 gray at 300 DPI,
busy errors, cancellation and owned-file lifecycle. The synthetic A4 fixture
extends `geometry_max` to 300 mm; its configuration follows the pinned
[upstream test backend](https://gitlab.com/sane-project/backends/-/raw/1.2.1/backend/test.c).
This does not prove Canon dimensions, optical DPI, transport or cancellation.
The receipt is `tasks/43/scanner-adapter-validation-20261102.json` in the plan
directory. Existing scanservjs and real scanner state are unchanged.

## Generated configuration

`ConfigFiles` serializes one registered device using its opaque ID rather than
its display name. It accepts HTTP(S) endpoints without credentials, query or
fragment and rejects INI comments, delimiter injection, quotes, whitespace and
control characters. Discovery and WSD discovery are disabled; debug and hexdumps
are off. These settings follow the extracted 0.99.29 packaged manual.
`WriteConfig` creates unique private directories, exclusive 0600 files, and syncs
files, directory and parent before returning. On partial failure it returns the
created directory so the caller can reconcile cleanup. The caller must retain it
until native processes have stopped.

This generator does not contact devices. Runtime wiring, device-name resolution,
backend capability probes and final-image rejection of fallback system
configuration remain pending. Generating a private file alone does not prove
that the packaged backend cannot merge system configuration.

The [claimed scan executor](scan-executor.md) now connects the acquisition port
to monitored claimed jobs and conditional publication/cleanup. The production
driver resolver, physical idle observer and executable dispatch remain pending.

A [read-only eSCL observer](escl-status.md) now implements the status port and is
connected to the executor in local HTTP fixtures. Real Canon idle/cancellation
acceptance, combined production driver setup and executable dispatch remain pending.

[Backend capability inspection](sane-capabilities.md) now reads the selected
SANE device constraints with `--all-options`. Selected-option inspection now validates the requested flatbed/mode/DPI/area against active constraints and read-back without acquisition. [Expected pixel geometry](scanner-geometry.md) and combined configured-driver resolution still require implementation/acceptance.


A [registered driver](registered-scanner-driver.md) now connects actual selected-option and native-parameter inspection, exact one-use capture plans and fresh idle observation to the existing scan executor. The integration proof uses official test:0 capture with a local eSCL status fixture. Production environment/configuration ownership, capability/readiness publication, worker scan dispatch/recovery and physical acceptance remain pending.
