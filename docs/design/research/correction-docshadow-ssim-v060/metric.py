import os,sys,time,json,hashlib,resource,signal
from pathlib import Path
os.environ['OMP_NUM_THREADS']='2';os.environ['OPENBLAS_NUM_THREADS']='2';os.environ['MKL_NUM_THREADS']='2';allowed=sorted(os.sched_getaffinity(0));os.sched_setaffinity(0,allowed[:2]);signal.alarm(30)
import numpy as np
from PIL import Image
from skimage.metrics import structural_similarity
case=json.loads(sys.argv[1]);ref=np.asarray(Image.open(case['reference']).convert('RGB'),dtype=np.float32)/255
path=Path(case['input']);assert hashlib.sha256(path.read_bytes()).hexdigest()==case['input_sha256']
a=np.load(path,allow_pickle=False) if path.suffix=='.npy' else np.asarray(Image.open(path).convert('RGB'),dtype=np.float32)/255
if a.ndim==4:a=a[0].transpose(1,2,0)
assert a.shape==ref.shape and np.isfinite(a).all() and np.isfinite(ref).all()
params={'data_range':1.0,'channel_axis':2,'win_size':7,'gaussian_weights':False,'use_sample_covariance':True,'K1':0.01,'K2':0.03}
t=time.monotonic();value=float(structural_similarity(ref,a,**params));assert np.isfinite(value)
print(json.dumps({'id':case['id'],'ssim':value,'metric_seconds':time.monotonic()-t,'peak_rss_bytes':resource.getrusage(resource.RUSAGE_SELF).ru_maxrss*1024,'shape':list(a.shape),'parameters':params,'reference':case['reference'],'reference_sha256':hashlib.sha256(Path(case['reference']).read_bytes()).hexdigest(),'input':str(path),'input_sha256':case['input_sha256'],'clipping':'none','status':'measured'}))
