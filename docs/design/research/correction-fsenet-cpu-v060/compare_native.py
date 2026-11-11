import os,json,hashlib
from pathlib import Path
os.environ['OMP_NUM_THREADS']='2';os.environ['OPENBLAS_NUM_THREADS']='2';os.environ['MKL_NUM_THREADS']='2';os.sched_setaffinity(0,sorted(os.sched_getaffinity(0))[:2])
import numpy as np
from data import get_validation_data
from config import Config
r=Path('/home/yamato/otama-homelab/scanner-kiosk');b=r/'docs/design/research/correction-fsenet-cpu-v060';w=Path('/tmp/scanner-fsenet-cpu-v060');native=json.loads((b/'native-runs.json').read_text());old={x['source_id'] if 'source_id' in x else x['id']:x for x in json.loads((r/'docs/design/research/correction-docshadow-native-v060/runs.json').read_text())};inputs={x['id']:x for x in json.loads((b/'old-onnx-native-input-identities.json').read_text())};conf=Config(str(w/'config.yml'));dataset=get_validation_data(conf.TRAINING.VAL_DIR,{'w':conf.TRAINING.PS_W,'h':conf.TRAINING.PS_H,'ori':conf.TRAINING.ORI});out=[]
for i,x in enumerate(native):
 inp,tar,name=dataset[i];assert name==x['id'];a=inp.unsqueeze(0).numpy();h=hashlib.sha256(a.tobytes(order='C')).hexdigest();assert h==inputs[x['source_id']]['preprocessed_tensor_bytes_sha256'] and list(a.shape)==inputs[x['source_id']]['preprocessed_shape']
 y=old[x['source_id']];assert y['input_sha256']==x['source_sha256'];actual=np.load(x['raw_output'],allow_pickle=False);reference=np.load(y['raw_output'],allow_pickle=False);assert actual.shape==reference.shape and np.isfinite(actual).all() and np.isfinite(reference).all()
 try:np.testing.assert_allclose(actual,reference,rtol=1e-4,atol=1e-5);status='allclose-pass'
 except AssertionError:status='allclose-fail'
 out.append({'id':x['source_id'],'source_sha256':x['source_sha256'],'input_tensor_bytes_sha256':h,'input_tensor_shape':list(a.shape),'input_tensor_exact_match':True,'output_shape':list(actual.shape),'rtol':1e-4,'atol':1e-5,'max_abs_diff':float(np.max(np.abs(actual-reference))),'numpy_assert_allclose':status,'fsenet_raw_sha256':x['raw_output_sha256'],'onnx_raw_sha256':y['raw_output_sha256']})
(b/'matched-input-cpu-output-comparison.json').write_text(json.dumps({'scope':'same exact native RGB tensor CPU output comparison, current CPUfork vs author-linked ONNX; not artifact lineage or CUDA parity;512 excluded because preprocess differs','thresholds_fixed_before_comparison':{'rtol':1e-4,'atol':1e-5},'rows':out},indent=2)+'\n');print(len(out),sum(x['numpy_assert_allclose']=='allclose-pass' for x in out))
