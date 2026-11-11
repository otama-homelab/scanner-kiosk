import os,time,json,signal,resource,hashlib
from pathlib import Path
os.environ['OMP_NUM_THREADS']='2';os.environ['OPENBLAS_NUM_THREADS']='2';os.environ['MKL_NUM_THREADS']='2';os.environ['ACCELERATE_USE_CPU']='true';os.sched_setaffinity(0,sorted(os.sched_getaffinity(0))[:2]);signal.alarm(180)
import torch,numpy as np
torch.set_num_threads(2);torch.set_num_interop_threads(1)
import infer as upstream
w=Path('/tmp/scanner-fsenet-cpu-v060');cases=json.loads((w/'cases.json').read_text());b=Path('/home/yamato/otama-homelab/scanner-kiosk/docs/design/research/correction-fsenet-cpu-v060');(w/'raw512').mkdir(exist_ok=True);rows=[];clock={}
def pre(module,args):
 if type(module) is upstream.Model:
  signal.alarm(30);clock['start']=time.monotonic();clock['input_shape']=list(args[0].shape)
def post(module,args,output):
 if type(module) is upstream.Model:
  signal.alarm(0);seconds=time.monotonic()-clock['start'];case=cases[len(rows)];raw=w/'raw512'/(case['id']+'.npy');a=output.detach().cpu().numpy();assert np.isfinite(a).all();np.save(raw,a,allow_pickle=False);rows.append({**case,'status':'measured','forward_seconds':seconds,'input_shape':clock['input_shape'],'output_shape':list(a.shape),'output_range':[float(a.min()),float(a.max())],'raw_output':str(raw),'raw_output_sha256':hashlib.sha256(raw.read_bytes()).hexdigest(),'parent_peak_rss_bytes':resource.getrusage(resource.RUSAGE_SELF).ru_maxrss*1024,'device':str(output.device)});(b/'resized512-runs.json').write_text(json.dumps(rows,indent=2)+'\n');print('SCANNER_FORWARD_RECEIPT',case['id'],seconds,flush=True);signal.alarm(180)
# Standard instrumentation hooks return None: they neither replace inputs nor outputs.
a=torch.nn.modules.module.register_module_forward_pre_hook(pre);c=torch.nn.modules.module.register_module_forward_hook(post)
try:upstream.infer()
finally:a.remove();c.remove()
assert len(rows)==len(cases)
print('SCANNER_CORPUS_COMPLETE',len(rows),flush=True)
