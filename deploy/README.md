# Deployment design examples

These are G3 configuration/rendering examples, not a released image or successful deployment. The `scanner serve` / `scanner worker` roles, health endpoints and environment loader are implemented locally; an unreleased image packaging candidate is under validation. See [packaging scope](../docs/design/image-packaging.md). Do not point the examples at the old scanservjs output volume.

Compose requires an explicitly selected released image digest, public origin, scanner registry and two pre-created directories. It binds the API to loopback for a separate HTTPS reverse proxy and never automatically creates missing bind directories. Named LLM credentials must be passed only to the worker; the API OIDC secret is not forwarded to it.

The Helm example requires distinct existing state/document claims and an immutable image digest. It does not provision storage or claim that a volume is excluded from snapshots. Credentials use separately managed API/worker secrets. Scanner registration belongs in `commonEnv.SCANNER_SCANNERS_JSON`. The chart has no public route and does not replace the existing homelab portal/scanservjs resources.

The separate [legacy eSCL fence candidate](fencing/legacy-escl-candidate.yaml)
is excluded from these examples and homelab GitOps. It passed installed-CRD and
server dry-run validation, but has not been applied or tested in the datapath.
It covers only the known legacy Pod transport; see the [handoff prerequisites](../docs/design/physical-writer-fencing.md).

Preprovision bind/PVC roots for the selected UID/GID (default 10001:10001) **as seen inside the containers**, mode 0700 (existing files: 0600). Rootless/user-namespace host IDs may differ; verify their mapping before setting host ownership.
The Helm pod omits fsGroup because recursive group permission changes conflict
with the application's private-store checks. Image directory ownership does not
repair an existing mount. Validate the actual storage driver before deployment.

Set Helm `runtimeIdentity.uid` / `runtimeIdentity.gid` to numeric values, or use
Compose `PUID` / `PGID` interpolation (for example `1000` / `100`). Both roles
use the same identity. Helm's schema requires integers in 1..4294967294;
Compose forwards the selected user string to Docker, so supply positive numeric
IDs there too. These Compose variables are not passed into the application and
do not make the bare image or executable switch users. For direct `docker run`,
use `--user UID:GID`; run the standalone binary as the chosen service account.
Existing volumes are not automatically chowned. Use the same identity for user
administration and preserve it across upgrades/restores; changing the setting
alone does not migrate existing ownership.

### First deployment: prepare storage

1. Choose the numeric runtime UID/GID and configure both roles before creating
   data. For rootless Docker or user namespaces, establish the corresponding
   host IDs first; container `10001` is not necessarily host `10001`.
2. Allocate two new, distinct dedicated directories or PVCs for state and
   documents. Keep the old scanservjs volume untouched. Use a filesystem whose
   SQLite locking and durability are supported; NFS is outside this contract.
3. With the application stopped, have the storage administrator set each new
   root to the selected identity as seen inside the container, mode `0700`.
   Do not recursively chown/chmod an existing data tree. Existing data needs a
   separate stopped-service ownership migration and recovery plan.
4. Exclude the documents volume from snapshots, replication and offsite backup
   **before writing the first document**, including inherited NAS policies.
   Prepare the separate state export/restore procedure described in
   [operations](../docs/design/operations-release.md).
5. Render the selected Compose/Helm configuration and verify distinct mounts,
   matching identities and the immutable image digest. For Compose, both host
   directories must already exist; for Helm, both claims must already exist.
6. Start with acquisition disabled (the default). Check both role logs for
   storage/permission failures, then create the simple-auth administrator, if
   used, as the same runtime UID. Confirm HTTPS login and API health. These
   checks do not establish worker/scanner readiness or storage fault tolerance.
7. Before enabling acquisition, accept the actual volume's locking, free-space
   behavior and recovery, and complete scanner fencing/readiness acceptance.
   A successful empty-store startup alone is insufficient.

Both mounts contain SQLite databases; a shared NFS document directory is not a
supported substitute for the local-store contract. See the
[homelab requirements check](../docs/design/homelab-readiness.md) for UID/GID,
NAS/backup, HTTPS, resources and upgrade acceptance gaps.

Both examples use a single application replica and separate API/worker process roles with bounded resources. Helm uses Recreate; stopping old work and reconciling hardware remains an application/operator requirement. Initial image/resources/storage/OIDC behavior must be validated before release. See `docs/design/operations-release.md`.


Native scanner settings in workerEnv/Compose worker environment now map to the worker-only tool loader and guarded Session constructor: SCANNER_SCANIMAGE_PATH, SCANNER_SANE_PARAMETERS_PATH and SCANNER_SANE_LIBRARY_DIRS_JSON. Loading settings performs no native/device IO. The constructor owns private per-device files. Worker startup invokes it only with SCANNER_ACQUISITION_ENABLED=true (strict true/false, default false); startup recovery still runs first. Enable only after the previous scanner writer has been stopped. The flag is not hardware fencing or readiness, and worker dispatch is connected for boot-pinned admitted jobs, while trusted physical readiness production and physical acceptance remain pending. Shutdown drains/closes the session before releasing the boot guard. The helper executable and official libsane/airscan still require final image packaging/acceptance. These examples do not advertise a ready scanner or provide deployment evidence.


The canonical API selector is SCANNER_AUTHENTICATE_TYPE=oidc|simple. Do not also supply legacy SCANNER_AUTH_MODE; simultaneous keys fail startup. The examples pass the selector and separate login rate/burst bounds only to the API. Simple mode ignores OIDC settings and requires operator-provisioned local credentials; user commands must run as the application OS user. Canonical none is not enabled yet; legacy guest retains its original isolation only through the legacy key. See docs/design/simple-authentication.md.

