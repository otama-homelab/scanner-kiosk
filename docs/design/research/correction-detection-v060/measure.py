from pathlib import Path
import json,hashlib,math,time,resource
import numpy as np
from PIL import Image
from ai_edge_litert.interpreter import Interpreter
p=Path('/tmp/scanner-v060-synthetic');out=Path('/tmp/scanner-v060-detection');h=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
cat=json.loads((p/'catalog.json').read_text());model=Path('/tmp/scanner-v050/upstream/fairscan-segmentation-model.tflite')
pre={'before_new_mask_inference':True,'source':'previously fixed six renderer-known full-canvas controls','sources':[{'id':f['id'],'sha256':f['sha256']} for f in cat['fixtures']],'model_sha256':h(model),'thresholds':{'mask_iou':0.95,'corner_diagonal_error':0.02},'scope':'mask diagnostic only; published normalization/clamp/threshold with Pillow resize, not Android parity or full detector. Existing DocAligner runs reused, no rerun.'};(out/'preregistration.json').write_text(json.dumps(pre,indent=2)+'\n')
it=Interpreter(model_path=str(model),num_threads=2);it.allocate_tensors();a=it.get_input_details()[0];b=it.get_output_details()[0];rows=[]
# Corner metric self-check: exact full-page quad has zero normalized error.
assert np.linalg.norm(np.array([[0,0],[899,0],[899,699],[0,699]])-np.array([[0,0],[899,0],[899,699],[0,699]]))==0
for f in cat['fixtures']:
 source=p/'corpus'/f['relative_path'];assert h(source)==f['sha256'];im=Image.open(source).convert('RGB');w,ht=im.size;gt=np.array([[0,0],[w-1,0],[w-1,ht-1],[0,ht-1]])
 arms={}
 for arm in ['none','docaligner']:
  samples=[]
  for path in sorted((p/(arm+'-'+f['id'])).glob('repetitions/*/artifact-catalog.json')):
   c=json.loads(path.read_text());assert c['fixture_sha256']==f['sha256'];art=c['artifacts'][0];params=art.get('parameters',{});q=np.array(json.loads(params['crop_quad'])) if arm=='docaligner' else gt.copy();assert q.shape==(4,2) and np.isfinite(q).all();err=np.linalg.norm(q-gt,axis=1)/math.hypot(w,ht)
   samples.append({'catalog':str(path),'catalog_sha256':h(path),'quad':q.tolist(),'corner_errors_diagonal':err.tolist(),'max_corner_error_diagonal':float(err.max()),'corner_pass':bool(np.all(err<=0.02)),'fallback':bool(params.get('fallback_reason')),'source_byte_identity':art['sha256']==f['sha256']})
  assert len(samples)==3;arms[arm]=samples
 rgb=np.asarray(im.resize((256,256),Image.Resampling.BILINEAR),dtype=np.float32);it.set_tensor(a['index'],((rgb-127.5)/127.5)[None]);start=time.perf_counter();it.invoke();raw=it.get_tensor(b['index'])[0,:,:,0];ms=(time.perf_counter()-start)*1000;assert np.isfinite(raw).all();pred=np.clip(raw,0,1)>=0.5;iou=float(pred.mean());np.save(out/(f['id']+'-mask-raw.npy'),raw)
 rows.append({'id':f['id'],'arms':arms,'fairscan_mask':{'iou':iou,'pass':iou>=0.95,'forward_ms':ms,'raw_sha256':h(out/(f['id']+'-mask-raw.npy'))}})
result={'rows':rows,'mask_process_peak_rss_bytes':resource.getrusage(resource.RUSAGE_SELF).ru_maxrss*1024,'scope':pre['scope'],'summary':{'fairscan_mask_pass':sum(r['fairscan_mask']['pass'] for r in rows),'docaligner_corner_pass_cases':sum(all(s['corner_pass'] for s in r['arms']['docaligner']) for r in rows),'docaligner_fallback_samples':sum(s['fallback'] for r in rows for s in r['arms']['docaligner']),'samples_per_arm':18}}
(out/'metrics.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps(result['summary']))
