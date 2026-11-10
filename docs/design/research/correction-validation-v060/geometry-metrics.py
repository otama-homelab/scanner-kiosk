from pathlib import Path
import sys,json,math
import numpy as np
import torch
import torch.nn.functional as F
p=Path('/tmp/scanner-v060');rows=[]
def pixel_grid(mesh,w,h):
 m=F.interpolate(mesh,size=(89,61),mode='bilinear',align_corners=True)[0].numpy();m[0]=(m[0]+1)*(w-1)/2;m[1]=(m[1]+1)*(h-1)/2;return m
def error(a,b,w,h):return float(np.sqrt(((a-b)**2).sum(axis=0)).mean()/math.hypot(w,h))
y,x=torch.meshgrid(torch.linspace(-1,1,89),torch.linspace(-1,1,61),indexing='ij');identity=torch.stack([x,y])[None]
known=pixel_grid(identity,61,89);assert error(known,known,61,89)==0 and np.allclose(known[:,0,0],[0,0]) and np.allclose(known[:,-1,-1],[60,88])
for r in json.loads((p/'truth-manifest.json').read_text())['rows']:
 d=p/('truth-'+r['id'])/'repetitions/001-cold';cat=json.loads((d/'artifact-catalog.json').read_text());a=next(a for a in cat['artifacts'] if a['role']=='generated');b=json.loads((d/'artifacts'/a['id']).read_text());m=torch.tensor(b['mesh'],dtype=torch.float32);gt=np.load(p/'uvdoc-subset/truth'/(r['id']+'-grid.npy'));w,h=r['width'],r['height'];pred=pixel_grid(m,w,h);noop=pixel_grid(identity,w,h)
 rows.append({'id':r['id'],'uvdoc_mean_error_fraction_diagonal':error(pred,gt,w,h),'noop_mean_error_fraction_diagonal':error(noop,gt,w,h),'uvdoc_max_error_fraction_diagonal':float(np.sqrt(((pred-gt)**2).sum(axis=0)).max()/math.hypot(w,h)),'gt_points':89*61,'scope':'dataset grid correspondence only; no OCR/digits/thin-line annotation; old ONNX comparison not included'})
print(json.dumps({'rows':rows,'coordinate_convention':'official grid2d HDF5 transpose, absolute source pixels; predicted normalized backward xy via align_corners_true to pixel(width-1,height-1); bilinear89x61 comparison; metric selfcheck passed','threshold_mean_error_fraction_diagonal':0.02},indent=2));assert len(rows)==3
