# Capability HTTP projection

Authenticated `GET /api/capabilities` reads the current worker OCR catalog through the read-only source. Missing, stale or withdrawn metadata returns 503; queries are rejected. The session/public-host guard runs before catalog access. Responses are no-store. The projection accepts sanitized scanner IDs/names and deployment limits, never endpoint URLs, drivers, credentials, database paths or worker identities.

Only models whose native output can feed the installed PDF pipeline (`searchable_pdf` or `pdf_text_layer`) are listed. Their effective output is `searchable_pdf`; language/preset names and exact model version are metadata from the validated catalog. Page and pixel limits are capped by deployment policy. `available` means configured with a compatible installed renderer, not a remote health/accuracy guarantee. Text/positioned-text-only models are omitted until a renderer is implemented. OCR defaults to off. The worker publishes its configured preferred model, languages and preset as validated metadata; the response includes that preference only when the model is compatible with the installed renderer. Each model also exposes its explicitly published `default_quality_preset`, or null when the user must choose. Provider names are public labels. No model/language/preset is guessed from array order by the API or browser.

Scanner display names must contain 1–256 Unicode code points. Deployment configuration rejects names outside this range with a key-only `SCANNER_SCANNERS_JSON` error before startup. The browser uses the same code-point count (not UTF-16 code units), preserves the label, and rejects an invalid inventory rather than truncating names. Emoji sequences and combining marks can contain multiple code points; this limit is not a grapheme count.

The content revision is a SHA-256 of the public projection with an empty revision field. Refreshing the same metadata does not change it; policy/model changes do. It excludes private coordinator identity and lease timestamps. The response follows the existing Capabilities contract. The previously unimplemented `limits.retention_hours` was changed to `retention_seconds` to represent all valid deployment durations without truncation.

Scanner capability publication/readiness is still incomplete. Configured devices appear with state `unknown` and empty source/size/color/DPI choices. The A4/color/300 capture default is the application preference, not evidence that a device supports it. No probe, remote OCR request or scan is started by this endpoint. Scan admission must stay gated on actual supported settings and device recovery evidence.

HTTP tests exercise authenticated publication/read, absent/withdrawn catalogs, query/Host rejection, filtering incompatible renderers, deployment caps, stable content revisions and exclusion of private fields. They publish fixture OCR metadata and do not establish an end-to-end live model selection, physical device readiness or browser OCR UI acceptance. The browser output dialog now consumes the projection; browser admission uses fixture metadata and is documented separately in product-web.md.


Scanner tuple publication is now connected through its own metadata Source. capture_options contains complete verified combinations; facet lists are not a Cartesian product. default_capture is nullable when the standard tuple is unverified or over budget. State stays unknown until readiness/admission are implemented. See [scanner metadata](scanner-metadata.md) for authority, caching, projection and validation.

Scanner availability now comes from a transactional scannerreadiness.Source.
GET /api/scanners shares the verified scanner projection without requiring OCR
metadata. See [scanner status](scanner-status.md); physical readiness production
is still unfinished, and POST admission revalidates every operative predicate.
