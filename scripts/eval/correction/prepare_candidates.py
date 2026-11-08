#!/home/yamato/.pyenv/shims/python3
import argparse
import hashlib
import json
import os
import subprocess
import sys
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument("--root", required=True)
parser.add_argument("--tesseract-runtime")
parser.add_argument("--output", required=True)
args = parser.parse_args()

repo = Path(__file__).resolve().parents[3]
root = Path(args.root).resolve()
tess = Path(args.tesseract_runtime).resolve() if args.tesseract_runtime else None
output = Path(args.output)

def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()

def optional_pin(name, version, license, path):
    if not path.is_file():
        return None
    return {"name": name, "version": version, "license": license, "sha256": digest(path)}

def config(candidate_id, executable, version, sources, dependencies, environment, steps):
    return {
        "format": "scanner-eval-candidate",
        "version": 1,
        "candidate_id": candidate_id,
        "candidate_version": version,
        "executable": str(executable),
        "executable_sha256": digest(executable),
        "argv": [],
        "environment": ["EVAL_CANDIDATE_ID=" + candidate_id] + environment,
        "parameters": {},
        "sources": sources,
        "dependencies": dependencies,
        "operator_steps": steps,
    }

pillow_version = "12.2.0"
candidates = {
    "none": config(
        "correction_none_001", repo / "scripts/eval/correction/no_correction.py", "1",
        [{"name": "scanner-kiosk correction helper", "version": "1", "license": "Apache-2.0"},
         {"name": "python", "version": "3", "license": "PSF-2.0"},
         {"name": "Pillow", "version": pillow_version, "license": "MIT-CMU"}],
        [],
        [], ["copy the immutable evaluation input without pixel transformation"],
    ),
    "leptonica": config(
        "correction_leptonica_001", root / "bin/leptonica_candidate", "1.87.0",
        [{"name": "Leptonica", "version": "1.87.0", "license": "BSD-2-Clause",
          "sha256": "fa2b40c5caea96d1bb93a97486262aed8731b69ce25a84a6bf5d25323e33f631"},
         {"name": "libpng", "version": "1.6.43", "license": "libpng-2.0"},
         {"name": "libjpeg", "version": "80", "license": "IJG"},
         {"name": "zlib", "version": "1.3", "license": "Zlib"}],
        [],
        [], ["build Leptonica 1.87.0 from the pinned source archive"],
    ),
    "opencv": config(
        "correction_opencv_001", root / "bin/opencv_candidate", "4.6.0",
        [{"name": "OpenCV", "version": "4.6.0", "license": "Apache-2.0"},
         {"name": "host OpenCV runtime", "version": "4.6.0+dfsg-13.1ubuntu1", "license": "Apache-2.0"}],
        [],
        [], ["use the installed OpenCV 4.6.0 libraries and headers"],
    ),
}
if tess is not None:
    if tess.is_file():
        version = subprocess.run(
            [str(tess), "--version"],
            check=False,
            capture_output=True,
            text=True,
            timeout=30,
        )
        first_line = (version.stdout + version.stderr).lstrip().splitlines()[0] if (version.stdout + version.stderr).strip() else ""
        if version.returncode != 0 or not first_line.startswith("tesseract 5.5.4"):
            print("scanner-eval: tesseract runtime must begin with tesseract 5.5.4", file=sys.stderr)
            raise SystemExit(2)
        runtime_root = tess.parent.parent
        extracted = [
            optional_pin("tesseract", "5.5.4", "Apache-2.0", runtime_root / "usr/bin/tesseract"),
            optional_pin("libtesseract", "5.5.4", "Apache-2.0", runtime_root / "usr/lib/libtesseract.so.5.5.4"),
            optional_pin("jpn.traineddata", "5.5.4", "Apache-2.0", runtime_root / "usr/share/tessdata/jpn.traineddata"),
            optional_pin("eng.traineddata", "5.5.4", "Apache-2.0", runtime_root / "usr/share/tessdata/eng.traineddata"),
        ]
        candidates["tesseract"] = config(
            "correction_tesseract_internal_001", repo / "scripts/eval/correction/tesseract_internal.py", "5.5.4",
            [{"name": "scanner-native-tesseract", "version": "5.5.4",
              "license": "Apache-2.0", "sha256": digest(tess)},
             {"name": "Leptonica", "version": "1.87.0", "license": "BSD-2-Clause"},
             {"name": "Pillow", "version": pillow_version, "license": "MIT-CMU"}],
            [pin for pin in extracted if pin is not None],
            ["EVAL_TESSERACT_ONEFILE=" + str(tess),
             "EVAL_TESSERACT_TESSDATA=" + str(runtime_root / "usr/share/tessdata")],
            ["use the pre-existing pinned scanner-native-tesseract onefile without installing packages"],
        )
    else:
        candidates["tesseract"] = config(
            "correction_tesseract_internal_001", repo / "scripts/eval/correction/tesseract_internal.py", "5.5.4",
            [{"name": "Tesseract", "version": "5.5.4",
              "license": "Apache-2.0", "sha256": digest(tess / "usr/bin/tesseract")},
             {"name": "Leptonica", "version": "1.87.0", "license": "BSD-2-Clause",
              "sha256": "fa2b40c5caea96d1bb93a97486262aed8731b69ce25a84a6bf5d25323e33f631"},
             {"name": "Pillow", "version": pillow_version, "license": "MIT-CMU"}],
            [{"name": "jpn.traineddata", "version": "5.5.4", "license": "Apache-2.0",
              "sha256": digest(tess / "usr/share/tessdata/jpn.traineddata")},
             {"name": "eng.traineddata", "version": "5.5.4", "license": "Apache-2.0",
              "sha256": digest(tess / "usr/share/tessdata/eng.traineddata")},
             {"name": "libtesseract", "version": "5.5.4", "license": "Apache-2.0",
              "sha256": digest(tess / "usr/lib/libtesseract.so.5")}],
            ["EVAL_TESSERACT_LOADER=" + str(tess / "lib/ld-musl-x86_64.so.1"),
             "EVAL_TESSERACT_BINARY=" + str(tess / "usr/bin/tesseract"),
             "EVAL_TESSERACT_LIB=" + str(tess / "lib") + ":" + str(tess / "usr/lib"),
             "TESSDATA_PREFIX=" + str(tess / "usr/share/tessdata")],
            ["use the pre-existing pinned v0.1.4 Tesseract runtime without installing packages"],
        )
output.mkdir(mode=0o700, parents=True)
for name, value in candidates.items():
    path = output / (name + ".json")
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w") as stream:
        json.dump(value, stream, indent=2)
        stream.write("\n")
    print(path)
