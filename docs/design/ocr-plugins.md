# OCR plugin contract v1

The versioned Go port is `pkg/ocrplugin`. It is implementation-independent: document/job services pass immutable selections and job-scoped artifact access, not DOM state, translated labels, filesystem paths, or arbitrary URLs.

## Registration and selection

The initial implementations are Tesseract and LLM adapters. Additional compiled providers register through the same interface. Adding a protocol adapter does not require editing document services or UI choices; capabilities drive available models/languages/presets. A configured protocol may have multiple profiles/models. Unknown protocols still require an adapter implementation: environment variables are configuration, not executable code or a universal protocol translator.

The registry snapshots versioned metadata in configuration order and returns isolated copies. It rejects incompatible plugin API versions and duplicate IDs. Capability discovery is local metadata; optional remote health is checked separately so a remote OCR outage does not prevent a basic scan without OCR. Misconfigured enabled providers fail validation; transient runtime unavailability affects that OCR selection without silently selecting another model.

Per deployment, environment variables select enabled plugins, defaults, model/profile metadata, endpoints, secrets, limits and protocol adapters. No plugin configuration file or normal-user endpoint is required. Do not download or execute plugin code from a user-provided URL. Go shared-object ABI loading is not the extension contract.

## Immutable request

A job records provider/plugin version, model ID/version, language IDs, preset, output kind, input revision, oriented page dimensions/DPI, and configuration revision. UI locale is absent from this request. Changing settings creates a new output revision; an active job retains its original selection.

`Registry.Select` checks exact version and requested capability, supported unique languages, preset, output, page count, and pixel limits. It does not implement a fallback. Application limits can be stricter and are also enforced before admission and while writing.

## Artifact and geometry boundary

`Access` belongs to one job. `OpenInput` accepts only this job’s listed opaque input IDs. `CreateOutput` assigns output IDs and enforces byte limits; plugins cannot choose filesystem names. Providers return artifact references. OCR text belongs in expiring artifacts, never session data, job configuration JSON, stdout logging, or metrics.

Positioned text is UTF-8 JSON with page ID, bitmap width/height, ordered spans and pixel boxes measured from the top-left of the supplied oriented bitmap. Unknown confidence is omitted, not fabricated. Model-specific scales must be normalized explicitly; confidence values are not a calibrated cross-model accuracy score.

`ValidatePositionedPage` rejects a mismatched page/dimensions, non-finite or out-of-bounds boxes, invalid UTF-8, and invalid confidence. The worker must additionally bound decoded bytes, span count and text length, validate the exact result page set and actual model version, and read artifacts only through its scoped access. These shape checks do not establish transcription accuracy or reading order quality.

Before publishing a searchable PDF, verify page count/dimensions/orientation, renderability, and extracted text against Japanese/English acceptance fixtures. Keep source images. Never label a Markdown/plain-text result as searchable PDF. A direct-PDF provider is validated too; model output is not trusted as an executable document-processing command.

## Cancellation and failure

Calls receive a deadline/cancellation context. Local adapters use owned process groups and bounded output; HTTP adapters use fixed, validated endpoints and bounded response bodies. API keys are injected server-side and are never returned in capabilities or errors. Cancellation waits for actual process/network completion and workspace cleanup before the job is reported cancelled.

Provider failure leaves the originals available for another explicitly requested output. No external request is made solely by opening a screen. Tests of the contract use artificial providers and synthetic text, not a real OCR model or external account.

## Separate PDF text layers

An engine can natively return `pdf_text_layer`: an image-free, invisible-text PDF with the same oriented page geometry. The coordinator combines it with the separately encoded original bitmap using the PDF assembler. This keeps JPEG/lossless image encoding independent from OCR. It is not a complete downloadable document until validated composition is finished. Direct `searchable_pdf` remains a separate output capability.

## Implemented configuration and Tesseract adapter

