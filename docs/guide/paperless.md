# Paperless-ngx compatibility

[User guide](usage.md) · [Installation](getting-started.md)

## Searchable PDF text

Paperless full-text search depends on extracted text. Incorrect OCR characters or missing text can cause missed matches; coordinate errors primarily affect selection and highlighting.

LLM supplements are optional and off by default. When enabled, they are also PDF text. Their additional descriptions can help discovery, but may produce matches for words absent from the original and influence automatic classification. The label `LLM supplement (interpretation, not OCR)` distinguishes their meaning for readers. A PDF text layer alone does not give the search engine separate original-text and supplement fields.

## Choose an OCR mode

The [Paperless v3.2.1 configuration reference](https://github.com/paperless-ngx/paperless-ngx/blob/v3.2.1/docs/configuration.md#PAPERLESS_OCR_MODE) describes these modes:

| Mode | Effect on Scanner Kiosk PDFs |
| --- | --- |
| `auto` | Uses `pdftotext` to detect sufficient existing text and skips OCR when found. A suitable starting point for retaining Scanner Kiosk's OCR. |
| `off` | Extracts existing text without running OCR. Image-only documents will not gain recognized text. Consider the rest of your intake workflow before selecting it. |
| `redo` / `force` | Can replace text layers or rasterize and recognize again. Invisible interpretation supplements may be lost. |

Archive PDF/A generation is a separate setting. UI settings may override environment variables; check the effective configuration. Scanner Kiosk does not automatically change Paperless settings.

Poppler/PDF.js extraction and original-image/text geometry are covered by local tests. Actual Paperless consumption, PDF/A conversion, and indexed search remain a separate integration check.

## Direct delivery from the app

The existing adapter has a fixed **Paperless-ngx v3.4.2 / API9** contract. It does not treat other versions as compatible. For example, direct delivery to v3.2.1 requires separate acceptance of version checks, task responses, and checksum reconciliation.

If direct delivery is not configured and accepted, download the PDF and import it through Paperless's own intake workflow first. App delivery requires the user to confirm the document and destination explicitly. Running OCR or saving a PDF does not automatically upload it to Paperless.
