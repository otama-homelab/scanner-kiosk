---
version: 1
slug: "web-src-client-app-ts"
primary_target: "web/src/client/app.ts"
related_targets: ["web/src/client/ocr-results.ts","web/public/style.css"]
---

# Document review workspace

Mode: Operate. The user confirmed import → correction → OCR → save, and explicitly asks for professional modern craft, enough useful density, and low eye travel. This supersedes the unreleased v0.2.23 tab-only proposal. Keep the recognizable Scanner logo and accent; replace the document screen's composition and local chrome, not the product's functionality.

## Direction contract

THESIS: One stable original and one adjacent task area; correction precedes recognition and the save action stays in a predictable position. Large stacked cards, duplicate full-page images and full-width Save buttons are removed from this surface.

OWN-WORLD: Cool neutral canvas, white/dark working surfaces, one restrained Scanner accent, crisp dividers and compact rectangular controls. System sans at readable working sizes. Density comes from actual pages, OCR text, source state and editing controls rather than decorative metrics.

STORY: The owner imports a page, checks and corrects its framing, recognizes the corrected page if needed, then saves. Source identity remains visible when comparing a saved OCR result with a new one. No automatic correction/model capability is implied.

FIRST VIEWPORT: Compact app chrome; document title/status at upper left and stable Save/actions at upper right; workflow navigation underneath; a large single page viewer beside a recognition/task pane on desktop. Narrow screens retain the same order with explicit source/text switching. Page, zoom and result controls stay next to their content.

FORM: User-pinned conventional document workspace, based on the requested Paperless-ngx/commercial scanner references and confirmed correction-first flow. Seed reference for explored surface alternatives: seed:4be3ea59f54dd853. The pinned low-travel workflow and product references take precedence over speculative concept alternatives.

FINISH: unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance

## Boundaries and proof

Reuse existing crop/rotate/undo, source coordinates, recognition, candidate adoption/discard, Save and recovery. No backend/schema/model/network-destination change, new physical scan or private-document replay. Ordinary PDF saving remains usable without OCR. Verify 320/390/768/1280/1440, JA/EN, light/dark, long text, missing previews, pending/failed operations, keyboard focus, exact source switching and single visible original. Inspect working screenshots and use the shipped independent finish reviewer before release; record the built system after review.
