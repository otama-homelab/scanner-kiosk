import hashlib
import importlib.util
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[3]
SCRIPT = Path(__file__).with_name("prepare_catalog.py")
FONT = Path("/usr/share/fonts/opentype/ipafont-gothic/ipag.ttf")
FONT_HASH = "503af4a8b84d1079b8e2e358dc7f7a7fb8cb7a1f212f35eaef6782dbfc75a55e"


class CatalogTest(unittest.TestCase):
    def generate(self, target, digest=FONT_HASH):
        return subprocess.run([sys.executable, str(SCRIPT), "--font", str(FONT),
                               "--font-sha256", digest, "--output-root", str(target)],
                              capture_output=True, text=True)

    def test_deterministic_japanese_sources(self):
        self.assertTrue(SCRIPT.is_file(), "Japanese corpus generator is missing")
        self.assertTrue(FONT.is_file(), "pinned IPA Gothic font is required")
        with tempfile.TemporaryDirectory() as temp:
            first, second = Path(temp) / "one", Path(temp) / "two"
            for target in (first, second):
                result = self.generate(target)
                self.assertEqual(result.returncode, 0, result.stderr)
            files = sorted(p.relative_to(first) for p in first.rglob("*") if p.is_file())
            self.assertTrue(files)
            for relative in files:
                self.assertEqual((first / relative).read_bytes(), (second / relative).read_bytes())
            catalog = json.loads((first / "evaluation/ocr-catalog.json").read_text())
            original = json.loads((ROOT / "scripts/fixtures/evaluation/catalog.json").read_text())
            self.assertEqual(catalog["fixtures"][:len(original["fixtures"])], original["fixtures"])
            oracle = json.loads((first / "evaluation/ocr-oracles.json").read_text())
            self.assertEqual(oracle["font"]["sha256"], FONT_HASH)
            vertical = next(e for e in oracle["entries"] if e["layout"] == "vertical")
            spans = vertical["pages"][0]["spans"]
            self.assertEqual(spans[0]["box"][0], spans[1]["box"][0])
            self.assertLess(spans[0]["box"][1], spans[1]["box"][1])
            self.assertGreater(spans[0]["box"][0], spans[-1]["box"][0])
            table = next(e for e in oracle["entries"] if e["layout"] == "table")
            self.assertTrue(all(s["text"] and s.get("cell") for s in table["pages"][0]["spans"]))
            self.assertEqual(len(next(e for e in oracle["entries"] if e["layout"] == "two-page")["pages"]), 2)

    def test_wrong_font_rejected_before_writing(self):
        self.assertTrue(SCRIPT.is_file(), "Japanese corpus generator is missing")
        with tempfile.TemporaryDirectory() as temp:
            target = Path(temp) / "out"
            result = self.generate(target, "0" * 64)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse(target.exists())


if __name__ == "__main__":
    unittest.main()
