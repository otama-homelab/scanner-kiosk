# Review page editing

The embedded client displays original-page previews with metadata rotation and
localized page labels. Rotate, Move earlier, Move later and Delete page use the
existing owner-scoped, revision-checked edit API. Movement sends the complete
ordered page-ID list; the server installs contiguous ordinals atomically.
Boundary movement controls and all page controls during an active job are disabled.

Deletion requires a modal confirmation, initially focused on Cancel. Escape or an
outside click closes it without mutation. Removing the final page leaves an empty
review document available for another capture. Edits invalidate current exports;
the client reloads the document rather than maintaining a second page-order model.
Each displayed command keeps an idempotency key across a failed retry. A local
edit gate prevents overlapping page commands. Late responses after navigation do
not navigate or redraw the replacement view. Cross-tab conflicts remain enforced
by the server revision and require reload.

Browser acceptance uses a private SQLite fixture and the real embedded API through
a local HTTPS proxy: two-page movement in both directions, persistence after
reload, deletion, cancellation of confirmation, final-page deletion and removal
of the now-obsolete download link. The fixture proxy explicitly forwards PUT and
DELETE. Responsive checks cover 320/390/768/1440 widths. This proves metadata
editing, not physical capture, file garbage collection or thumbnail generation.
The presentation-only `reviewViews` module adds Overview and Page view. Selecting
an overview image opens that page; previous/next controls and an image strip
navigate in current server order. End controls are disabled. Mode controls expose
aria-pressed, image controls have localized page names, and hidden cards are
removed from keyboard navigation by the hidden attribute. View changes issue no
edit requests. Selection is scoped to this mounted view and is discarded when the
server projection reloads; it does not duplicate document state or persist scans
in browser storage. The strip and overview now prefer private thumbnail URLs with one-time original
preview fallback; Page view reads the original. See [worker thumbnails](thumbnails.md)
for generation timing and remaining capture/backfill/GC work.
