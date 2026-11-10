# v0.6.0 autonomous receipt contract

Date: 2026-11-11. This is a fixed evidence schema for the six gap plans, not implementation code.

Every persisted receipt is written by the root. Coordinators and workers return proposals or private shards only. Each evidence reference has exactly these nonempty fields:

| Field | Type | Rule |
|---|---|---|
| `path` | string | Repository-relative receipt/source path or opaque local evidence ID; never an owner-private path |
| `sha256` | string | 64 lowercase hexadecimal characters over the referenced bytes |
| `revision` | string | Nonempty source, input, or evidence revision |

Predicate maps have the exact keys named by their plan and Boolean values. An empty map, missing key, non-Boolean value, empty evidence list, or evidence reference with an empty/invalid field fails validation.

Statuses are `measured-pass`, `measured-fail`, `not-run`, or `waiting_external_*`. Every waiting/not-run row has `complete=false`, `complete_measurement=false`, and `quality_pass=false`. A measured failure may have `complete_measurement=true` but always has `quality_pass=false`. Requirement completion is row-specific: only an executed measurement/determination requirement may complete with measured-fail; truth acquisition cannot.

Negative counterexamples that every join rejects:

* `{"status":"waiting_external_input","complete":true}`
* `{"status":"measured-fail","quality_pass":true}`
* `{"predicates":{},"evidence":[]}`
* `{"status":"ready","predicates":{"required":false}}`
* `{"sha256":"","revision":""}`

The final audit keeps exactly the fourteen original IDs in order. Each row binds at least one valid evidence reference. `original_14_of_14_verified` is valid only when every row is complete; otherwise the status is `automation_queue_exhausted`.

Negative checks run against copies of the actual receipts in private scratch storage: delete a required predicate, empty the evidence list, set waiting plus complete=true, and set measured-fail plus quality_pass=true. Execute the same plan validator with only its input file redirected to each invalid copy; require nonzero exit. Never insert an assertion about an intentionally invalid raw condition into the success-path command. Original receipts remain unchanged.
