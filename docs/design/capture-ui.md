# Capture UI

The embedded Astro client now connects Home to the OCR-independent scanner list
and new-scan command. Document review links to a next-page capture route, which
reloads the document revision/purpose from the server before showing controls.
The physical readiness producer remains unfinished; a normal unprepared worker
still shows Not ready and cannot enable Scan.

`capture.ts` owns only unsubmitted preferences and bounded request/view lifecycle.
Scanner status, verified capture tuples, document ownership/revision, queued work
and publication remain authoritative on the server. No scanner/job/document state
or credentials are copied into localStorage/sessionStorage. Accepted commands
navigate to the existing owned server job URL; its polling survives reload.

## Interaction

One registered scanner is selected automatically. Multiple scanners require
explicit selection in a searchable native modal; no busy/removed device is silently
replaced. Choices retain server identity, status and complete capture tuples. A
late first metadata result may initialize a previously empty selection from a
verified default. A previously chosen unsupported tuple instead disables Scan and
requires explicit settings review.

Home always offers Document/Photo for a new document, independently of existing
history. Next-page mode fixes purpose to the server document. Photo append is
excluded once a page exists; server rules remain the final authority. Settings use
a fixed-size modal with scrolling content: paper size/color are coupled to verified
options, DPI is under Advanced and prefers the server's standard 300-dpi default,
and photo output can be JPEG/PNG. Summary labels show paper size, color, DPI and
output format separately. Applying a modal never starts capture. Outside click,
Escape and keyboard-native selection close/operate the dialogs without nesting.

The client refreshes scanner metadata with GET only, pauses hidden views, aborts
superseded requests and disposes timers/dialogs on route/locale change. Background
refresh preserves focus and does not replace a known status with Loading every
five seconds. Errors invalidate availability; stale settings or non-idle status
cannot enable the primary action. POST always revalidates independently.

Scan submits only an explicit user action, one pending request at a time, with a
stable idempotency key for the unchanged intent. New commands contain scanner,
purpose, capture and optional photo format; next commands exclude purpose/output (including empty photo retries, whose
format is fixed to the server preference) and carry the fetched revision in If-Match. Known rejection can be retried only
explicitly. Unknown network/storage outcome disables re-submission in that view
and directs the user to History; it is never automatically replayed. Leaving the
view aborts browser work without claiming server cancellation or late navigation.

## Server-owned photo preference

Document projection now exposes nullable `photo_format`: latest accepted scan or
render JPEG/PNG intent, with legacy photos defaulting to JPEG and documents null.
It is selected within the existing projection query, not retained in the screen.
The normal Save image action and output modal use this value so a PNG scan is not
silently converted to JPEG after navigation/reload. Ownership/retention filtering
is unchanged. The broader existing history-query audit remains unfinished.

## Layout and evidence

Desktop keeps aligned scanner/settings columns. Mobile uses compact cards and
structured settings; the primary action fits the tested 390×844 initial viewport.
Settings/advanced changes stay inside a fixed dialog frame. English is the default,
all new controls/messages have Japanese text, and no slogans/redundant format copy
or action logs are introduced.

`check-web-capture.cjs` runs the embedded authenticated frontend in Chromium with
mocked scanner/admission/job/document responses. It checks widths 320/390/768/1440,
explicit search/selection, busy/unknown refusal, outside dismissal, coupled choices,
DPI/advanced fixed height, explicit-only POST/key reuse on known rejection, server-job
navigation, PNG intent, uncertain-response lock/history guidance, locale disposal,
next-page revision/body, single-device selection, initial metadata arrival, persisted PNG empty-photo retry and mobile
primary placement. These are UI-contract fixtures, not hardware/readiness proof.
Existing browser cases additionally exercise real API sessions/history/output and
real worker OCR downloads with independent PDF text/pixel checks. Backend photo
HTTP tests verify JPEG/default/PNG intent persists in document projection.

Still pending: real ownership/readiness production and renewal, full native
API→worker→device/mobile acceptance, complete review editing/thumbnail/deletion,
physical restart recovery/cleanup, resource/IdP/accessibility
acceptance and release/deployment/cutover. This stage does not stop scanservjs or
make a real device ready automatically.

Final validation includes strict TypeScript 5.9.3 noEmit checking of all four
client modules, ten capture UI groups and the existing browser/native OCR suites.
The temporary compiler adds no repository dependency.

Document/history now discover active jobs from the server and link to existing
status without re-execution; see [active job/history](active-job-history.md).
