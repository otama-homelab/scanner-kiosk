import os,sys,time,json,hashlib,signal,resource
from pathlib import Path
os.environ['OMP_NUM_THREADS']='2'; os.environ['OPENBLAS_NUM_THREADS']='2'; os.environ['MKL_NUM_THREADS']='2'
allowed=sorted(os.sched_getaffinity(0));os.sched_setaffinity(0,allowed[:2])
import numpy as np
import onnxruntime as ort
from PIL import Image
from onnx_runner import DocShadowRunner
case=json.loads(sys.argv[1]); shape=int(sys.argv[2]);signal.alarm(30)
t=time.monotonic();opts=ort.SessionOptions();opts.intra_op_num_threads=2;opts.inter_op_num_threads=1
runner=DocShadowRunner.__new__(DocShadowRunner)
runner.model=ort.InferenceSession('/tmp/scanner-docshadow-v060/docshadow_sd7k.onnx',sess_options=opts,providers=['CPUExecutionProvider']);load=time.monotonic()-t
im=Image.open(case['path']).convert('RGB');original=list(im.size)
# Official infer.py resizes via Pillow before unchanged DocShadowRunner.preprocess.
target=(shape,shape) if shape else im.size
a=DocShadowRunner.preprocess(np.array(im.resize(target)))
out={'id':case['id'],'input':case['path'],'input_sha256':hashlib.sha256(Path(case['path']).read_bytes()).hexdigest(),'original_size':original,'model_size':list(target),'scope':'official CLI resize diagnostic, not native-resolution model quality','load_seconds':load,'providers':runner.model.get_providers(),'input_spec':[(x.name,x.shape,x.type) for x in runner.model.get_inputs()],'timing_seconds':[]}
for k in range(3):
 signal.alarm(30);t=time.monotonic();res=runner.run(a);out['timing_seconds'].append(time.monotonic()-t);signal.alarm(0)
 assert res.shape==(1,3,target[1],target[0]) and np.isfinite(res).all()
path=Path('/tmp/scanner-docshadow-v060/outputs')/(case['id']+('_native' if not shape else '')+'.npy');path.parent.mkdir(exist_ok=True);np.save(path,res,allow_pickle=False)
out.update({'output_shape':list(res.shape),'output_range':[float(res.min()),float(res.max())],'raw_output':str(path),'raw_output_sha256':hashlib.sha256(path.read_bytes()).hexdigest(),'peak_rss_bytes':resource.getrusage(resource.RUSAGE_SELF).ru_maxrss*1024,'status':'measured','intra_threads':2,'inter_threads':1,'cpu_affinity':sorted(os.sched_getaffinity(0))})
print(json.dumps(out))
