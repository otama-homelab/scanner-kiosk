import json,hashlib
from pathlib import Path
import h5py,numpy as np
from PIL import Image
p=Path('/tmp/scanner-v060/uvdoc-subset');out=p/'truth';out.mkdir(exist_ok=True);rows=[]
for s in sorted((p/'metadata_sample').glob('*.json')):
 m=json.loads(s.read_text());image=p/'img'/(s.stem+'.png');w,h=Image.open(image).size
 with h5py.File(p/'grid2d'/(m['geom_name']+'.mat'),'r') as f:g=f['grid2d'][:].T.transpose(2,0,1)
 assert g.shape==(2,89,61) and np.isfinite(g).all()
 with h5py.File(p/'seg'/(m['geom_name']+'.mat'),'r') as f:mask=f['seg'][:].T
 assert mask.shape==(h,w) and set(np.unique(mask))<={0,1}
 np.save(out/(s.stem+'-grid.npy'),g);np.save(out/(s.stem+'-mask256.npy'),np.asarray(Image.fromarray(mask).resize((256,256),Image.Resampling.NEAREST)))
 rows.append({'id':s.stem,'source_sha256':hashlib.sha256(image.read_bytes()).hexdigest(),'width':w,'height':h,'grid_shape':list(g.shape),'truth':'dataset-supplied independent grid2d/seg, loader orientation from official data_UVDoc.py; pixel coordinates; no content transcription','split':'diagnostic-only-first3'})
Path('/tmp/scanner-v060/truth-manifest.json').write_text(json.dumps({'rows':rows,'metric':'mean Euclidean pixel correspondence error / image diagonal; bilinear align_corners interpolation to89x61; no-op identity baseline. FairScan256IoU uses nearest-neighbor GT and published clamp/0.5; Pillow resize not Android parity.','content_truth_complete':False},indent=2))
assert len(rows)==3
