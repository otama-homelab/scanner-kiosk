#!/usr/bin/env python3
"""Independent synthetic PDF verification; run with the pikepdf/Pillow Python."""
import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import tempfile
import argparse
import pikepdf
from PIL import Image

parser = argparse.ArgumentParser()
parser.add_argument('--receipt', required=True)
args = parser.parse_args()
root = Path(__file__).resolve().parents[1]
checks = []
with tempfile.TemporaryDirectory(prefix='scanner-pdf-') as directory:
    work = Path(directory)
    env = dict(os.environ, SCANNER_TEST_PDF_OUTPUT_DIR=directory)
    result = subprocess.run(['go', 'test', '-race', '-count=1', './internal/pdfassemble'], cwd=root, env=env, capture_output=True, text=True)
    if result.returncode:
        raise RuntimeError(result.stdout + result.stderr)
    checks.append('Go assembler race tests and synthetic fixture generation')
    names = ['source-gray.png', 'source-jpeg.jpg', 'source-rgb.png']
    original = {name: (work / name).read_bytes() for name in names}
    sizes = [(148, 210), (210, 297), (105, 148)]
    for encoding in ['jpeg', 'lossless']:
        with pikepdf.open(work / (encoding + '.pdf')) as pdf:
            assert len(pdf.pages) == 3
            for index, page in enumerate(pdf.pages):
                box = [float(v) for v in page.MediaBox]
                expected = [0, 0, *(v / 25.4 * 72 for v in sizes[index])]
                assert all(abs(a-b) < .02 for a,b in zip(box, expected))
                assert int(page.obj.get('/Rotate', 0)) % 360 == (90 if index == 0 else 0)
                images = list(page.get_images().values())
                assert len(images) == 1
                image = images[0]
                filters = image.get('/Filter')
                filters = list(filters) if isinstance(filters, pikepdf.Array) else [filters]
                assert ('/DCTDecode' in [str(v) for v in filters]) == (encoding == 'jpeg')
                if encoding == 'jpeg' and index == 1:
                    assert image.read_raw_bytes() == original[names[index]]
                if encoding == 'lossless':
                    actual = pikepdf.PdfImage(image).as_pil_image()
                    expected_image = Image.open(work / "source-jpeg-decoded.png") if index == 1 else Image.open(io.BytesIO(original[names[index]]))
                    mode = 'L' if index == 0 else 'RGB'
                    assert actual.convert(mode).tobytes() == expected_image.convert(mode).tobytes()
            checks.append(encoding + ': page order, paper sizes, rotation, image encoding verified independently')
    for encoding in ['jpeg', 'lossless']:
        path = work / ('ocr-' + encoding + '.pdf')
        with pikepdf.open(path) as pdf:
            assert len(pdf.pages)==2
            for index, page in enumerate(pdf.pages):
                assert int(page.obj.get('/Rotate',0)) % 360 == [180,90][index]
                paper = [(148,210),(210,297)][index]
                expected_box = [0,0,*(value/25.4*72 for value in paper)]
                assert all(abs(float(actual)-expected)<.02 for actual,expected in zip(page.MediaBox,expected_box))
                images = list(page.get_images().values())
                assert len(images)==1
                image = images[0]
                if encoding=='jpeg' and index==1:
                    assert image.read_raw_bytes()==original['source-jpeg.jpg']
                if encoding=='lossless':
                    expected = Image.open(work / ('source-rgb.png' if index==0 else 'source-jpeg-decoded.png')).convert('RGB')
                    assert pikepdf.PdfImage(image).as_pil_image().convert('RGB').tobytes()==expected.tobytes()
                text = subprocess.check_output(['pdftotext','-f',str(index+1),'-l',str(index+1),'-raw',str(path),'-'],text=True)
                compact = ''.join(text.split())
                assert ['BETATWO','ALPHAONE'][index] in compact
                assert ['ALPHAONE','BETATWO'][index] not in compact
        checks.append(encoding + ': OCR page-ID binding, order, rotations, extracted text and original image retention')
    assert all((work / name).read_bytes() == data for name, data in original.items())
    checks.extend(['JPEG stream preserved byte-for-byte', 'lossless decoded pixels match all originals', 'original files unchanged'])
files = sorted([*root.glob('internal/pdfassemble/*.go'), *root.glob('internal/captureimage/*.go'), *root.glob('internal/artifactscope/*.go'), *root.glob('internal/pdftextlayer/*.go'), root/'go.mod', root/'go.sum', Path(__file__).resolve()])
receipt = {'scope': 'Synthetic product image/OCR-layer PDF assembly only; no scanner, OCR provider, worker coordinator or HTTP acceptance', 'checks': checks, 'sources': {str(p.relative_to(root)): hashlib.sha256(p.read_bytes()).hexdigest() for p in files}, 'tools': {'pikepdf': pikepdf.__version__, 'go': subprocess.check_output(['go', 'version'], text=True).strip()}}
Path(args.receipt).write_text(json.dumps(receipt, indent=2) + '\n')
print(json.dumps({'passed': len(checks), 'scope': receipt['scope']}))
