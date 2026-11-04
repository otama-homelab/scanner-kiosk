# Deployment, resource and release contract

Status: local implementation and an unreleased image candidate are under validation. Container fixture evidence exists; production storage, Helm deployment and release acceptance remain pending. See [packaging](image-packaging.md) and [dependency security](dependency-security.md).

## Process and deployment shape

One application replica, one image, API and worker roles. Build Astro at image-build time and embed the static output in Go. Node is not a runtime service. The API never performs scanning/OCR/PDF work inside an HTTP request. The worker supports bounded concurrent scans on distinct devices; rendering/OCR remains single-concurrency, and physical scanner leases remain per device.

The intended container layout is API and worker containers in the same Pod sharing the two mounted stores. Separate memory limits keep an OCR child failure from directly killing the API process. The worker launches owned processing subprocesses with an allowlisted environment and job-only file references. It does not receive OIDC client credentials. Separate containers are an operational/resource boundary, not a claim that hostile administrator-installed plugins are safely sandboxed.

Use Recreate rather than overlapping rolling scanner writers. Quiesce new admissions, request cancellation/drain, wait for confirmed process/device release, and only then replace the worker. Pod/node loss with uncertain hardware activity is reconciled before a new scan. No active-active or multi-node automatic scanner failover is promised initially.

The [physical writer preflight](physical-writer-fencing.md) separates the known
legacy Pod path from host/LAN writers and preserved printing. Its candidate is
not referenced by the chart; passing server dry-run does not authorize applying
it or enabling readiness. API and worker share one Pod network identity.

## Environment configuration

`.env.example` lists configuration keys for the implemented role-specific loader; it contains no working credentials. Deployment/operator configuration is environment-only, including OIDC, plugin/model profiles, endpoints and resource limits. JSON-valued environment variables express structured profiles. Secrets are separate environment values supplied by the deployment secret mechanism, never committed or returned to the browser. The server does not load an arbitrary user-provided settings path.

The executable loader must reject unknown `SCANNER_` keys, unsupported auth/driver/plugin values, duplicate IDs, bad URLs, absent required secrets, invalid numeric bounds and incompatible defaults. OIDC outage or incomplete OIDC configuration never enables guest mode. Guest mode is an explicit deployment choice, with separate server principals. Disabled optional providers need no secret.

Validate exact public origin and preserve the Host header through the known reverse proxy. Redirect URLs derive from configured public URL, not untrusted forwarded headers. Default session cookies are Secure/HttpOnly/host-only/Lax with the `__Host-` prefix. Local browser development also requires HTTPS; the runtime has no insecure-cookie mode. See [HTTP runtime](api-runtime.md) for current routes, startup, health scope and admission bounds.

## Initial bounded resource policy

| Resource | Starting policy | Acceptance still needed |
|---|---|---|
| API memory | 256 MiB request / 512 MiB limit candidate | Real preview/history polling, many sessions, large responses |
| Worker memory | 512 MiB request / 1536 MiB limit candidate | Real 300/600dpi color/gray, OCR model/preset, interrupted work |
| Worker CPU | Up to 2 CPUs, one heavy job, OMP_THREAD_LIMIT=1 for initial Tesseract runs | Throughput and cancellation response |
| Per-page pixels | 40 million maximum | Actual device geometry and peak decode/transforms |
| Document/job limits | 20 docs / 500 MiB per owner; 50 pages / 200 MiB per job | Disk reservations, compressed inputs that expand, full quota/disk |
| Execution time | Scan 120s, OCR 600s, process-stop grace 15s candidates | Real TS8130 behavior and model performance |
| Content TTL | 24h from first admission, physical cleanup target 5min | Clock edges, streams, failed cleanup, restart and storage policies |
| Request receipts | 72h, content-free digests/tombstones | No stale command restarts a physical scan |

These are admission/container bounds and candidate defaults, not measured capacity claims. Every deadline is capped by document expiry. Defaults may be lowered after experiments. 1200dpi remains unavailable; advertised hardware support is not resource acceptance. Bytes include original and derived data/reservations, not just the final download. Reserve before reading; bound writes and clean aborted staging data. Do not evict another user’s documents to make room.

Artificial pdfcpu processing of three 2480x3508 images completed in the recorded spike; that isolated run cannot set an OCR/600dpi/container budget. Release measurements must include worker/API memory separately, native child peak memory, CPU, temp volume, output size and elapsed time. LLM/GPU inference may be an explicitly configured external service; it is not a mandatory local GPU workload.

