import os,json,time,signal,resource,hashlib
from pathlib import Path
os.environ['OMP_NUM_THREADS']='2';os.environ['OPENBLAS_NUM_THREADS']='2';os.environ['MKL_NUM_THREADS']='2';os.sched_setaffinity(0,sorted(os.sched_getaffinity(0))[:2]);signal.alarm(180)
import torch
torch.set_num_threads(2);torch.set_num_interop_threads(1);t=time.monotonic();p=Path('/tmp/scanner-fsenet-cpu-v060/SD7K.pth');d=torch.load(p,map_location='cpu',weights_only=True);assert isinstance(d,dict) and isinstance(d.get('state_dict'),dict)
print(json.dumps({'status':'safe-load-pass','torch':torch.__version__,'torch_cuda_version':torch.version.cuda,'load_seconds':time.monotonic()-t,'peak_rss_bytes':resource.getrusage(resource.RUSAGE_SELF).ru_maxrss*1024,'model_sha256':hashlib.sha256(p.read_bytes()).hexdigest(),'top_level_keys':list(d),'state_dict_count':len(d['state_dict']),'first_state_keys':list(d['state_dict'])[:10],'cpu_only':all(not v.is_cuda for v in d['state_dict'].values() if isinstance(v,torch.Tensor)),'weights_only':True,'map_location':'cpu'}))
