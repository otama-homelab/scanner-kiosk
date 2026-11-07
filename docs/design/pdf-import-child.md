# Bounded PDF preparation child

`pdfimport.Prepare` launches the current application executable as the private
`__pdf-import` command. The command is handled before service configuration,
sessions, databases or worker/native setup. It uses the existing process-group
supervisor, file-backed stdin/stdout, an explicit environment containing only Go
resource settings, and no inherited API/provider credentials.

The caller supplies a sealed regular input file and a distinct, empty private
scratch output file under its existing storage reservation. Preparation never
truncates either file. The parser runs only in the child. The child rejects
unsupported active/encrypted/external PDF content through `pdfimport.Split`.
Ordinary absolute HTTP(S) hyperlinks are preserved for explicit user clicks;
validation never fetches their targets. Browser-printed `javascript:` Link
annotation click actions are discarded without changing page content. File/data
URLs, URL credentials, automatic actions and dangerous chained actions remain
rejected. Actual JavaScript actions are never admitted.

Before reading untrusted input the child installs Linux limits: 512 MiB data
segment/mapped anonymous allocation, 2 GiB virtual address space, 30 seconds CPU
(31 hard), 32 descriptors, no core dumps, and a file-size cap equal to the bounded
result payload plus framing. Go's soft memory target is 256 MiB with two runnable
processors and a maximum of 16 runtime threads. The parent enforces a 30-second
wall deadline and terminates/reaps the owned process group on cancellation.
No-new-privileges and non-dumpable mode are required. This is a resource boundary,
not a filesystem or network namespace sandbox; no path/URL from PDF input is used
as an I/O capability and the stateless parser rejects external references.

The binary result has a fixed header and bounded page count, then each standalone
PDF's dimensions, byte length, SHA-256 and content. The parent validates count, dimensions,
lengths, total bytes, PDF prefix, offsets and complete framing without parsing PDF
content. It returns file sections, rather than loading every prepared page into
its heap. The caller can copy each section into its existing artifact publication
scope and seal it normally. Partial, malformed or failed results return no pages.

`ErrUnstopped` means the supervisor could not confirm the complete child group
has stopped. The caller must retain job/reservation authority for reconciliation.
Other failures still require normal unpublished-scratch cleanup, and never imply
successful import publication. Neither the helper nor its caller extends TTL,
creates a document, registers artifacts or retries physical scans.

Isolated tests cover successful same-executable splitting, unchanged original,
private result sections, absence of a parent environment sentinel, malformed and
unsafe input, page limits, cancellation, output alias/nonempty-file refusal,
corrupt result framing and actual kernel rejection of a 600 MiB anonymous mapping
under the child memory limit. The CLI dispatch test verifies service stores are
not created. Existing process-group cancellation/escalation tests remain in use.
The test helper now waits on a timer rather than an empty select, so Go's deadlock
detector cannot make it exit before cancellation is exercised.

The worker now claims sealed imports, verifies the raw input descriptor, runs the
child and copies bounded result sections into the existing artifact scope. Each
sealed page must match the child's size and SHA-256 before atomic publication.
The remaining reservation covers both the framed scratch result and page copies:
payload is at most half the reservation after framing overhead. Scratch is removed
and its directory synced before publication. A known failed execution removes only
its unpublished output. Failed empty documents are hidden from History immediately
and queued for automatic document cleanup through the existing producer-stop
barrier, which also removes the uploaded source. A failed operation on a document
with existing pages preserves those pages. Unknown child stop, filesystem
or commit outcomes retain the reservation for reconciliation. Publication and failed
stop acknowledgements have exact job/owner/runner read-back checks. The startup
recovery file bound now matches the existing 10,000-output scope/page cap.

Unstarted import cancellation/expiry and offline producer/worker recovery are now
connected to the existing maintenance and administrative recovery paths.

## PDF output child

Plain and OCR PDF output now use the internal `__pdf-assemble` command of the same
executable. `processrun.ApplyPDFLimits` keeps import splitting at 512 MiB data,
2 GiB virtual address space and a 256 MiB Go target. Assembly uses 1280 MiB data,
3 GiB virtual address space and a 768 MiB Go target because admitted 600dpi color
pages require simultaneous bitmap and PDF buffers. Virtual address space is not
a resident RAM requirement. Both fixed profiles retain the 30-second CPU limit,
16-thread limit, file/descriptor bounds and parent deadline. No service
configuration is loaded by either command.
The child starts in the private document root with an explicit credential-free
environment. A private, bounded (16 MiB) file manifest contains the immutable
ordered snapshot, relative input capabilities (path/size/SHA-256), output limits
and optional OCR-layer capabilities. The child opens only these confined inputs
through `artifactread`, verifying the opened files before parsing. PDF paths and
external references never become arbitrary filesystem/network open requests.

The parent accounts for manifest bytes and one scope-owned bounded output against
its remaining job reservation. The child writes directly to that regular file;
after the entire process group is confirmed stopped, the scope verifies its
identity/size/hash, syncs/closes it and renames the same inode in place. There is
no second PDF copy that halves the usable output capacity. Originals and OCR
layers are read in place. The full output bound remains charged while production
is active; unused credit is refunded only after verified finalization. Failed
finalization retains its charge until confirmed removal.

Preparation scratch is closed/removed and its parent synced before the final
scope is sealed and published. Unknown child stop retains the output, manifest,
job and reservation for administrative reconciliation; scope cleanup refuses a
pending file-backed producer. The parent hashes bounded bytes without parsing
the generated PDF. Existing job cancellation/revision/owner/publication guards
remain in the coordinator.

Imported pages retain their text/vector/geometry and rotation. OCR requests and
image thumbnails include scanned images only; imported PDF-only output does not
invoke an OCR provider even if the pinned settings enabled OCR. The existing
image thumbnail path does not pretend a PDF is a raster input. Safe PDF browser
preview remains separate pending work.

Source tests cover mixed page output, preserved text/vector, input digest refusal,
credential-env isolation, budget/cancellation, scratch cleanup, mixed OCR with
only image pages passed to the provider, actual worker import -> PDF output, and
both private CLI commands bypassing service stores. Existing kernel memory-bound
and process-group tests cover the shared limit/supervision code. Six related full
suites passed; hosted executable acceptance is still pending.

Browser upload and prepared-page preview links are now connected (see
`pdf-import-http.md`). Typed parser failures now persist through the stopped-job
transaction to allowlisted bilingual job messages. Actual native browser PDF
viewing and hosted standalone candidate acceptance remain pending in plan 43-08. No new
service, runtime image, upstream fork or dependency is introduced here.
