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
its unpublished output; the uploaded source remains. Unknown child stop, filesystem
or commit outcomes retain the reservation for reconciliation. Publication and failed
stop acknowledgements have exact job/owner/runner read-back checks. The startup
recovery file bound now matches the existing 10,000-output scope/page cap.

Unstarted import cancellation/expiry, offline producer/worker recovery, PDF render
and preview integration, upload UI and actual hosted standalone candidate acceptance
remain pending in plan 43-08. No new
service, runtime image, upstream fork or dependency is introduced here.
