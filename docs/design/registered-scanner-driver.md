# Registered scanner driver

Current identity boundary: production Session requires physical/handoff-bound
work and verifies the registered endpoint's advertised UUID before native
metadata/capture and before/after Idle. Mismatch retains unresolved state and
durable resources. Public observation joins the device gate and Close lifecycle.
See [identity binding](scanner-identity.md) for scope and compatibility. Scan HTTP
admission and runtime dispatch are connected; production readiness publication
and real physical fencing remain pending. Local fixtures verify these software boundaries; they do not establish physical
ownership or production readiness.

`scannerdriver.Driver` implements the existing scan executor's driver port using a registered backend map and an independent idle observer. Construction is local and does not discover devices or create idle leases. The caller supplies the fresh worker boot ID, native adapters built from registered configuration, deployment bounds and an observer bound to those same registered IDs. Public URLs/native arguments are absent from claimed work. The environment-fed Configure/Session component is now implemented below; worker startup now invokes it behind explicit acquisition opt-in; scan HTTP admission uses trusted readiness records, while their production producer remains pending; worker dispatch uses the configured Session for physically bound, boot-pinned admitted jobs.

Resolve accepts one already-claimed ScanWork. It verifies boot/job/device/revision/deadline and application capture enums before native inspection. Selected constraints and native parameter read-back are inspected sequentially; each stopped inspection must be cleaned before the next stage. It verifies parameter format and deployment pixel bounds, builds a Plan from backend dimensions and caps bytes by the existing reservation. It does not start acquisition during Resolve. The returned capturer accepts that exact Plan once, under the immutable job deadline. Mutated settings and repeated calls cannot start another scan. A bounded per-instance job-ID guard retains consumed IDs until their immutable deadlines; this is additional replay defense, not durable authority or a replacement for store claim/recovery fencing.

Native operations and idle observations are serialized through a context-aware gate. The idle port is restricted to registered IDs and fresh observations after known local stop; cached, future or invalid observations fail. The real eSCL observer remains a read-only GET port. Local exit, successful inspection and an Idle response do not independently establish initial device readiness, release durable resources or fence another writer.

`scanner.ErrUnresolved` carries unknown process state, retained inspection cleanup or a prior execution into the executor. Resolve errors carrying it leave launch/stop unknown, so the executor does not fabricate a no-capture stop timestamp or use idle to release the lease/reservation. Unknown inspections are retained and not cleaned while stop is unproven. Close may retry cleanup only for known stopped inspections, waits for the native gate with its caller's context, and never releases durable job resources. A duplicate capturer invocation returns no stop proof because the first invocation may still run. Definitive pre-native refusals can carry an explicit no-start attempt instead.

`scannerdriver.Native` bridges the concrete SANE adapter's inspection/capture boundaries. The concrete adapter documents that a nil attempt is returned only before allocation/start; this bridge converts such error refusals into explicit no-start evidence. Unknown arbitrary backend results remain unresolved. Other implementations can use the backend/observer ports without changing the executor or document store. Parameters.Validate shares the strict format/selection/budget checks with the helper decoder. Configure/Session now owns generated configuration and checks its identity/content before native operations. The helper is packaged in the locally checked image; executable/backend version binding and production scanner readiness remain pending. HTTP admission is connected to the readiness repository boundary.

Default tests cover exact settings, boot/device/deadline/reservation rejection before IO, one-use plans, parameter bounds, unresolved cleanup/stop retention, ordinary stopped failures, fresh idle, and concurrent duplicate/close behavior. A scan executor regression proves unresolved driver inspection cannot release held leases/reservations. Tagged official-backend integration connects real selected-option inspection, native parameters, virtual PNG capture, the existing monitored executor and real document transactions. Local HTTP eSCL fixtures prove Idle publication and Processing refusal followed by fresh Idle cleanup. These are virtual scanner/status fixtures, not Canon acceptance or an operational scan HTTP endpoint.


## Environment-fed session and input ownership

Worker config loads `SCANNER_SCANIMAGE_PATH` (default /usr/bin/scanimage), `SCANNER_SANE_PARAMETERS_PATH` (default /usr/libexec/scanner/sane-parameters) and `SCANNER_SANE_LIBRARY_DIRS_JSON` (default []). They are deployment metadata only: loading does not stat/probe tools or contact devices, and the API role does not read them. Paths/JSON/list bounds and ambiguous library separators are validated without echoing values. Scanner IDs now consistently use the public/driver maximum of 96 characters. Compose/Helm examples pass these settings only to the worker; design checks verify placement and Compose overrides. These examples are not deployed.

