#!/usr/bin/env python3
import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from PIL import Image


HERE = Path(__file__).resolve().parent
PREPARE = HERE / "prepare_candidates.py"
TESSERACT = HERE / "tesseract_internal.py"


class TesseractOnefileTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name)
        for name in ("leptonica_candidate", "opencv_candidate"):
            path = self.root / "bin" / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(name.encode())
        runtime = self.root / "runtime"
        self.onefile = runtime / "tools" / "scanner-native-tesseract"
        self.onefile.parent.mkdir(parents=True)
        self.onefile.write_text(
            "#!/usr/bin/env python3\n"
            "import sys\n"
            "from PIL import Image\n"
            "if sys.argv[1:] == ['--version']:\n"
            "    print('tesseract 5.5.4')\n"
            "    raise SystemExit(0)\n"
            "Image.new('RGB', (12, 8), 'white').save('stdout.processed.tif')\n"
        )
        self.onefile.chmod(0o555)
        for relative, payload in (
            ("usr/bin/tesseract", b"inner tesseract"),
            ("usr/lib/libtesseract.so.5.5.4", b"inner library"),
            ("usr/share/tessdata/jpn.traineddata", b"japanese model"),
            ("usr/share/tessdata/eng.traineddata", b"english model"),
        ):
            path = runtime / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(payload)

    def tearDown(self):
        self.temporary.cleanup()

    def test_prepare_accepts_versioned_onefile_and_records_extracted_hashes(self):
        output = self.root / "candidates"
        result = subprocess.run(
            [
                sys.executable,
                str(PREPARE),
                "--root",
                str(self.root),
                "--tesseract-runtime",
                str(self.onefile),
                "--output",
                str(output),
            ],
            text=True,
            capture_output=True,
            timeout=30,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        config = json.loads((output / "tesseract.json").read_text())
        self.assertEqual(config["candidate_version"], "5.5.4")
        self.assertIn("EVAL_TESSERACT_ONEFILE=" + str(self.onefile), config["environment"])
        self.assertEqual(config["sources"][0]["sha256"], self.digest(self.onefile))
        self.assertEqual(
            {entry["name"] for entry in config["dependencies"]},
            {
                "tesseract",
                "libtesseract",
                "jpn.traineddata",
                "eng.traineddata",
            },
        )

    def test_tesseract_helper_invokes_onefile_with_its_tessdata(self):
        output = self.root / "candidate-output"
        output.mkdir()
        source = self.root / "source.png"
        Image.new("RGB", (12, 8), "white").save(source)
        environment = dict(
            os.environ,
            SCANNER_EVAL_INPUT=str(source),
            SCANNER_EVAL_OUTPUT=str(output),
            SCANNER_EVAL_ORIGINAL_ID="artifact_original_onefile",
            SCANNER_EVAL_SOURCE_PAGES="page_onefile",
            EVAL_CANDIDATE_ID="candidate_tesseract_onefile",
            EVAL_TESSERACT_ONEFILE=str(self.onefile),
            EVAL_TESSERACT_TESSDATA=str(self.root / "runtime" / "usr" / "share" / "tessdata"),
        )
        result = subprocess.run(
            [sys.executable, str(TESSERACT)],
            cwd=output,
            env=environment,
            text=True,
            capture_output=True,
            timeout=30,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue((output / "corrected.png").is_file())
        receipt = json.loads((output / "result.json").read_text())
        self.assertEqual(receipt["candidate_id"], "candidate_tesseract_onefile")

    def test_prepare_rejects_wrong_tesseract_version(self):
        bad = self.root / "runtime" / "tools" / "scanner-native-tesseract-5.5.2"
        bad.write_text("#!/bin/sh\nprintf 'tesseract 5.5.2\\n'\n")
        bad.chmod(0o555)
        output = self.root / "bad-candidates"
        result = subprocess.run(
            [
                sys.executable,
                str(PREPARE),
                "--root",
                str(self.root),
                "--tesseract-runtime",
                str(bad),
                "--output",
                str(output),
            ],
            text=True,
            capture_output=True,
            timeout=30,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("5.5.4", result.stderr)
        self.assertFalse((output / "tesseract.json").exists())

    @staticmethod
    def digest(path):
        import hashlib

        return hashlib.sha256(path.read_bytes()).hexdigest()


if __name__ == "__main__":
    unittest.main()
