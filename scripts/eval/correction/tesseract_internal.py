#!/home/yamato/.pyenv/shims/python3
import os
import subprocess
from pathlib import Path

from PIL import Image

from common import write_result

loader = os.environ["EVAL_TESSERACT_LOADER"]
binary = os.environ["EVAL_TESSERACT_BINARY"]
runtime_lib = os.environ["EVAL_TESSERACT_LIB"]
source = os.environ["SCANNER_EVAL_INPUT"]
output = Path(os.environ["SCANNER_EVAL_OUTPUT"])
subprocess.run([
    loader,
    f"--library-path={runtime_lib}",
    binary,
    source,
    "stdout",
    "-l", "jpn+eng",
    "--psm", "1",
    "-c", "tessedit_write_images=1",
], cwd=output, check=True, timeout=90, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
internal_files = list(output.glob("*.processed.tif"))
if len(internal_files) != 1:
    raise SystemExit("tesseract internal image unavailable")
internal = internal_files[0]
with Image.open(internal) as image:
    image.load()
    corrected = image.convert("RGB")
    corrected.save(output / "corrected.png", format="PNG")
    width, height = corrected.size
write_result("tesseract-internal", "5.5.4", width, height,
             fallback_reason="ocr-internal-image")
