# Product frontend integration

The product has a static Astro frontend in `web/`, served by Go. Astro 7.4.4 and its dependency lockfile follow the existing stack spike; Node is needed for building only. Current screens connect real sessions, document history/detail, source previews, rotation and native download links. The prototype's fake document model, fabricated scans, design tools and Paperless connector are excluded.

```sh
python3 scripts/build-web.py --output /tmp/scanner
```

This runs npm ci with lifecycle scripts disabled, builds Astro, validates public asset types before embedding, stages generated assets in `internal/webui/dist`, and builds Go with `-tags webui`. Generated directories are ignored by Git. `all:dist` is essential: default Go directory embedding excludes `_astro`, silently dropping the generated JavaScript. A tagged test checks those scripts are served. Backend-only builds retain root 404. Release builds must use the UI-enabled path and verify assets.

The static handler serves a bounded explicit asset map with configured Host and GET/HEAD checks, no directory listing, document-root access or arbitrary SPA fallback. Headers include no-store, nosniff, no-referrer and a self-only CSP without inline scripts, object content or framing. API/auth traffic uses the API token bucket; public assets retain concurrency bounds without consuming that bucket. Build validation rejects unreviewed asset types before they enter the binary.

English is the default, with Japanese support. Locale/theme preferences are the only localStorage data. Document and authorization state stays on the server. Client state is transient: current response/view, cursor and pending operation key. In-flight bootstrap coalescing prevents competing anonymous sessions; generation checks prevent stale response rendering. OIDC start consumes the JSON authorization target and navigates over HTTPS. Explicit reset appears after stale/conflicting authentication. Back/forward-cache restoration refreshes server state.

History uses real pagination. Review applies source-image rotation once, submits revision-bound edits, retains an operation key for retry and links to authorized downloads. Server/provider text is never interpolated as HTML. Labels and navigation/theme accessibility names have JA/EN catalogs. The approved logo, indigo accent and neutral dark styling are reused.

Home currently marks scanning unavailable. Scanner capabilities, scan admission and settings are not connected. [Render admission](render-http.md) now links review to the real worker/job/download flow. Review needs grid/single-page switching, reorder/delete controls and full processing states. OCR output settings are connected as described below. Whole-document deletion, thumbnails, capture settings, mobile file save/resume, keyboard/screen-reader acceptance and the complete scan workflow remain pending. This is partial product integration.

```sh
python3 scripts/check-web.py --playwright-module /path/to/playwright-core \
  --chromium /path/to/chrome --receipt /tmp/product-web-check.json
```

The checker builds the embedded binary and uses private SQLite plus a loopback TLS proxy. A test-only proxy endpoint seeds synthetic PNG/document records; no product seeding endpoint exists. Headless Chromium checks real guest login, history, PNG, revision-safe rotation, JA/EN, theme/locale persistence, 320/390/768/1440 widths, two-browser isolation, guest end, job polling/cancellation/reload/terminal stop with focus preservation, and JS/CSP errors. Only the fixture certificate error is ignored. Source and generated-asset hashes are recorded separately. Desktop and 390px screenshots were also inspected. This does not certify real scanner, OIDC browser login, production proxy, mobile hardware, screen reader or native download behavior.

