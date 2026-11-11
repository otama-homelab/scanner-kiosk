import os,json,hashlib,signal,resource
from pathlib import Path
os.environ['OMP_NUM_THREADS']='2';os.environ['OPENBLAS_NUM_THREADS']='2';os.environ['MKL_NUM_THREADS']='2';os.sched_setaffinity(0,sorted(os.sched_getaffinity(0))[:2]);signal.alarm(180)
import torch
p=Path('/tmp/scanner-fsenet-cpu-v060/SD7K.pth');x=torch.load(p,map_location='cpu',weights_only=True);a=x['state_dict']['module.backbone.coefficient_1_0'].detach().numpy();assert a.shape==(2,3);rows=[]
for i in [0,1]:
 value=a[i,:][None,:,None,None]
 rows.append({'source_expression':f'coefficient_1_0[{i}, :][None, :, None, None]','row_index':i,'broadcast_shape':list(value.shape),'dtype':value.dtype.str,'bytes':value.nbytes,'raw_bytes_sha256':hashlib.sha256(value.tobytes(order='C')).hexdigest()})
b=Path('/home/yamato/otama-homelab/scanner-kiosk/docs/design/research/correction-fsenet-artifact-identity-v060');(b/'coefficient-rows.json').write_text(json.dumps({'scope':'Source-defined row selection/broadcast analysis only; runtime/checkpoint not modified','source_key':'module.backbone.coefficient_1_0','original_shape':list(a.shape),'original_bytes_sha256':hashlib.sha256(a.tobytes(order='C')).hexdigest(),'rows':rows,'peak_rss_bytes':resource.getrusage(resource.RUSAGE_SELF).ru_maxrss*1024},indent=2)+'\n');print(rows)
