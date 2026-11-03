# Captured bitmap validation

`internal/captureimage.Validate` validates a completed immutable staged JPEG/PNG in
the resource-limited worker. The caller supplies exact expected bitmap dimensions
from its checked scanner geometry, the requested color mode, byte budget and pixel
budget. The validator does not certify optical DPI or infer paper dimensions from
a file extension, filename or untrusted image metadata.

It bounds the entire encoded file, hashes every byte, reads header dimensions and
checks pixel multiplication by division before full image allocation. It then fully
decodes the bitmap, verifies dimensions and supported decoded 8-bit gray/RGB color
mode, and rejects transparent/unsupported decoded types. Returned MIME type, size
and SHA256 come from the actual data. A cancelled read returns cancellation; decoder
errors are normalized without disclosing file contents or paths. The reader is
rewound after success. No bitmap is retained in the returned metadata.

The input must be a private regular file or trusted memory fixture. All writers
must have stopped first and the file must remain immutable while inspected. It
must be synced/closed before its metadata is passed to `CommitScanPage`. Seek and
context checks do not provide an OS sandbox or hard CPU deadline inside a decoder;
native/process memory limits and one-heavy-job scheduling remain required. Byte
and pixel limits do not prove real scanner DPI accuracy or image quality.

Tests cover PNG/JPEG in gray/color, actual byte size/hash, damaged/truncated data,
geometry/color mismatch, byte/pixel limits, unsupported decoded depth/transparency
and pre-cancelled work. A database integration creates an actual synthetic gray
620×877 PNG, validates it, syncs/closes it and publishes its verified metadata and
capture settings through scan completion. Device capture/stop reports in that test
remain simulated; it is not Canon, A4 metrology, native adapter or application
acceptance. The actual scanner execution and completed-file handoff are pending.