Astro references: [static deployment](https://docs.astro.build/en/guides/deploy/) and [client scripts](https://docs.astro.build/en/guides/client-side-scripts/).


## Job screen

`#/job/{id}` reads the actual owned job projection. Active states poll every two seconds; unchanged state preserves the existing DOM and keyboard focus. Queued/running jobs expose cancellation with a stable request key. Cancelling remains distinct from cancelled and survives reload. Terminal states stop polling. Leaving the view or hiding the page cancels requests/timers; becoming visible reloads server state. Known 429/503 responses back off using bounded Retry-After, up to three retries, then offer manual reload. Unknown errors are not presented as completed work.

Render admission now navigates to the job screen; scan admission remains pending. Browser verification currently opens a seeded job deep link, submits real cancellation and verifies reservation retention at the fixture boundary. It injects a terminal database transition to test rendering and polling cessation; it does not prove a worker or scanner stopped. It also checks focus survives an unchanged poll, reload retains cancelling, and no further polling occurs beyond a normal interval after terminal status.


## Output dialog

Review now offers Output settings without expanding its background card. Document output selects JPEG or lossless PDF image encoding; photo output selects JPEG or PNG. Confirmation submits a new revision-bound render command and opens its durable job. Close, Escape and outside clicks dismiss an idle dialog. During submission, controls are fixed, Escape/backdrop dismissal is blocked, and a 15-second request timeout prevents an indefinite modal lock. An unknown failure retains the same key for the same command; changing the command obtains a new key. A response from a departed view cannot redirect the current view. No draft/output preferences are written to localStorage.

Browser verification checks unchanged background geometry, outside/Escape close, the admitted lossless setting, actual worker completion, and PNG photo export through the real browser download mechanism. Mobile Japanese dialog screenshots were inspected. OCR selection is now connected in the same output dialog; capture settings and device selection remain pending.


## OCR output selection

The PDF output dialog loads authenticated `/api/capabilities` and uses a separate `web/src/client/ocr.ts` component for draft OCR intent. OCR starts off; the configured worker preference fills model/language/preset controls without enabling processing. Models and quality options come from current public capabilities. Changing models keeps only supported selected languages; a model without a published preset default requires an explicit choice. Empty language selection prevents submission. Native labels use Intl.DisplayNames for language names; fixed strings and common quality labels support English/Japanese. External-processing models display a service-transfer notice near model selection.

The dialog keeps a fixed frame with an independently scrolling body and footer. Toggling OCR or loading capabilities does not resize the background or frame. It supports outside/Escape close, cancels pending capability reads when closed/navigated, and suppresses late results. Closing a former dialog cannot dispose a newer one. A failed catalog read disables unavailable OCR while ordinary PDF output remains usable. An already enabled unavailable selection is not silently converted to plain output. Processing submission freezes controls, times out after 15 seconds, preserves the same key for the same retried command and sends provider/model/languages/preset without client model-version authority. HTTP 422 reports unsupported settings; the server still enforces current capabilities and dimensions.

The browser checker adds unavailable-OCR/plain independence, configured preferences, language/quality requirements, external notice, stable frame, four viewport widths and a failed-then-retried OCR admission with exact settings pinned by the real API. Its model catalog is explicitly authored in the fixture database; the admitted OCR job remains queued and is not given fabricated successful output. This base fixture case does not prove provider execution or accuracy. An optional native scenario below independently connects real Tesseract through the browser; separate installed Tesseract checks remain available. Actual scan controls, production scanner publication, physical mobile/accessibility and full end-to-end release acceptance remain pending.


## Browser to native Tesseract verification

The browser checker can additionally run an actual worker daemon and two synthetic Japanese/English source pages. It reads the daemon's current catalog through the real API, selects OCR and lossless encoding in the output dialog, admits the command, waits for actual recognition/publication, and saves scan.pdf with Chromium's native download mechanism. No catalog, success transition or output PDF is injected for this native scenario. Captured document/page rows and source PNGs are test fixtures; acquisition is still outside this proof.

```sh
python3 scripts/check-web.py \
  --playwright-module /path/to/playwright-core \
  --chromium /path/to/chrome \
  --fixture-python /path/to/pillow-pikepdf-venv/bin/python \
  --tesseract-binary /path/to/tesseract \
  --tessdata /path/to/tessdata \
  --font /path/to/NotoSansJP.ttf \
  --receipt /tmp/scanner-product-web-check.json
```

The four fixture/tool options must be supplied together. The receipt records `native_ocr: null` without them; a passing base fixture run must not be read as native proof. With them, `native_ocr` records five browser checks, independent artifact checks, fixture/tool hashes and the exact selected model version. The fixture Python requires Pillow/pikepdf and `pdftotext` must be installed. Independent checks compare the browser download to the published artifact hash, extract Japanese and distinct English text per page, verify page order, original lossless pixels, A4 dimensions and rotations, and confirm original retention and released job/native resources. The worker exits cleanly and withdraws its catalog before the test API shuts down.

This scenario exposed a language-order bug: checkboxes followed the model's advertised ordering rather than the configured preference. Controls now preserve the preferred/previously selected language ordering and append remaining languages afterward. The native command is verified as `ja,en`. Tesseract documents that output and processing time can depend on language order ([official command-line guide](https://tesseract-ocr.github.io/tessdoc/Command-Line-Usage)). This is a synthetic clean-page integration proof, not a recognition-quality benchmark, physical scanner, real mobile/IdP or deployment acceptance.

## Keyboard bypass navigation

The first Tab stop is a bilingual skip-to-content link. It becomes visible on
focus and moves focus to the programmatically focusable main landmark without
changing the hash route, so a document or history view is preserved. It uses
the existing theme colors and a visible focus outline. `check-web-keyboard.cjs`
checks session, home and history in English/Japanese at widths 320 and 1440,
including locale changes and continued Tab navigation inside session content.
The shared `check-web.py` runner includes this check with mocked GET responses;
this does not replace real screen-reader or mobile hardware acceptance.


### Direct PDF import and stale-session recovery

The home Import PDF action opens the native file picker in the original click
handler. Cancel leaves the current view unchanged. A selected PDF uses the same
bounded upload/digest/idempotency controller as the retained direct `/import`
route, without a second submit action. Known rejection exposes the file/limit
controls; an uncertain response keeps the original file, digest and request key
for explicit reconciliation. The initiating import job opens the existing
page editor after success; reopening other jobs never triggers that navigation.

Session bootstrap marks unusable cookies with optional `reset_required:true`
without replacing them in a late GET response. The frontend performs one
same-origin `POST /auth/session/reset` and one fresh bootstrap. This clears only
the browser cookie, never a principal, document, or a login successor. A valid
concurrent login produces 409 and is loaded normally. Storage failure leaves the
cookie untouched; recovery does not loop or replay a business mutation.


### Optional OCR presentation

The output dialog offers searchable PDF from the sanitized worker capability
catalog. A successful empty plugin catalog hides OCR entirely; no OCR endpoint is
called just to create an ordinary PDF. Local Tesseract is valid without a URL.
Configured remote profiles disclose their processing destination before any
explicit OCR admission. Model/language/quality controls appear only after the
user enables searchable PDF. The dialog expands on that deliberate action and
keeps its frame stable while those detailed settings change. Metadata failure
explains that ordinary PDF remains selectable and offers an explicit retry;
missing remote destination metadata still fails closed for OCR.

## Preview and save flow

The document editor keeps one primary “Save PDF”/“Save image” action visible. It admits the existing revision-bound render intent only when a current output is absent; after successful processing it downloads that output without a separate Create/Open/Download sequence. A current output uses the same save label and skips rendering. Processing stays beside the existing preview; queued/running share a task description. Cancellation, uncertain requests, failed processing and explicit retry retain the original server job and idempotency rules.

The editor has one overview with always-visible, labeled icon actions for rotation, insert, replace, reorder and confirmed page deletion. Output options and confirmed document deletion share the document menu. Document titles use the creation date/time in the selected locale. History shows a compact first-page preview and direct delete icon; grid/list controls persist only the layout preference in localStorage. Switching layout reuses the loaded rows. Accepted document deletion returns directly to History. Failed imports return home with an allowlisted reason; failed empty documents leave History and retire through normal worker cleanup, preserving documents that already contain pages.

Prepared, authorized PDF pages render to canvas with PDF.js, loaded on demand. One worker decodes one visible/nearby page at a time; navigation destroys the page task. Fonts, CMaps, codecs and their notices are served from this origin, with no CDN or document upload to another service. The Node canvas package is optional tooling and is not a browser/runtime native dependency. The original prepared-PDF link remains available when rendering fails. Existing owner/expiry checks, raw-intake isolation, PDF output and server resource limits are unchanged. Public asset type checks now also cover the worker and narrowly scoped PDF support files, under the existing file/count/total-size bounds.
