# OCR action and failure clarity

The owner reports LLM OCR failed and its workflow is hard to understand. Preserve the existing document workbench and import → correction → OCR → save sequence.

## Observed causes

Read-only metadata shows two recent LLM re-recognition jobs failed within about0.3–0.9seconds while an earlier LLM job succeeded. A public68KiB JA/EN fixture reproducesHTTP502 in0.251seconds: OmniRoute cannot reach GUFO. GUFO is waiting in the registered one-use GPU admission shell; Lemonade is running and both reserve the same shared GPU claim. Existing exclusive admission requires competing apps stopped. The current GUFO deployment UID and dormant spec match registered metadata. Owner GPU-role choice is requested; no competing workload was stopped and no admission token was fabricated.

The recognition dialog also missed the shared dialog layout because CSS was scoped to one ID. Initial screenshots show its Start action below the visible modal. An assertion reproduces this before the fix. Empty-result OCR placed recovery below a full correction preview.

## Small coherent fix

- Reuse one shared dialog class/body/footer for output and recognition, with a bounded scroll body and visible actions. Optional output OCR stays optional; recognition opens already enabled.
- Hide the unselected correction tab's panel on the OCR tab. Keep page selection and correction intact.
- Add an optional safe recognition-purpose flag to owned job responses, derived from immutable configuration booleans. No configuration, provider response or credential is exposed; no migration. Existing owner/expiry/error sanitization remains.
- Show recognition-specific progress and local failure recovery. First-use wording, applied/new result labels, current engine and explicit whole-document-use guidance clarify the step before saving. No automatic retry, adoption or fallback is added.

## Proof and limits

Purpose projection cases and owner/expiry/safe-error tests pass. Browser scenarios test direct enabled recognition without a second switch, unavailable/empty catalogues, recognition progress/failure, retained download, visible actions/recovery, JA/EN320/390/1280, plus comparison/adoption/discard/unknown outcomes. Frozen full browser/API/worker proof and independent finish review are separate gates. Real owner devices and restoration of GUFO are not implied by mock browser success; current LLM restoration depends on the explicit GPU-role choice.
