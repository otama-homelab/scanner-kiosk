# Synthetic correction references

Generated with the built-in image_gen tool for owner-authorized local evaluation. Artificial pages; no private documents. Original output rasters are retained without editing. Their pixels are the reference, not an assertion that generated text or page lighting is physically correct. Photo lighting, paper gradients and seal texture are reference content to preserve. No OCR transcription is inferred from these images.

Prompt set:
- Blue: scientific/educational clean frontal pale-blue page, edge-to-edge, no perspective or cast shadows; landscape/lake and red-flower photographs, two red circular star seals, color swatches and fine black rules; no words, numbers, signatures or logos.
- Peach: independent clean frontal pale-peach page, no perspective, curl or cast shadows; brick-street and boat/sea photographs, two green circular leaf seals, fine grid and cyan/magenta/yellow/black/orange/violet swatches; no words, numbers, signatures or logos.

Existing `evaluation.ResolveFixture` pattern/faint and pattern/shadow recipes are reused unchanged for the paired corpus. No new correction or shadow synthesis algorithm was implemented. Replay helper and fixed manifest: `docs/design/research/correction-paired-v060/`. Run prepare.py from repository root after compiling materialize.go from a temporary directory inside the Go module (required for internal package imports); the binary belongs at `/tmp/scanner-v060-paired/materialize`. Python uses the already isolated CPU environment, NumPy and Pillow.

SHA256:
- `reference-blue.png`: `e7ea007f5d69c82bf185b3ecc3e47d127189356ca9046036c6961bc280687ea4`
- `reference-peach.png`: `f192c56c7eae4f3d860c7a99189b2e71b11bbd1daba4c4c10af7fe4bc66bf34b`
