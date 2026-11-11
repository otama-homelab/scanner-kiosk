import json,hashlib,shutil
from pathlib import Path
import numpy as np
from PIL import Image
root=Path('/tmp/scanner-fairscan-jvm.k0rsMW');src=root/'FairScan-2c5230b12f69f3cee2cc5a970013da20848d1272';corpus=Path('/tmp/scanner-v060-synthetic');masks=Path('/tmp/scanner-v060-detection');out=Path('/home/yamato/otama-homelab/scanner-kiosk/docs/design/research/correction-fairscan-jvm-resume-v060');rows=[]
h=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
for kind in ['images','masks']:(src/'evaluation/dataset'/kind).mkdir(parents=True,exist_ok=True)
cat=json.loads((corpus/'catalog.json').read_text())
for f in cat['fixtures']:
 assert f['document_type']=='public-synthetic-known-renderer'
 image=corpus/'corpus'/f['relative_path'];assert h(image)==f['sha256']
 imagealias=src/'evaluation/dataset/images'/(f['id']+'.jpg');shutil.copyfile(image,imagealias)
 rawpath=masks/(f['id']+'-mask-raw.npy');raw=np.load(rawpath);assert raw.shape==(256,256) and np.isfinite(raw).all()
 mask=np.rint(np.clip(raw,0,1)*255).astype(np.uint8);maskout=src/'evaluation/dataset/masks'/(f['id']+'.png');Image.fromarray(mask).save(maskout)
 assert h(imagealias)==f['sha256'] and np.array_equal(np.asarray(Image.open(maskout)),mask)
 rows.append({'id':f['id'],'source_sha256':f['sha256'],'image_alias_sha256':h(imagealias),'image_alias':'PNG original bytes named jpg; OpenCV magic decoding; no recompression','diagnostic_raw_mask_sha256':h(rawpath),'mask_png_sha256':h(maskout),'mask_scope':'existing Pillow-normalized LiteRT diagnostic, official quantization clip/round only; not official full preprocessing parity'})
assert len(rows)==6
(out/'inputs.json').write_text(json.dumps({'fixtures':rows,'scope':'official quad stage on identity-bound previously diagnostic masks; not full pipeline quality'},indent=2)+'\n')
print('Six public synthetic pairs prepared, no new inference')
