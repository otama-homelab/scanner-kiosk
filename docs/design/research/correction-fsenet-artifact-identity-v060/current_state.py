import os,json,hashlib,time,resource,signal
from pathlib import Path
os.environ['OMP_NUM_THREADS']='2';os.environ['OPENBLAS_NUM_THREADS']='2';os.environ['MKL_NUM_THREADS']='2';os.sched_setaffinity(0,sorted(os.sched_getaffinity(0))[:2]);signal.alarm(180)
import torch,numpy as np
torch.set_num_threads(2);torch.set_num_interop_threads(1)
p=Path('/tmp/scanner-fsenet-cpu-v060/SD7K.pth');assert hashlib.sha256(p.read_bytes()).hexdigest()=='6a514e34cf2d3aff12eebd0c4f7b04466f47c1c0c41019d81091cc4d2defdc56';t=time.monotonic();x=torch.load(p,map_location='cpu',weights_only=True);rows=[]
for key,v in x['state_dict'].items():
 assert isinstance(v,torch.Tensor) and v.device.type=='cpu';a=v.detach().numpy();loadname=key[7:] if key.startswith('module') else key
 rows.append({'original_key':key,'official_loader_key':loadname,'shape':list(a.shape),'dtype':a.dtype.str,'bytes':a.nbytes,'raw_bytes_sha256':hashlib.sha256(a.tobytes(order='C')).hexdigest()})
out={'checkpoint':str(p),'checkpoint_sha256':hashlib.sha256(p.read_bytes()).hexdigest(),'safe_weights_only':True,'torch':torch.__version__,'rows':rows,'elapsed_seconds':time.monotonic()-t,'peak_rss_bytes':resource.getrusage(resource.RUSAGE_SELF).ru_maxrss*1024};Path('/home/yamato/otama-homelab/scanner-kiosk/docs/design/research/correction-fsenet-artifact-identity-v060/current-state.json').write_text(json.dumps(out,indent=2)+'\n');print(len(rows))