## Storage and backup

The durable state mount and temporary document mount are separate. The latter includes its SQLite DB/WAL, originals, thumbnails, OCR artifacts, all intermediate/temp files and exports. Point every processing temp directory into that mount; never silently fall back to the container’s general temp filesystem.

Before production, verify that the chosen temporary volume is outside recursive NAS snapshots, replication and offsite jobs. Existing scanservjs storage is known to be protected and is not suitable merely because its PVC exists. New storage placement/provisioning is a separate pending infrastructure action. A local directory example must not be described as snapshot-safe without checking its parent.

Configuration recovery uses a logical export of permitted durable records. Restored sessions and authorization attempts are invalidated. Original-document restoration is deliberately not part of configuration recovery. Normal process restart preserves valid sessions and unexpired local documents, reconciles workers, and reruns cleanup.

The executable [logical identity recovery commands](state-recovery.md) now
export OIDC/simple durable identity records and restore them only into a fresh
private state root, excluding all sessions/protocol/document state. They do not
recover deployment secrets, prove physical scanner release, replace an existing
volume or complete the production upgrade/rollback procedure.

## Health, logs and failure visibility

Liveness shows process health only. Readiness requires valid configuration and both stores; an unavailable optional OCR endpoint should not make basic scanning unavailable. Device/provider status is a separate capability/health projection. Do not expose DSNs, tokens, environment dumps or original filenames.

Logs record stable request/job/internal-principal IDs, stage, result, error code and timings. Never log text/image payloads, full provider request/response bodies, authorization codes, state/nonce/verifier, session cookie or API key. Native tool stderr is bounded and normalized before logging. Use secret-canary tests, including debug log levels of third-party clients.

## Release and rollback

The selected channel policy is [trunk previews and tagged releases](release-channels.md): `main` pushes are preview inputs and `v*` tags are release inputs. Source validation is wired; actual publication remains gated on the complete native distribution and release evidence.

A release needs: pinned Go/Node/dependency versions and image digest; upstream license notices/SBOM; dependency/container checks; reproducible static/build tests; Compose and Helm examples; OIDC examples; configuration backup/restore; schema-version checks; and upgrade/rollback instructions. No nonexistent image or unexecuted CI is a successful release.

[Distribution boundaries](license-distribution.md) separates observed browser
output, build tooling, Go/native binaries and optional fonts. Archive notice
gaps must be reviewed against the material actually distributed; the current
browser module observation does not clear the project or native license gates.

### Source CI candidate

[Source checks](../../.github/workflows/checks.yml) is a GitHub Actions candidate
for `main` pushes, `v*` tag pushes, pull requests and manual runs. It pins official checkout/setup
actions by commit and uses the source build's Go 1.27.3, Node 24.22.2 and npm
11.21.1. It verifies Go modules, runs race tests, builds the embedded frontend
through the existing build script, tests the embedded assets, checks the version
command and rejects changed dependency locks. Frontend installation and Go tests
run sequentially. The token has read-only contents permission and checkout does
not persist credentials. No dependency caches or build artifacts are uploaded.

A separate `deployment-contracts` job checks the Kubernetes configuration on
Ubuntu 24.04/x86_64 using CPython 3.12, hash-locked PyYAML 6.0.3 and Helm 3.21.1
whose official archive SHA256 is checked before extraction. It runs Helm lint,
rendered ENTRYPOINT/args, temporary-cache and probe contracts, and rejects missing
inputs, mutable image digests and reused PVC names. It has no cluster credentials
and performs no deployment or native scanner execution. It does not build or
approve the complete onefile/native distribution.

The source repository is https://github.com/otama-homelab/scanner-kiosk and is
currently private. Hosted source checks passed all four jobs for commit
c4a4fa799085c2f37b509c98a4068b99d60a6179 (run 41787580872), including the
required-checks aggregate. Main branch protection remains unconfigured: the
Free/private repository API rejects it with HTTP 403; public/private visibility
is still an operator decision. A paid plan or visibility change has not been
applied. A source-check pass does not approve distribution: this workflow
does not publish an image, create a release, deploy, or implement the image SBOM,
vulnerability, license/source-delivery and hardware/recovery acceptance gates.
Those must be completed for the exact release inputs before publication. Current
dependency findings and unresolved license obligations remain blockers.

