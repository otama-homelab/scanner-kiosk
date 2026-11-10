import time,json,resource
from pathlib import Path
import numpy as np
from PIL import Image
from ai_edge_litert.interpreter import Interpreter
p=Path('/tmp/scanner-v060');start=time.perf_counter();it=Interpreter(model_path='/tmp/scanner-v050/upstream/fairscan-segmentation-model.tflite',num_threads=2);it.allocate_tensors();a=it.get_input_details()[0];b=it.get_output_details()[0];rows=[]
for r in json.loads((p/'truth-manifest.json').read_text())['rows']:
 tick=time.perf_counter();rgb=np.asarray(Image.open(p/'uvdoc-subset/img'/(r['id']+'.png')).convert('RGB').resize((256,256),Image.Resampling.BILINEAR),dtype=np.float32);x=(rgb-127.5)/127.5;it.set_tensor(a['index'],x[None]);fw=time.perf_counter();it.invoke();raw=it.get_tensor(b['index'])[0,:,:,0];pred=np.clip(raw,0,1)>=0.5;gt=np.load(p/'uvdoc-subset/truth'/(r['id']+'-mask256.npy')).astype(bool);intersection=int(np.logical_and(pred,gt).sum());union=int(np.logical_or(pred,gt).sum());assert union>0 and np.isfinite(raw).all();rows.append({'id':r['id'],'iou':intersection/union,'raw_range':[float(raw.min()),float(raw.max())],'forward_ms':(time.perf_counter()-fw)*1000,'decode_preprocess_forward_ms':(time.perf_counter()-tick)*1000,'threshold':0.5});np.save(p/('fairscan-'+r['id']+'-raw.npy'),raw)
print(json.dumps({'rows':rows,'rss_bytes':resource.getrusage(resource.RUSAGE_SELF).ru_maxrss*1024,'whole_process_ms':(time.perf_counter()-start)*1000,'scope':'mask diagnostic only; published normalization/clamp/threshold; Pillow bilinear not verified Android ResizeOp parity; not complete detector or product pass'},indent=2))
assert len(rows)==3
