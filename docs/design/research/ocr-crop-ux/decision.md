# OCR UX and automatic document cropping

2026-11-12. Scope: existing document OCR controls and saved-result comparison.

OCR remains opt-in. The shared controls used for output and re-recognition explain searchable text and document-language selection. Languages use the available dialog width rather than a one-line scrolling strip. Empty processing notices take no space; configured external destinations remain visible before submission. Result identity, model version and image hash remain available under native, keyboard-accessible disclosure. Original/candidate comparison, whole-document adoption, cancellation, uncertain-request recovery and previous downloads are unchanged.

Automatic document cropping is not enabled. The official FairScan CPU/JVM path executes, but its fixed-mask synthetic corner pilot passes 0/6; detection alone is not evidence of a safe crop. Original-photo independent annotations and strict Android parity remain missing. See `../correction-validation-v060/summary.json` and `../correction-fairscan-mask-parity-v060/summary.json`.

The next eligible integration is a crop proposal using an existing official model/library, preserving the original and requiring preview/explicit application with undo. It must first establish exact runtime/model/license identity and mask/coordinate parity, then measure independently annotated corners and preservation of edge text, numbers and thin lines on same-original phone/scanner inputs. Failed or missing detection retains the original and existing manual crop. No custom detector, training, converter or relaxed quality gate is authorized by this UI change. Generated examples may exercise controls but cannot replace the missing original-photo annotations.

This change does not close the v0.6.0 model or device acceptance gaps and does not add a new model or network destination.
