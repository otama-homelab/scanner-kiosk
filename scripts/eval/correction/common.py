import json
import os
from pathlib import Path


def write_result(algorithm, version, width, height, confidence_kind="unavailable",
                 confidence_value=None, fallback_reason="", crop_quad=None):
    original = os.environ["SCANNER_EVAL_ORIGINAL_ID"]
    pages = [page for page in os.environ["SCANNER_EVAL_SOURCE_PAGES"].split(",") if page]
    output = Path(os.environ["SCANNER_EVAL_OUTPUT"])
    candidate = os.environ.get("EVAL_CANDIDATE_ID", "candidate_correction_001")
    quad = crop_quad if crop_quad is not None else [[0, 0], [width, 0], [width, height], [0, height]]
    parameters = {
        "crop_quad": json.dumps(quad, separators=(",", ":")),
        "rotation_degrees": "0",
        "output_width": str(width),
        "output_height": str(height),
        "confidence_kind": confidence_kind,
        "fallback_reason": fallback_reason,
    }
    if confidence_value is not None:
        parameters["confidence_value"] = f"{confidence_value:.9g}"
    result = {
        "format": "scanner-eval-candidate-result",
        "version": 1,
        "candidate_id": candidate,
        "artifacts": [{
            "id": "artifact_candidate_correction_001",
            "role": "correction",
            "path": "corrected.png",
            "media_type": "image/png",
            "source_pages": pages,
            "parents": [original],
            "algorithm": algorithm,
            "algorithm_version": version,
            "parameters": parameters,
        }],
    }
    (output / "result.json").write_text(json.dumps(result, separators=(",", ":")) + "\n")
