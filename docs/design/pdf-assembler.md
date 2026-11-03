# Product image PDF assembler

`internal/pdfassemble` uses upstream pdfcpu v0.16.4 without patches. Its input is an ordered list of job-authorized stream openers with expected bitmap geometry, color, paper size and page rotation. It does not accept browser filesystem paths or query the database.

The assembler fully validates PNG/JPEG inputs, imports each image on its specified paper size, applies page rotation, merges in supplied order, then validates PDF structure and page count. JPEG mode preserves existing JPEG streams; PNG inputs are encoded at the supplied JPEG quality. Lossless mode preserves PNG pixels and converts decoded JPEG pixels to explicit 8-bit RGB PNG before import. This does not restore detail already lost in JPEG compression. Original artifacts remain unchanged.

Input bytes, pixel counts, page count, transcode bytes, aggregate intermediate PDF bytes and final bytes have explicit bounds. These limits do not bound all upstream allocations: execute inside the resource-limited worker process. Context cancellation is propagated to pdfcpu and checked during input reads; an already-blocked arbitrary stream opener/read cannot be forcibly interrupted by this function.

This is a PDF engine component, not the export coordinator. `GenerateScoped` now resolves authorized input IDs through artifactscope and writes an ordinary `pdf` output. Synthetic component integration exercises generation → seal → conditional DB publication → release → owned download metadata. Immutable job snapshot mapping, publication/read-back orchestration, OCR text-layer composition, image-only export and executable worker integration remain pending. A PDF without OCR must not be advertised as searchable.

## Independent verification

Run with a Python environment containing pikepdf and Pillow:

```sh
/path/to/python scripts/check-pdf.py --receipt /tmp/pdf-assembler-check.json
```

The command runs product Go race tests and generates synthetic mixed grayscale/RGB JPEG/PNG inputs. Independent pikepdf inspection checks page order, A5/A4/A6 MediaBoxes, rotation, compression filters, exact preserved JPEG bytes, lossless decoded pixels (using Go-decoded JPEG pixels as the reference to avoid decoder rounding differences), and unchanged source files. Temporary artifacts are removed. This does not prove real scanner, OCR overlay, worker orchestration, HTTP or deployment behavior.