`config.LoadOCR` accepts a registry of compiled plugin loaders. It validates the
enabled list, explicit default and language syntax, preserves configuration order,
and calls only enabled loaders. Loader failures expose a stable plugin ID, never
raw provider errors. `config.LoadTesseract` validates absolute executable/data paths.
Adding a loader does not edit the shared selection parser. Runtime composition must
still intersect default languages with installed model capabilities. LLM profiles and the Chat Completions adapter are implemented as described below;
startup composition remains pending and no universal protocol is assumed.

`internal/ocr/tesseract` implements the OCR port on Linux. It discovers installed
configured dictionaries (ja→jpn and en→eng by default) and fingerprints the binary
and dictionaries, including required `osd.traineddata`, as the model revision. Deployment must keep these dependencies
immutable. Capabilities are copied; callers cannot mutate them. It supports `text`
and `pdf_text_layer`, auto layout with orientation detection (PSM1) and a block preset (PSM6). It does not claim
to return a complete searchable PDF.

Inputs are copied only through the supplied job access, byte-bounded and checked
against the immutable bitmap dimensions. Each call uses a private workspace and
sequential pages, an overall deadline, explicit environment, and process-group
supervision. Native stderr stays private. An output/stderr growth watchdog stops
work over budget; it permits overshoot between observations and is not a filesystem
quota. The coordinator still needs global disk reservations, disk-full handling
and deployment storage limits. Result copies are bounded, originals unchanged.
Uncertain process-group cleanup returns `ErrReconciliation` and preserves staging
for reconciliation; it never claims successful cleanup. Output references remain
unpublished until the application validates and commits them.

`python3 scripts/check-tesseract.py --help` describes the opt-in real CLI test.
It creates a clean synthetic 300dpi Japanese/English page, runs both output modes
through the product adapter, verifies text extraction, and records source/tool
hashes. The regular Go suite uses synthetic local tool fixtures and skips the real
CLI test when its explicit fixture variables are absent. No user document, live
scanner, remote LLM, final PDF assembler or product acceptance is covered.


## LLM profile and runtime boundary

`config.LoadLLM` loads the enabled provider's profiles from
`SCANNER_OCR_LLM_PROFILES_JSON`. Each profile declares protocol, full endpoint URL,
request model, exact expected response model, deployment version, supported
languages/output kinds and token limit. `token_parameter` explicitly selects
`max_completion_tokens` or `max_tokens` for the configured endpoint; there is no
retry with alternate parameters. HTTP requires `allow_insecure_http: true`.
URLs cannot contain userinfo, query parameters or fragments. Profiles are
non-secret; `api_key_env` may reference only `SCANNER_OCR_LLM_KEY_*`. The API role
can load this metadata without keys; `ResolveLLMCredentials` is worker-only.
Credentials must never appear literally in profile JSON or endpoint paths.

`internal/ocr/llm` sends one oriented bitmap per request, sequentially, through
job-scoped input access. It validates JPEG/PNG dimensions and byte bounds first.
No document IDs, filenames, arbitrary image URLs, tools or function definitions
are sent. The image is a data URL; the prompt asks for verbatim transcription and
treats document instructions as data. Requests disable streaming and storage.
The HTTP client uses no ambient proxy, cookies, retries or redirect following.
Provider policy/retention and actual model accuracy require separate acceptance;
the request fields and prompt cannot guarantee either.

Responses have bounded bodies, a request-wide deadline and cancellation. The
adapter requires one completed assistant result, the exact configured response
model, no refusal and no tool/function calls. Structured content rejects unknown
fields, missing geometry, excess spans/text and out-of-bounds boxes. Page identity
and dimensions come from the local immutable request. Unknown confidence is
omitted. Result text is artifact content only, never an error or log. No partial
or failed response is published, and no automatic fallback/retry incurs another
remote OCR call. Final publication still requires the coordinator's revision,
ownership, expiry and cancellation checks.

The standard race-enabled Go suite uses local HTTP fixtures for this adapter;
it proves the wire/error boundary, not a real model, endpoint compatibility,
position accuracy or final searchable-PDF assembly. See `.env.example` for an
explicitly disabled-by-default profile example.

Client cancellation confirms the local request has ended. A remote server may
continue computing after disconnection; this protocol offers no universal remote
job-cancel acknowledgement. This must not be described as proof of remote process
termination or a refund. Physical scanner reconciliation remains independent.

