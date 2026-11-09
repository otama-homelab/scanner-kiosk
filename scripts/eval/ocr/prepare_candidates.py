#!/usr/bin/env python3
"""Verify local official artifacts and index completed receipts, never run benchmarks."""
import argparse
import hashlib
import json
from pathlib import Path

COMMIT = "c1c9669177642daed98be14240197d971621b16a"
FONT_SHA256 = "503af4a8b84d1079b8e2e358dc7f7a7fb8cb7a1f212f35eaef6782dbfc75a55e"


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def verify_ref(root, ref):
    root = root.resolve(strict=True)
    relative = Path(ref["path"])
    if relative.is_absolute() or ".." in relative.parts:
        raise ValueError("evidence path outside root")
    path = (root / relative).resolve(strict=True)
    if not path.is_relative_to(root) or not path.is_file() or path.stat().st_size > 200 << 20:
        raise ValueError("invalid evidence file")
    if digest(path) != ref["sha256"]:
        raise ValueError("changed evidence identity")
    return path


def reference(root, path):
    path = path.resolve(strict=True)
    ref = {"path": str(path.relative_to(root.resolve(strict=True))), "sha256": digest(path)}
    verify_ref(root, ref)
    return ref


def index_manifest(root, oracle, pairs):
    if len(pairs) < 1 or len(pairs) > 256 or root.stat().st_mode & 0o077:
        raise ValueError("invalid private evidence root")
    for pair in pairs:
        for key in ("candidate_config", "standalone", "benchmark"):
            verify_ref(root, pair[key])
        for key in ("samples", "sample_catalogs", "page_sources"):
            if not pair[key] or len(pair[key]) > 256:
                raise ValueError("missing completed measurements")
            for ref in pair[key]:
                verify_ref(root, ref)
        if len(pair["samples"]) < 3 or len(pair["samples"]) != len(pair["sample_catalogs"]):
            raise ValueError("missing cold/warm sample identities")
        for identity in pair["identities"]:
            verify_ref(root, identity["file"])
    return {"format": "scanner-eval-ocr-run-manifest", "version": 1,
            "evidence_root": str(root.resolve()), "oracle_sha256": digest(oracle), "pairs": pairs}


def prepare(root, engine, runtime, identities, python, adapter):
    """Materialize a hash-bound adapter config from inspected local identities."""
    if engine not in ("tesseract-5.5.4", "ndlocr-lite-1.3.0"):
        raise ValueError("unsupported pinned engine")
    for item in identities:
        verify_ref(root, item["file"])
    if not any(i["kind"] == "font" and i["file"]["sha256"] == FONT_SHA256 for i in identities):
        raise ValueError("approved font missing")
    runtime["files"] = [i["file"] for i in identities]
    lock = root / (engine + "-runtime.json")
    with lock.open("x") as stream:
        json.dump(runtime, stream, indent=2)
    lock.chmod(0o600)
    return {"format": "scanner-eval-candidate", "version": 1, "candidate_id": engine,
            "candidate_version": runtime["version"], "executable": str(python),
            "executable_sha256": digest(python), "argv": [str(adapter)],
            "environment": ["OCR_ENGINE=" + engine, "OCR_EVIDENCE_ROOT=" + str(root),
                            "OCR_RUNTIME_LOCK=" + str(lock), "OCR_RUNTIME_LOCK_SHA256=" + digest(lock),
                            "OMP_NUM_THREADS=2", "OPENBLAS_NUM_THREADS=2"],
            "sources": [{"name": "scanner-kiosk OCR adapter", "version": "1", "license": "Apache-2.0", "sha256": digest(adapter)}],
            "dependencies": [], "parameters": {},
            "operator_steps": ["Verify immutable official source/model blobs and PyPI wheel SHA256 before offline isolated install.",
                               "Run the same frozen fixture through real CPU recognition and the common production compositor."]}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", required=True, type=Path)
    parser.add_argument("--oracle", required=True, type=Path)
    parser.add_argument("--pairs", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    result = index_manifest(args.root, args.oracle, json.loads(args.pairs.read_text()))
    with args.output.open("x") as stream:
        json.dump(result, stream, indent=2)
        stream.write("\n")
    args.output.chmod(0o600)
