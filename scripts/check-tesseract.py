#!/usr/bin/env python3
"""Run the product adapter against an installed CLI using an artificial JP/EN page."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
from datetime import datetime, timezone

p = argparse.ArgumentParser()
for name in ('fixture-python', 'binary', 'tessdata', 'font', 'receipt'):
    p.add_argument('--' + name, required=True)
p.add_argument('--standalone-binary', help='Exercise this bundled worker with a clean cache and automatic native paths')
a = p.parse_args()
repo = Path(__file__).resolve().parent.parent
sources = [*repo.glob('cmd/**/*.go'), *repo.glob('internal/**/*.go'), *repo.glob('internal/**/*.sql'), *repo.glob('pkg/**/*.go'), repo / 'go.mod', repo / 'go.sum', Path(__file__)]
def hashes():
    return {str(x.relative_to(repo)): hashlib.sha256(x.read_bytes()).hexdigest() for x in sorted(sources)}
before = hashes()
fixture_code = '''
import sys
from PIL import Image, ImageDraw, ImageFont
image=Image.new('RGB',(2480,3508),'white')
draw=ImageDraw.Draw(image);font=ImageFont.truetype(sys.argv[1],80)
for y,text in [(200,'日本語の書類'),(400,'Scanner OCR 2026'),(600,'テスト文書 300 dpi')]:
    draw.text((180,y),text,font=font,fill='black')
image.save(sys.argv[2],dpi=(300,300))
dense=Image.new('RGB',(2480,3508),'white')
draw=ImageDraw.Draw(dense);font=ImageFont.truetype(sys.argv[1],80)
for i in range(15):
    draw.text((180,200+i*200),['日本語の書類','Scanner OCR 2026','テスト文書 300 dpi'][i%3],font=font,fill='black')
dense.save(sys.argv[3],dpi=(300,300))
'''
with tempfile.TemporaryDirectory(prefix='scanner-provider-fixture-') as temp:
    fixture = Path(temp) / 'synthetic.png'
    orientation_fixture = Path(temp) / 'orientation.png'
    subprocess.run([a.fixture_python, '-c', fixture_code, a.font, str(fixture), str(orientation_fixture)], check=True, capture_output=True)
    env = dict(os.environ, GOTOOLCHAIN='go1.27.3', SCANNER_TEST_TESSERACT_BINARY=str(Path(a.binary).resolve()), SCANNER_TEST_TESSDATA=str(Path(a.tessdata).resolve()), SCANNER_TEST_OCR_FIXTURE=str(fixture), SCANNER_TEST_OCR_ORIENTATION_FIXTURE=str(orientation_fixture), SCANNER_TEST_OCR_COMPOSE_OUTPUT=temp)
    if a.standalone_binary:
        env['SCANNER_TEST_STANDALONE_BINARY'] = str(Path(a.standalone_binary).resolve(strict=True))
    go_stdout = Path(a.receipt).with_suffix('.go-test.jsonl')
    go_stderr = Path(a.receipt).with_suffix('.go-test.stderr.log')
    with go_stdout.open('w') as stdout, go_stderr.open('w') as stderr:
        result = subprocess.run(['go', 'test', '-race', '-count=1', '-json', './internal/ocr/tesseract', './internal/artifactscope', './internal/ocrcomposition', './internal/workerruntime'], cwd=repo, env=env, stdout=stdout, stderr=stderr, text=True, timeout=300)
    events = [json.loads(line) for line in go_stdout.read_text().splitlines() if line.startswith('{')]
    if result.returncode != 0 or any(e['Action'] in ('fail','skip') for e in events):
        raise SystemExit('Tesseract adapter integration failed:\n' + go_stdout.read_text()[-24000:] + go_stderr.read_text()[-4000:])
    passed = [e['Test'] for e in events if e['Action']=='pass' and 'Test' in e]
    required = {'TestInstalledTesseract/text', 'TestInstalledTesseract/pdf_text_layer', 'TestInstalledTesseractScoped/text', 'TestInstalledTesseractScoped/pdf_text_layer', 'TestInstalledTesseractPDFComposition', 'TestInstalledWorkerOCRCommand'}
    required.update(f'TestInstalledTesseractOrientation/{angle}/{kind}' for angle in (0,90,180,270) for kind in ('text','pdf_text_layer'))
    if not required.issubset(passed) or before != hashes():
        raise SystemExit('Missing real CLI evidence or source changed during verification')
    inspect_code = r"""
import json,sys,subprocess
from pathlib import Path
from PIL import Image,ImageChops
import pikepdf
root=Path(sys.argv[1])
with pikepdf.open(root/'layer.pdf') as layer:
    assert len(layer.pages)==1 and not layer.pages[0].images
with pikepdf.open(root/'base.pdf') as base, pikepdf.open(root/'composed.pdf') as composed:
    assert len(base.pages)==len(composed.pages)==1
    before=base.pages[0].images;after=composed.pages[0].images
    assert len(before)==len(after)==1
    left=pikepdf.PdfImage(next(iter(before.values()))).as_pil_image()
    right=pikepdf.PdfImage(next(iter(after.values()))).as_pil_image()
    assert left.mode==right.mode and left.size==right.size and left.tobytes()==right.tobytes()
