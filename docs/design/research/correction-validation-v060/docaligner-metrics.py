from pathlib import Path
import json,math
import numpy as np
p=Path('/tmp/scanner-v060');rows=[]
for r in json.loads((p/'truth-manifest.json').read_text())['rows']:
 d=p/('docaligner-truth-'+r['id']);j=json.loads((d/'benchmark.json').read_text());w,h=r['width'],r['height'];g=np.load(p/'uvdoc-subset/truth'/(r['id']+'-grid.npy'));corners=np.stack([g[:,0,0],g[:,0,-1],g[:,-1,-1],g[:,-1,0]])
 params=[]
 for f in sorted(d.glob('repetitions/*/artifact-catalog.json')):
  c=json.loads(f.read_text());a=c['artifacts'][0];assert a['sha256']==c['fixture_sha256'];assert a['parameters']['fallback_reason']=='unsafe-or-missing-corners';params.append(a['parameters'])
 q=np.array(json.loads(params[0]['crop_quad']));err=np.linalg.norm(q-corners,axis=1)/math.hypot(w,h);mask=np.load(p/'uvdoc-subset/truth'/(r['id']+'-mask256.npy'));rows.append({'id':r['id'],'fallback_samples':len(params),'total_samples':len(params),'output_byte_identical_to_source':True,'full_image_noop_mask_iou':float(mask.mean()),'corner_error_fraction_diagonal':err.tolist(),'corner_mean_error_fraction_diagonal':float(err.mean()),'resource_metrics_qualified':False,'resource_note':'overlapped automated regression; timings/RSS not used for qualification'})
assert len(rows)==3
print(json.dumps({'rows':rows,'scope':'existing complete DocAligner path on3additional UVDoc cases; all fallback to no-op. Does not satisfy original6photo annotations/content requirements.'},indent=2))
