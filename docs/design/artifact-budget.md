# Shared stage output budget

`artifactscope.NewBudget` creates one in-process byte budget for the outputs of an admitted job. Pass the same object to `NewWithBudget` for OCR and final-output scopes. Each scope retains its own per-file/total/count limits; a write must fit both its local limits and the shared budget. The coordinator must derive the budget from its durable admitted reservation, not browser input.

Full write credit is reserved atomically before writing. Partial/unwritten bytes are refunded, while bytes actually written remain charged even if the writer fails. Concurrent scopes cannot spend the same remaining capacity. Seal closes/syncs metadata but does not refund files. Scope Close refunds its actual bytes only after successful removal and parent-directory sync; failed removal/sync retains the charge and can be retried. Release transfers cleanup responsibility and keeps the charge because files remain. The budget is discarded when the job lifecycle ends; it does not track later TTL deletion of published documents.

`Remaining` lets a sequential coordinator cap the final assembler while OCR intermediates remain on disk; the writer still enforces shared capacity if another stage changes usage concurrently. Scope-output refunds remain tied to actual scope deletion. [Native scratch leases](ocr-native-reservation.md) use a separate trusted adapter confirmation after process stop and workspace cleanup. Original inputs are not charged again here: they already belong to the document's stored-artifact usage and are read-only.

This accounts only for logical output bytes written through scope writers. It is not a filesystem/project quota, a filesystem-block/metadata bound, a durable reservation, memory accounting, or a native scratch bound. Native OCR now reserves planned staging bytes through a separate port; watchdog overshoot, physical disk probes/resource acceptance and executable-worker OCR wiring remain separate unfinished boundaries. A fresh in-process budget never authorizes reclaiming files or restarting jobs from a previous boot.

## Evidence

Race-enabled tests share budgets across separate roots/stages and check retained sealed intermediate charges, combined-limit rejection, concurrent write contention, refund after confirmed deletion, failed-cleanup retention/retry, and charge retention after Release. A closed root descriptor injects cleanup failure without production hooks.

The real two-page Tesseract integration now uses one budget for the sealed OCR scope and final PDF scope. It verifies their combined actual sizes, caps multi-page generation by the remaining budget, removes final output without refunding retained OCR layers, then removes layers and checks zero usage. Independent pixel/render/text checks still pass. This is a synthetic component chain, not a complete production OCR reservation/cleanup implementation.
