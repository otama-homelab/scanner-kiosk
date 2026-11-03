# Ordinary PDF render coordinator

`internal/renderjob` connects claimed DB work to actual image-PDF generation. It is a synchronous worker component, not an executable worker or HTTP service. Its record port is implemented by `documents.Store`.

`Prepare` reloads current claimed metadata, selects PDF/image rendering or the configured OCR text-layer pipeline, caps processing context by the job deadline, and limits output to both policy and admitted reservation. It constructs the scope allowlist from DB records. Image headers are read through verified private inputs with a one-MiB header bound; format must match stored media type. Actual dimensions then go to the PDF assembler's pixel bounds and full bitmap validation. The ordered snapshot controls paper size, color, rotation and compression. The final output is closed, synced and sealed.

The resulting `Prepared` object owns the unpublished scope. `Publish` runs conditional DB publication. Confirmed success transfers file lifetime to document cleanup. Confirmed rejection removes generated data and preserves originals. An uncertain commit triggers bounded read-back independent of the cancelled processing context:

- Recorded success retains files without repeating publication.
- Confirmed absence removes files and returns the original publication error.
- Unavailable read-back returns `ErrPending`, retains the scope, and prevents `Close` from deleting it. The caller must keep this object and retry `Publish`, which repeats only read-back.

`Close` can discard work before publication; cleanup errors are visible and retryable. This object must not be serialized or returned to the browser. No scope is released to an invented durable recovery mechanism. Worker crash/startup reconciliation remains required: unreferenced workspaces must not be deleted before old workers are stopped and durable publication references checked.

## Evidence and remaining work

Race-enabled tests run with actual SQLite state/document databases and a real synthetic PNG. They cover preparation → discovered geometry → PDF → seal → publication → owned lookup, cancellation before publication, unchanged originals, corrupt inputs, pixel bounds, abandoned work, and unsupported OCR rejection when no selector is supplied. A record-port wrapper injects committed-but-error, rollback and unavailable-read-back outcomes; it is fault injection, not a hardware/database outage test.

`PrepareWatched` now adds [DB cancellation monitoring](render-monitor.md) during generation. Executable role startup and render dispatch now exist, including the [OCR worker pipeline](ocr-worker-pipeline.md). Process isolation/resource acceptance, automatic startup/native reconciliation, scanner orchestration, authenticated endpoints and TTL cleanup remain unfinished. Failed preparation/publication does not by itself terminalize a DB job or release its reservation. For a completed `Prepared`, `Finish` now connects confirmed unpublished cleanup to [render completion](render-completion.md) and reservation release. `ExecuteClaimed` now consumes preparation cleanup evidence to complete ordinary failure paths; see [executor](render-executor.md). Automatically retrying rendering is not implied.
