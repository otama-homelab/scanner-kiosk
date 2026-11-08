#!/home/yamato/.pyenv/shims/python3
import argparse
import json
import os
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument("--catalog", required=True)
parser.add_argument("--output", required=True)
args = parser.parse_args()

source = json.loads(Path(args.catalog).read_text())
fixtures = []
for fixture in source["fixtures"]:
    if fixture["document_type"] in {"invalid", "oversized", "dependency-failure"}:
        continue
    fixture = dict(fixture)
    fixture["expected_outputs"] = ["correction"]
    fixture.pop("expected_text", None)
    fixture.pop("expected_failure_class", None)
    fixtures.append(fixture)
source["fixtures"] = fixtures
path = Path(args.output)
path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
with os.fdopen(fd, "w") as stream:
    json.dump(source, stream, indent=2, ensure_ascii=False)
    stream.write("\n")