## Worker startup composition

`internal/ocr/runtime.Build` receives registered factories and configured settings,
constructs only enabled providers, checks their stable identities and capabilities,
and resolves an installed model supporting every default language. It rejects a
missing default or missing dictionary rather than substituting another provider.
Adding a compiled factory does not change document services. Construction failure
closes already-created instances in reverse order; normal Close is idempotent.

The Linux `NewWorker` composition root registers Tesseract and LLM loaders/factories,
uses configured job limits/deadlines and resolves LLM credentials in the worker
only. It rejects API-role configuration. `config.LoadWorker` never reads public
HTTP/auth/OIDC settings and requires no IdP secret. Constructors perform no remote
OCR calls. Tests cover a future factory, disabled plugins, lifecycle cleanup,
missing installed languages, worker-only keys and no remote startup probe.

This composes OCR components; it is not the executable worker loop. The API role
still needs a separate capability projection and must not call the worker builder
or receive LLM credentials. Job-scoped artifact access, publication fencing,
worker restart reconciliation and the application entry point remain pending.

Native providers can optionally use [scratch reservations](ocr-native-reservation.md) through the common port. Tesseract releases per-page staging only after confirmed stop and synced cleanup, and returns the shared reconciliation marker when that cannot be confirmed. The port does not claim a filesystem hard quota.

The LLM adapter now supports an optional local positioned-to-PDF renderer via constructor options. A validated caller-approved TrueType font enables PDF text layers only for positioned models and binds the font digest/renderer version into model identity. The remote endpoint still returns positioned JSON, never a trusted PDF. Worker font loading is opt-in via SCANNER_OCR_LLM_TEXT_LAYER_FONT; default configured LLM instances retain their existing capabilities. Deployed catalogue publication and live-model acceptance remain pending.

Optional positioned LLM PDF support uses `SCANNER_OCR_LLM_TEXT_LAYER_FONT`, an absolute clean path to a worker-readable, license-approved TrueType font. Empty means disabled. The metadata loader performs no font I/O; worker initialization reads only regular files up to 32 MiB, without blocking on FIFOs, and enforces SCANNER_OCR_MAX_RESULT_BYTES on rendered layers. A configured invalid font fails OCR initialization without fallback. Provision a read-only font file separately and review its embedding/redistribution conditions; this repository does not install or redistribute a production font. The font digest and renderer version are pinned in model identity. Capabilities reach the API through the worker catalogue; a path alone does not advertise accepted OCR quality.

External searchable-PDF models require a `processing_destination` public label. LLM profiles provide it as administrator-authored non-secret metadata; the profile hash pins label and actual endpoint configuration together. The application does not derive the label from a private URL or credential. The label describes the destination chosen by the administrator, not an independent certification of that service. Font-enabled LLM construction rejects a missing label, and the public capabilities carry it per model. The dialog names this destination before explicit PDF creation, in the selected UI language. Unavailable or undisclosed external metadata cannot enable OCR; ordinary PDF remains available. This adds destination disclosure, not a separate consent checkbox or automatic transmission on view open.

## Orientation detection and pinned selections

The `auto` preset now invokes Tesseract's orientation/script detection with
PSM1. `block` keeps PSM6. `osd.traineddata` is a required model dependency and
participates in the existing opaque `model_version` fingerprint. Missing OSD
fails provider construction; it does not silently run a different preset.
This changes recognition behavior for rotated horizontal originals while
preserving the HTTP fields, provider/model IDs and preset names.

Existing render jobs retain their pinned selection. A changed OSD model produces
a different model version; an old version is rejected before opening its input
or allocating output. There is no automatic migration, retry or provider fallback
for such a job. No new request parameter or HTTP API version is needed.

Direction detection applies to recognition, not source-file mutation or saved
page rotation. The renderer still overlays the text on the original bitmap and
then applies the user's existing rotation to both together. The corrected
physical-original review used the existing 90-degree page rotation. Its operator
confirmed that the corrected recognition was broadly readable; this is one
original's assessment, not a universal accuracy guarantee.