`Configure(ctx, workerConfig, liveGuard)` requires that the live coordinator guard matches both the private document-directory inode and its lock inode. A hard-linked lock in another directory is insufficient. It validates all registered IDs/driver types/native endpoint syntax before creating files. No native process, status GET, discovery, acquisition or ready/idle transition occurs during construction. It creates one private config workspace and one single-device config directory per device, then constructs native adapters and the pinned eSCL observer using the fresh guard ID. The fixed native name is airscan:e0:<registered-id> for the isolated single-device config; deployment isolation and real airscan acceptance remain separate gates.

The caller retains a nonnil Session even when construction fails, and must Close it before releasing the borrowed guard. Configure does not release or reacquire the guard. Native/status callbacks verify the guard/root binding and generated input identity/content before proceeding. Configuration changes invalidate the session; restoring files allows cleanup, not hot resumption of a closed driver. Startup recovery/exclusive-writer gates are still the worker composition root's responsibility.

Configuration writes use SANE's shared serializer and `WriteConfigAt`, relative to a pinned os.Root. A renamed/replaced absolute parent cannot redirect writes into its replacement. Cleanup first closes/drains the driver, then preflights the entire owned tree using directory/file identities, private permissions, bounded file counts and content hashes. Foreign files, modified content, symlinks and replaced directories cause retention before any deletion. It removes only tracked generated names, never recursively deletes unknown content, syncs directories, and preserves incremental cleanup state for retries. Unknown local work prevents config cleanup entirely. Constructor failure after file creation preserves cleanup ownership. Neither construction nor Close creates/releases durable scanner leases or reservations.

Tests cover no device IO during construction, failed-constructor cleanup, API/guard/endpoint/ID rejection before writes, foreign/changed/replaced input retention and restoration, guard loss before status IO, root binding despite hard-linked lock files, pinned-parent writes and API/worker environment boundaries. The session factory has configuration/lifecycle coverage with placeholder executables, runtime capture coverage with fake native executables, and separate tagged real virtual-backend driver/executor coverage. Worker Run now invokes it after the recovery gate only when SCANNER_ACQUISITION_ENABLED=true (default false, strict true/false, worker-only). Construction failures stop startup and clean any nonnil partial session. Shutdown withdraws/joins metadata publication and processing before bounded Session.Close, then releases stores/guard. A cleanup failure is joined with ErrReconciliation and retained inputs; cancellation does not hide it. The opt-in does not prove hardware fencing or publish scanner readiness. Runtime tests cover disabled missing tools, enabled constructor failure before dispatch, cancellation cleanup, foreign-input retention and zero status requests/lease creation. Worker native dispatch is now connected for new boot-pinned admissions; runtime tests use fake native executables, local eSCL identity/status endpoints and bound AdmitReadyScan with SQL ownership/readiness fixtures. HTTP admission and capture UI are connected; production readiness generation and real scanner/mobile acceptance remain pending.


InspectCapture now shares Resolve's read-only selection/parameter/stop/cleanup path, without consuming job IDs or invoking Capture/idle. Session exposes that port to the enabled worker metadata publisher. Worker-to-API tuple publication is connected; device readiness and HTTP scan admission remain separate. See [scanner metadata](scanner-metadata.md).

The driver now serializes native operations per registered device. The shared
job-use fence still prevents replay across devices; Close joins every device and
preserves unresolved evidence. See [multiple scanners](multiple-scanners.md) for
the per-device admission/runtime integration and remaining physical acceptance.

## Explicit native CA trust path

Worker configuration optionally reads the standard `SSL_CERT_FILE` variable into
`ScannerTools.CAFile`. A present value must be a bounded, clean absolute path;
an empty/relative/traversing/NUL/CR/LF value fails without echoing it. Loading
configuration does not stat/read the file or contact a device. The API config
loader does not consume this native scanner setting. Go's own standard TLS root
handling remains separate from that config loader.

The guarded factory passes this field into `sane.Options.CAFile`, where path
syntax is checked again for programmatic callers. The adapter adds only the
explicitly configured `SSL_CERT_FILE` to its existing clean child environment.
Both inspection/parameter helpers and capture use that same environment; ambient
CA settings, API keys and OIDC credentials are not inherited by the adapter.
The onefile native wrapper now preserves this one standard variable alongside
its existing allowlist, including an explicitly empty value for native rejection;
it does not forward `SSL_CERT_DIR`. When no explicit value exists, the new
onefile selects its integrity-checked bundled PEM for the app and native tools.

The TLS-verifying sane-airscan fork uses only the specified PEM file when this
variable is present. A missing/invalid file cannot fall back to system trust.
Without the variable, backend system-trust behavior applies. The prior 6eab/0.99.36 payload does not implement this verification and remains
historical. The new unpublished native input contains the TLS-verifying fork and
CA bundle, with actual onefile wrapper TLS evidence. Distroless TLS, full
processing/physical acceptance and complete source/license/security gates remain
pending.

Tests use fake native processes to prove the config/factory/inspection/capture
path and credential isolation, plus the tagged wrapper contracts. These are
separate from the real fork's isolated TLS tests and from physical scanning.