The workflow also runs the offline Debian source collector safety tests. Actual
source retrieval is an explicit release-preparation operation described in
[dependency evidence](dependency-security.md#retaining-debian-source-files); CI
does not download or upload the source bundle.

The compiled recovery check also runs in source CI. It covers identity recovery
against a preserved synthetic document root, owner separation and restart with
an empty document root. Its receipt stays in runner temporary storage; no
recovery archive or document fixture is uploaded. Hosted execution and full
infrastructure/secret recovery remain separate acceptance work.

Migration versions/checksums prevent an older binary opening a newer incompatible DB. Introduce backward-compatible migrations where possible; rollback to an older schema requires an explicit recovery path, not disabling the version check. Draining and scanner reconciliation precede both upgrade and rollback. Never copy new private outputs into the old shared output directory as a rollback shortcut.

### Operator sequence for upgrade and rollback

This is the procedure to accept on the selected storage/deployment before
production, not evidence that a live upgrade has occurred.

1. Record the old/new immutable image digests (or binary checksums), matching
   deployment configuration, numeric UID/GID, claims/paths and migration files.
   Keep the old executable/image available. Recover secrets through their
   existing managed system; identity export does not contain deployment secrets.
2. Stop new admissions and edits through the deployment's maintenance routing,
   retaining monitoring/cancel access. Verify new work is refused. Drain active
   jobs or request cancellation and verify local processes have stopped and the
   physical device is reconciled. An unknown job/held lease is a stop condition;
   do not erase it, release it manually or start a second writer to continue.
3. Export identities with the **old** executable to a new private archive outside
   the document volume. As the same application account, the standalone command
   is `SCANNER_STATE_DIR="$state_root" scanner state export "$new_archive"`.
   Use an existing private backup directory, verify success/private file mode
   and retain its checksum. For a container, mount that private backup directory
   explicitly and invoke the same command through the existing entrypoint;
   an archive left only in an ephemeral container filesystem is not a backup.
4. Stop the old application normally and verify termination before replacing it.
   Keep both existing data roots and their ownership unchanged; do not copy live
   SQLite DB/WAL files, reuse the legacy shared output root or snapshot documents.
   Apply the new pinned artifact/configuration with one writer and Recreate.
   Acquisition stays disabled until storage/schema startup and device ownership
   checks pass. A failed migration/startup keeps maintenance in place.
5. Check API health, fresh authentication, an existing owner's unexpired document
   visibility and denial to another owner, then perform the accepted scanner
   smoke task. Confirm job terminal state, stop/release and cleanup. Only then
   reopen admissions. A health probe alone is not the scanner acceptance result.

For rollback, first repeat the maintenance/drain/confirmed-stop steps for the
new worker. Reuse the old artifact with existing stores only when its checked
migration versions/names/hashes accept **both** resulting databases. Even an
additive SQL migration is not automatically downgrade-compatible: storage.Open
rejects an unknown version or changed checksum. Never edit migration rows or
retry with guards disabled.

If the old artifact cannot open those schemas, remain in maintenance. Restore
its pre-upgrade identity archive with that old artifact into a **new, empty,
private state root** using `SCANNER_STATE_DIR="$fresh_state_root" scanner state
restore "$preupgrade_archive"`. Use a separate empty document root; this recovery
restores no sessions or documents. Confirm the impact with the operator before
switching volumes: existing pending outputs must not disappear through an
implicit recovery choice. Keep the stopped roots available only within their
existing retention/privacy policy; recovery is not permission to retain scans
longer or include them in backup. If required preconditions or compatible
recovery materials are absent, stop rather than inventing a downgrade.

The initial transition back to legacy scanservjs is a separate cutover rollback:
stop/reconcile the new writer first, then restore the recorded old route and
writer controls. Never run both writers or import private outputs into legacy
shared history. Record the selected-artifact pair, storage results, known-job
outcomes and operator acceptance before marking the real upgrade/rollback gate.

The existing portal/scanservjs/Stirling and printing remain unchanged during development. Production switch, URL/auth changes, old output ownership/retention, and eventual PVC/snapshot deletion follow the parent migration gates. Local image build/runtime fixture checks are connected. Actual storage/CSI, native/OCR combined load and deployment acceptance remain pending; the initial absence of a local Docker daemon was resolved using the existing Docker Desktop tool.

## Executable core configuration validation

`internal/config.Load` now reads public origin/listen address, explicit auth mode,
OIDC client settings, registered scanners, storage paths, retention/timeouts and
resource limits through an injected environment lookup. Missing required values
and invalid values return a key-only error. There is no automatic guest fallback.
The default is OIDC, and a public HTTPS origin is mandatory for the API. Canonical selectors are oidc/simple; named none remains pending, and legacy guest requires the legacy selector.
The two storage paths must be absolute, clean and non-overlapping. Filesystem
symlinks/mount identity must additionally be checked when opening the stores.
Scan concurrency is independently bounded to 1–32, registered device count and reservation capacity; render processing concurrency remains one.
Scanner endpoints remain administrator input; this parser performs no network I/O.

Secret values support redacted fmt/JSON output; adapter use requires an explicit
conversion. This does not authorize dumping the configuration or environment.
Tests cover authentication settings, inconsistent limits, malformed scanner JSON
and secret canaries. The loaders and independent OCR provider composition run
in their respective executable roles. Successful core validation is not a claim that all env settings or
OIDC discovery, filesystem access, scanner connectivity or model health work.

Role-specific loading now separates `config.Load` (API) from `config.LoadWorker`.
The worker loader deliberately never reads OIDC/public-origin/listen settings;
worker composition requires a Worker role and resolves only selected OCR keys.
This fixes the initial loader's implicit requirement to supply the API's OIDC
secret to a worker. Common limits and scanner metadata still undergo validation.


Local release-mode resource measurements now exercise real HTTP admission/status
and normal worker PDF/thumbnail processing with synthetic A4-equivalent300/600dpi
pages, default50-page jobs and bounded output expansion failure. See
[resource measurements](resource-measurement.md). Worker deployment examples now
set the standard Go GOMEMLIMIT=1024MiB soft runtime budget under the separate
1536MiB container limit. Native/OCR/parallel-device/container and real storage
acceptance remain pending; local measurement success is not G5 acceptance.


## Observed realtime discontinuity in local acceptance

A WSL2 OCR bad-version test captured a correctly rejected OCR result followed by
stop-proof refusal: the reported process stop was 78ms earlier than the job's
immutable start. FinishStoppedRender returned ErrConflict, kept the job running
and retained its reservation. A separate two-goroutine, 20-second clock probe
observed a roughly 97.163ms backwards UnixNano step while Time.Sub continued
forward. This explains that captured cleanup failure; other historical failures
have not been individually attributed.

The observed host used kernel 6.18.54.1-microsoft-standard-WSL2 and clocksource
`tsc`; systemd-timesyncd was active and NTP reported synchronized. The process
responsible for the correction remains unidentified. No host clock settings,
stop validation, test expectations or retry behavior were changed. Full local
integration acceptance remains pending clock stabilization or verification in a
stable clock environment. A successful isolated retry does not clear the stored
failure, and a backwards timestamp must not authorize reservation release.


### Proposed host clock remediation (not applied)

[Ubuntu's WSL time-sync documentation](https://ubuntu.com/wsl/docs/stable/explanation/time-sync/)
recommends disabling systemd-timesyncd on Ubuntu 24.04 and earlier when the
Windows/Hyper-V implicit synchronization is used. Local read-only preflight found
Ubuntu 24.04.2, implicit synchronization enabled, guest timesyncd active/enabled,
and Windows Time running. Windows source inspection returned access denied;
noninteractive sudo is unavailable. The recent guest journal did not identify
a correction event. Similar reports in [WSL issue 11790](https://github.com/microsoft/WSL/issues/11790)
are corroboration, not proof of this host's correction actor.

The concrete administrative change for review is:

```sh
sudo systemctl disable --now systemd-timesyncd.service
```

It changes the current Ubuntu unit's active and boot-enabled state. Original
state was active/enabled; rollback is:

```sh
sudo systemctl enable --now systemd-timesyncd.service
```

After an administrator applies the change, verify inactive/disabled state and
unchanged Hyper-V implicit synchronization, observe realtime/monotonic deltas
for at least 60 seconds, repeat the OCR bad-version case 100 times, then run the
full Go race/design suite. Retain all earlier failures. Reappearance of backwards
steps leaves acceptance incomplete and requires further clock-authority analysis.
This plan introduces no product timestamp clamping or relaxed stop evidence.


### Initial catalog publication under a backward wall clock

The catalog's lease deadline alone does not establish usable authority. Writes
also require the observed wall clock to be at least the stored heartbeat. A
backward step between Begin and the first Publish therefore rejects publication,
cancels the catalog publisher and withdraws its snapshots, even if valid_until
is still in the future. Advancing the wall clock afterwards does not revive the
stopped authority. This preserves the existing fail-closed behavior.

`TestCatalogInitialPublicationClockRegressionStopsAuthority` uses the real SQLite
catalog store and publisher lifecycle with an atomic controlled clock. Its steady
control publishes; its backward case moves from 1000 ms to 999 ms before first publication,
observes worker-context cancellation and stopped authority with the original
1000 ms heartbeat/31000 ms deadline, and confirms no revival after 1001 ms. Twenty
race-enabled repetitions passed. This proves a possible causal path matching the
earlier stopped/future-deadline failure shape, not the cause of every historical
failure or end-to-end scan safety.

A current 65-second read-only host probe still observed two realtime backward
steps (about 40.9 ms and 41.3 ms), while monotonic time continued forwards. Guest
timesyncd remained active/enabled and Hyper-V implicit synchronization remained
enabled. The correction writer is still unidentified; no host setting was
changed. The administrator remediation and full validation above remain open.

```sh
go test -race -count=20 \
  -run '^TestCatalogInitialPublicationClockRegressionStopsAuthority$' \
  ./internal/workerruntime
```

This controlled-clock regression cannot clear host clock stability or substitute
for the original failed full Go run, OCR repetitions and subsequent whole-suite
acceptance in a stable environment. No timestamp clamping, authority retry,
lease release or reduced validation was introduced.

### Read-only timesync correlation (2026-11-04)

A later 90.045-second host observation sampled realtime/monotonic every 1ms
(82,497 samples) and read timesyncd's `NTPMessage` about every 250ms
(353 successful queries). It captured three backwards wall steps of
-36.165/-33.836/-28.186ms, spaced 30.001/30.002 seconds apart. Each preceded
the first observed increment of timesyncd's packet counter (2573→2576) by
approximately 394/223/313ms. The read-only preflight reported a 32-second poll
interval and an NTP offset of -38.330ms; no recent guest journal entries were
returned. The interval report and measured event spacing are retained separately.

This strengthens a time-sync interaction hypothesis; it does not identify the
clock-setting syscall/process. Some steps occurred before a query still reported
the old packet count, so the query brackets are not exact correction intervals.
No host setting changed, and the proposed administrator remediation above is
still unapplied. Clock acceptance and the required subsequent OCR100/full-suite
checks remain open. Raw observations are in the parent plan's
`clock-timesync-correlation-20261104/current-evidence.json`.


### Current onefile identity recovery acceptance

The existing recovery fixture passed all eight checks using current onefile
`e5363774…` directly, with private launcher cache defaults and no native tool
path overrides. It exercised logical export from a live API, exclusive archive
creation, restore into fresh state, old-session rejection and fresh login,
preserved document ownership/bytes, rejection of an active destination,
restart with an empty document store, and malformed/absent-source rejection.
All owned API processes stopped normally. This completes the current-candidate
local identity recovery check required by the distribution acceptance plan.

The stores and document payloads were synthetic, with real SQLite and loopback
HTTP; the fixture manually transported Secure cookies. Actual PVC/NAS loss,
HTTPS/IdP, storage faults and upgrade/rollback remain distinct existing
acceptance requirements. Evidence is in the parent plan's
`current-onefile-state-recovery-20261104/current-evidence.json`. Only the fixture's
binary selection and scratch environment changed; product code was unchanged.


### Current contrast-fix development candidate

The source input-boundary contrast fix is now included in onefile `7685e0c3…`
and distroless image `c8bf0afc…`. Actual HTTP hashes matched all five packaged
assets without overrides; only style.css differs from the earlier `e5363774…`
candidate. All selected Go/SQL/module source files and 232 native entries remain
unchanged. The new onefile passed 12 capture and 12 bilingual keyboard checks,
and its API stopped normally. The image retained its pinned base layers,
ENTRYPOINT/CMD and nonroot identity, and completed a fresh-cache version smoke
under a 768MiB/no-swap limit. This is not a minimum-memory guarantee.

The source contrast fix is no longer waiting for local packaging. The candidate
still reports dev/unreleased; no registry publication, deployment, physical scan
or new full-suite acceptance occurred. Existing broader receipts retain their
original artifact scope. Current evidence is in the parent plan's
`contrast-onefile-20261104/current-evidence.json`.
