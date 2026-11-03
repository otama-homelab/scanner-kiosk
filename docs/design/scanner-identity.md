# Advertised scanner identity binding

The registered eSCL endpoint supplies an advertised UUID. This observation is
not an authenticated hardware credential or an exclusive writer fence. The
production readiness producer and physical ownership handoff remain unimplemented;
this change does not make a scanner ready automatically.

## Observation and canonical identity

`escl.Observer.ObserveIdentity` performs one uncached, bounded GET of the
registered endpoint's `ScannerCapabilities`. It uses the pinned HTTP client,
without proxy, credentials, redirects, retries or TLS verification bypass. It
rejects non-200 responses, aged/compressed responses, oversized bodies and invalid
XML. The shared XML parser bounds bytes, nodes, tokens and depth and rejects DTDs,
directives and additional roots.

The document must contain a direct `scan:ScannerCapabilities` root, one nonempty
direct `pwg:Version` and exactly one direct `scan:UUID`. A hyphenated UUID or UUID
URN is decoded to 16 bytes; case differences normalize to the same identity.
Nil/max UUIDs, compact/braced values, duplicates and nested substitutes fail.
The internal ID is `physical_` followed by the hex SHA256 of
`scanner-kiosk/device-uuid/v1\x00` and those bytes. There is no serial or URL
fallback. Observations redact implicit formatting and JSON output.

Aliases advertising the same UUID therefore share an ID. Different UUIDs for a
shared physical engine are not discovered automatically. A future trusted
producer must verify that mapping and fence all writers, including legacy/local
processes, throughout acquisition. An opaque hash and recorded timestamps do not
provide that ownership guarantee.

## Admission and native boundaries

`AdmitReadyScan` copies physical and handoff IDs from the same readiness
transaction into the immutable scan snapshot and rechecks that authority before
commit. Browser-supplied IDs are rejected. Withdrawal after admission does not
rewrite the saved binding.

Production `scannerdriver.Session.Resolve` rejects unbound or partially bound
work before native IO. Within the registered device gate, it checks the expected
identity before native metadata resolution, immediately before capture, and
before and after the physical Idle observation. A real mismatch latches that
device unresolved: later Idle or Close cannot clear it, and the worker retains
the held lease and reservation rather than publishing a page or releasing the
device. A transient network/context failure before native start remains a known
no-start case; cleanup still requires fresh matching identity and Idle proof.

The public observation method also holds the device gate. Close joins in-flight
observations; guard/workspace ownership is checked around the HTTP operation.
Separate device gates preserve local isolation. Whole-daemon native crash and
restart recovery remain separate unfinished acceptance work.

## Compatibility and evidence scope

The existing v1 internal snapshot gains optional physical/handoff fields; no
public API setting or database migration is added. Both process roles must use
the same pinned image; mixed old workers are unsupported. Generic driver tests
can still exercise trusted legacy unbound work, but the production Session cannot.

Tests cover strict XML/UUID parsing, HTTP bounds, substitution at resolve/capture
and after capture, lifecycle joining, gate isolation, admission persistence and
client injection rejection. Runtime tests use local HTTP endpoints, fake native
executables and SQL ownership/readiness fixtures. They establish the software
binding boundaries, not real hardware ownership, physical writer fencing or
production readiness publication. Real scanner acceptance remains required.

See [readiness admission](scanner-readiness.md) and
[registered driver](registered-scanner-driver.md).
