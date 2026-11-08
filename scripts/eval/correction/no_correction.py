#!/home/yamato/.pyenv/shims/python3
import os
import shutil
from pathlib import Path

from PIL import Image

from common import write_result

source = Path(os.environ["SCANNER_EVAL_INPUT"])
output = Path(os.environ["SCANNER_EVAL_OUTPUT"])
target = output / "corrected.png"
shutil.copyfile(source, target)
with Image.open(target) as image:
    image.load()
    width, height = image.size
write_result("none", "1", width, height)
