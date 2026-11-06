# Optional Tesseract API v1

The application keeps the `tesseract` OCR plugin ID. A configured endpoint selects
its HTTP adapter; an absent endpoint retains the existing local adapter during
migration. There is no fallback between them. The LLM plugin connects directly to
LiteLLM or its configured Chat Completions endpoint and needs no extra adapter API.

The terminal service executable is `scanner-tesseract-api serve`. It uses the
existing native Tesseract provider, process supervision and dictionary fingerprint.
It opens no application database and receives no session, owner, document/job ID,
artifact ID, file path or image URL. Requests carry one JPEG/PNG bitmap and OCR
settings. Inputs and text live only in a private service workspace, not logs.

## Application configuration

Enable `tesseract` with the existing `SCANNER_OCR_PLUGINS` and default selection.
Set these only in the worker:

| Variable | Meaning |
| --- | --- |
| `SCANNER_OCR_TESSERACT_ENDPOINT` | HTTPS origin, without credentials, query or path |
| `SCANNER_OCR_TESSERACT_API_KEY` | Shared random printable-ASCII secret, 32–256 bytes |
| `SCANNER_OCR_TESSERACT_DESTINATION` | User-facing destination label; default `Tesseract OCR service` |
| `SCANNER_OCR_TESSERACT_ALLOW_INSECURE_HTTP` | Default `false`; opt in only for the intended private HTTP deployment |

Static configuration never loads the key or performs network I/O. Existing lazy
optional-OCR initialization fetches authenticated metadata with a deadline of at
most five seconds. Failure leaves ordinary scan/PDF processing available and the
catalog retries initialization. Successful initialization pins the dictionary
revision for the worker's lifetime. Updating dictionaries requires restarting the
worker to refresh that selection; mismatched in-flight revisions fail explicitly.
Do not mutate dictionaries inside a running image.

Service image builds must link the verified native partition manifest digest into
`buildinfo.NativeRuntimeSHA256`. The provider combines a linked digest with the service
source revision before fingerprinting the executable and dictionaries. A shared
library or service-only rebuild therefore invalidates an old selection too. The
service's `version` command exposes the linked `native_runtime_sha256`; source-only
development without that linked value retains the legacy local fingerprint. The
container builder must verify the linked digest against the actual image payload.

The application's model revision additionally hashes the endpoint and destination
label, following the LLM adapter's configuration-pinning pattern. A queued job
cannot move to a newly configured service or disclosure label. Key rotation alone
does not change the selection. The wire protocol uses the service's native revision;
the adapter maps the result back to the application-bound revision.

Remote capabilities are marked as external processing and carry the configured
destination label. The UI uses its existing capability-driven OCR controls. A
service outage never triggers an automatic second OCR call or a model fallback.

## HTTP contract

All responses use `Cache-Control: no-store` and `X-Content-Type-Options: nosniff`.
Every `/v1/` call requires `Authorization: Bearer <key>`. No cookies or CORS flow
are used. Authentication is checked before reading the image. The client follows
no redirects and uses no ambient proxy. Network policies/TLS remain deployment
responsibilities; possession of the key grants OCR-service access, not document
access in the application.

- `GET /v1/capabilities`: the `ocrplugin.Capabilities` JSON for the installed
  Tesseract model, with `api_version: 1`, provider ID `tesseract`, model ID
  `installed`, fingerprinted model version, languages, presets and output kinds.
  Metadata is bounded to 64 KiB in the client.
- `POST /v1/recognize`: raw bitmap body, `Content-Type: application/octet-stream`.
  `X-OCR-Request` holds a JSON object of at most 8192 bytes, with `model_version`,
  `languages`, `quality_preset`, `output`, `width`, `height` and `dpi`. Unknown
  fields and trailing JSON are rejected. The model version must match metadata.
  Geometry, languages, preset, output, input bytes and pixels are validated before
  the engine starts. Client and service stream the image, including chunked bodies.
- Success is HTTP 200 with the exact `X-OCR-Model-Version`, known Content-Length
  and either `text/plain; charset=utf-8` or `application/pdf` for the image-free
  `pdf_text_layer`. This is not a complete searchable PDF. The client checks
  revision, MIME, byte limit, UTF-8/PDF framing and commits through job-scoped
  output access. The application retains its existing final PDF validation and
  atomic publication authority.
- Failure statuses: 401 unauthorized, 409 model revision mismatch, 413 byte limit,
  422 invalid request/image, 429 already processing, 503 engine/storage/stop failure.
  Error bodies never include native stderr, endpoint secrets or recognized text.
- `GET /health/live` and `/health/ready`: unauthenticated minimal health, with no
  metadata. An uncertain native stop/cleanup makes both return 503.

Calls are sequential within an application job. The service processes one request
at a time and rejects contention instead of queuing it. It returns success only
after the native process has stopped and the private workspace has been removed
and synced. An uncertain stop retains the workspace, poisons the service and
prevents another request; the container must be stopped completely before restart
or cleanup. Startup refuses nonempty native/request work directories; reset only
the owned ephemeral workspace after stopping the complete prior container. A
client disconnect ends the local request but is not itself proof
that remote execution has stopped. The service owns cancellation and its own
bounded timeout/scratch resources, independent of application storage reservations.

## Service configuration and limits

The service uses `SCANNER_OCR_TESSERACT_API_KEY`, local `PATH`/`DATA_DIR` settings
from the existing Tesseract configuration, and the following service-only options:

| Suffix after `SCANNER_OCR_TESSERACT_` | Default |
| --- | --- |
| `LISTEN_ADDR` | `:8090` |
| `WORK_DIR` | `/tmp/ocr` |
| `MAX_INPUT_BYTES` | 209715200 (200 MiB) |
| `MAX_RESULT_BYTES` | 16777216 (16 MiB) |
| `MAX_PAGE_PIXELS` | 40000000 |
| `TIMEOUT_SECONDS` | 180 |
| `STOP_GRACE_SECONDS` | 2 |

All work directories must be private (0700). No endpoint may be configured in the
terminal service. API v1 bounds each result to at most 64 MiB; the worker applies
the stricter application budget. Input streaming preserves the existing 200 MiB
default instead of importing the LLM adapter's 64 MiB data-URL limit. Disk space,
memory, CPU and PID limits still belong to the container. Native staging can hold
two input copies plus output/diagnostics; size the bounded scratch volume for the
configured limits. These checks are not a filesystem quota or an OCR accuracy
guarantee.

## Delivery status

The HTTP adapter/service source is being implemented under plan 43-09. Existing
published images still bundle Tesseract. Independent service/core images, source
inventories, deployment examples and real-container acceptance are not complete.
Do not treat source-level fixture tests as proof of the final container split or
Japanese/English recognition quality.