for name in ['base','composed']:
    subprocess.run(['pdftoppm','-scale-to','800','-singlefile','-png',str(root/(name+'.pdf')),str(root/name)],check=True,capture_output=True)
with Image.open(root/'base.png') as before,Image.open(root/'composed.png') as after:
    assert before.size==after.size and ImageChops.difference(before.convert('RGB'),after.convert('RGB')).getbbox() is None
for prefix,rotations in [('multi',[90,0]),('worker',[0,90])]:
    with pikepdf.open(root/(prefix+'-base.pdf')) as base,pikepdf.open(root/(prefix+'-composed.pdf')) as composed:
        assert len(base.pages)==len(composed.pages)==2
        for index in range(2):
            left,right=base.pages[index],composed.pages[index]
            assert list(left.MediaBox)==list(right.MediaBox)
            expected=[612,792] if prefix=='multi' and index==0 else [210/25.4*72,297/25.4*72]
            assert all(abs(float(value)-wanted)<.02 for value,wanted in zip(list(right.MediaBox)[2:],expected))
            assert int(left.obj.get('/Rotate',0))==int(right.obj.get('/Rotate',0))==rotations[index]
            assert len(left.images)==len(right.images)==1
            before=pikepdf.PdfImage(next(iter(left.images.values()))).as_pil_image()
            after=pikepdf.PdfImage(next(iter(right.images.values()))).as_pil_image()
            assert before.size==after.size and before.tobytes()==after.tobytes()
            text=subprocess.check_output(['pdftotext','-f',str(index+1),'-l',str(index+1),'-raw',str(root/(prefix+'-composed.pdf')),'-'],text=True)
            compact=''.join(text.split())
            assert '日本語の書類' in compact and 'ScannerOCR2026' in compact
    for name in [prefix+'-base',prefix+'-composed']:
        subprocess.run(['pdftoppm','-scale-to','800','-png',str(root/(name+'.pdf')),str(root/name)],check=True,capture_output=True)
    for index in [1,2]:
        with Image.open(root/(prefix+'-base-'+str(index)+'.png')) as before,Image.open(root/(prefix+'-composed-'+str(index)+'.png')) as after:
            assert before.size==after.size and ImageChops.difference(before.convert('RGB'),after.convert('RGB')).getbbox() is None

print(json.dumps(['image-free real Tesseract layers','composed original pixels unchanged','rendered PDF appearance unchanged','Japanese/English extraction checked per page','two-page letter/A4 order and rotations','rotated multi-page rendered appearance unchanged','compiled worker OCR publication and rendered appearance']))
"""
    inspection_stdout = Path(a.receipt).with_suffix('.pdf-inspection.stdout.log')
    inspection_stderr = Path(a.receipt).with_suffix('.pdf-inspection.stderr.log')
    with inspection_stdout.open('w') as stdout, inspection_stderr.open('w') as stderr:
        inspection = subprocess.run([a.fixture_python, '-c', inspect_code, temp], stdout=stdout, stderr=stderr, text=True, timeout=60)
    if inspection.returncode != 0:
        raise SystemExit('Independent PDF inspection failed:\n' + inspection_stderr.read_text()[-4000:])
    composition_checks = json.loads(inspection_stdout.read_text())
    receipt = {'timestamp_utc': datetime.now(timezone.utc).isoformat(), 'passed': True, 'tests': passed, 'composition_checks': composition_checks, 'source_sha256': before, 'fixture_sha256': hashlib.sha256(fixture.read_bytes()).hexdigest(), 'tool_sha256': {'binary': hashlib.sha256(Path(a.binary).read_bytes()).hexdigest(), 'font': hashlib.sha256(Path(a.font).read_bytes()).hexdigest(), **{name:hashlib.sha256((Path(a.tessdata)/(name+'.traineddata')).read_bytes()).hexdigest() for name in ('jpn','eng','osd')}}, 'limitations': ['Synthetic clean 300dpi fixture; no physical scanner or accuracy benchmark', 'Tesseract, scoped two-page composition and compiled OCR worker plus local LLM mock; no HTTP authorization, real LLM/scanner or production acceptance', 'Staging growth watchdog permits overshoot; deployment storage limits still required']}
    receipt['orientation_fixture_sha256'] = hashlib.sha256(orientation_fixture.read_bytes()).hexdigest()
if a.standalone_binary:
    receipt['standalone_binary_sha256'] = hashlib.sha256(Path(a.standalone_binary).read_bytes()).hexdigest()
    receipt['standalone_worker_environment'] = 'empty private cache; no host PATH or explicit native tool/data overrides'
Path(a.receipt).resolve().write_text(json.dumps(receipt, ensure_ascii=False, indent=2)+'\n')
print(json.dumps({'passed': True, 'tests': len(passed), 'receipt': a.receipt}))
