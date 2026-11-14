# LLM PDF OCR and distribution — v0.2.32

Released and deployed exact source `0535151860d7deec8a0510a07c2153237f564767`. [Release](https://github.com/otama-homelab/scanner-kiosk/releases/tag/v0.2.32) · [evidence](release.json).

Two coordinate defects were reproduced: normalized LLM boxes treated as pixels, and relative PDF watermark scaling differing from the image contain-fit. The provider now validates a1000grid before converting to exact pixels; the shared overlay uses absolute contain-fit. A bounded interpretation field stays separate from transcription and is embedded as labeled searchable PDF text. Existing imported PDF text is retained, with only the supplement added. Old OCR/PDF artifacts stay immutable; create and explicitly adopt a new candidate to update them.

The distribution includes Japanese setup/use/Paperless guides and real desktop/mobile screenshots made with synthetic data. Candidate archive checks verify their exact source contents and exclude internal research. Seven required CI jobs, full race,600dpi release profile, worker and integrated browser paths passed.48publicassets, paired OCI/config/aliases, same-schema27 CAS/Recreate deployment, all3containersReady/restart0 and10servedassets were checked. Source/license/native inputs remain bound;12existing advisories/rawthresholdfalse remain explicit.

v0.2.31 remains an unpublished failed candidate. Its fixture failure and earlier local timeouts are retained separately from later passes. No external LLM request, private replay, physical scan, livePaperless ingest or owner device acceptance was performed. Original correction requirements remain7/14complete; no new correction model is qualified.