`SCANNER_SCAN_CONCURRENCY` is worker-only (default 1, range 1–32). Effective native
scan tasks are capped by registered device count and the global/job reservation
byte ratio. It does not initialize readiness, fence legacy writers or prove RSS
headroom. The daemon supports concurrent boot-pinned scans on different devices;
OCR/PDF rendering still waits for scans and remains single-concurrency. Verify
actual combined memory/disk/native-process load before enabling a larger value.


Worker examples now set GOMEMLIMIT=1024MiB (Compose override: GOMEMLIMIT;
Helm override: workerEnv.GOMEMLIMIT). Supply it only to the worker environment.
This standard Go runtime setting is a soft budget, separate from the1536MiB
container limit and native/OCR child allocations. Local synthetic measurements
are documented in docs/design/resource-measurement.md; container/native/OOM and
production resource acceptance remain incomplete. No deployment was performed.

Optional positioned LLM PDF support uses `SCANNER_OCR_LLM_TEXT_LAYER_FONT`, an absolute clean path to a worker-readable, license-approved TrueType font. Empty means disabled. The metadata loader performs no font I/O; worker initialization reads only regular files up to 32 MiB, without blocking on FIFOs, and enforces SCANNER_OCR_MAX_RESULT_BYTES on rendered layers. A configured invalid font fails OCR initialization without fallback. Provision a read-only font file separately and review its embedding/redistribution conditions; this repository does not install or redistribute a production font. The font digest and renderer version are pinned in model identity. Capabilities reach the API through the worker catalogue; a path alone does not advertise accepted OCR quality.


Kubernetes標準のDeployment・Service・既存PVC・HTTP Probeを使用する（Knative Servingは不要）。実行ファイルはimageのexec形式 `ENTRYPOINT ["/usr/local/bin/scanner"]` に任せ、Helmは `args: [serve]` / `[worker]` のみ指定する。Composeの `command` もCMDの引数置換なのでENTRYPOINTは維持される。

各containerはread-only rootと専用 `/tmp`（Helmは512Mi emptyDir、Composeは512MiBのexec可能tmpfs）を持ち、`XDG_CACHE_HOME=/tmp` で単一ファイルのnative runtimeを展開する。展開先には実行権限が必要。APIには最大約300秒のstartup probe猶予を設けるが、実環境の起動時間受入は別途必要。health probeはAPI/DB状態のみを示し、workerや実機scannerのreadiness証明には使わない。worker用の偽のHTTP probeは設けない。

HelmとComposeの既定値は同梱scanner/OCRツールのパスを上書きしない。必要な場合のみHelmのworkerEnv、またはComposeを起動する環境／env-fileへ明示する。Composeの値なしenvironmentは未設定ならcontainer環境から除外し、明示した空文字はそのまま渡す（不正な設定は起動時に拒否）。`.env.example`のツールパスも任意のoverrideとしてコメント化している。単一ファイル＋distrolessを配布方針とする。root `Dockerfile` をcanonicalとし、`deploy/standalone` は同じ定義への互換symlink。旧Debian候補は `deploy/debian` へ隔離した。Alpineは試作native依存の取得元で、採用container baseではない。

ローカルrender検証: `python3 scripts/test_helm_native.py`（HelmとPyYAML）、`python3 scripts/test_compose_native.py`（Docker Compose v2）。cluster／Docker daemon接続なし。

The earlier sane-airscan payload failed native TLS peer authentication. The current
unreleased onefile includes a verifying fork and bundled CA roots, with isolated
wrapper tests, including 26 direct peer/trust and 14 redirect cases in the
updated distroless candidate. HTTPS downgrade is refused; real certificate
provisioning and deployment acceptance remain pending.
An HTTPS URL or successful Go status probe alone does not establish native TLS
authentication. See [native TLS boundary](../docs/design/native-tls.md).

The `deployment-contracts` source CI job runs the render suite, including Helm
lint and rejection of missing inputs, mutable image digests and shared PVC names.
It uses Helm 3.21.1 with a checked archive SHA256 and hash-locked PyYAML 6.0.3 on
CPython 3.12/Linux x86_64. The lock in `scripts/requirements-deployment.txt` is
specific to that interpreter ABI/platform; review a new wheel hash before using
another platform. These checks neither contact a cluster nor execute the native
scanner. Hosted workflow execution and deployment acceptance remain pending.

An administrator may set `SSL_CERT_FILE` explicitly for the worker when using
the TLS-verifying native backend. Helm's `workerEnv` map can carry that path;
Compose users can add it to the worker environment using an override. Keep the
value absent unless a PEM file is mounted/provided; do not forward an empty
string or the full host environment. The application passes only this selected
trust setting into native processes. The current experimental verifying fork and bundled CA do not establish
production deployment or complete security acceptance.

Custom identity local smoke: the current unreleased distroless candidate also
ran as `1000:100` with fresh private tmpfs stores/cache, no network or host
mounts, read-only root, dropped capabilities and a 768MiB/no-swap limit. API
stayed running while `version`, stdin user creation and `worker --once`
succeeded, then stopped normally. This verifies those fresh-store operations;
it does not verify real PVC ownership, rootless ID maps, NAS ACLs, HTTP readiness
or OCR at the alternate identity. Default identity and all private-store guards
remain unchanged.
