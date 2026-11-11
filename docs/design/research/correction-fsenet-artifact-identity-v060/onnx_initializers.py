import os,json,hashlib,time,resource,signal
from pathlib import Path
os.environ['OMP_NUM_THREADS']='2';os.environ['OPENBLAS_NUM_THREADS']='2';os.environ['MKL_NUM_THREADS']='2';os.sched_setaffinity(0,sorted(os.sched_getaffinity(0))[:2]);signal.alarm(180)
import onnx,numpy as np
from onnx import numpy_helper
p=Path('/tmp/scanner-docshadow-v060/docshadow_sd7k.onnx');assert hashlib.sha256(p.read_bytes()).hexdigest()=='8c09b9320a0fb3c53806cdf9cb8410b3706c77caed81f4ec1cb0bf2cbd14b049';t=time.monotonic();model=onnx.load(p,load_external_data=False);rows=[]
for tensor in model.graph.initializer:
 assert not tensor.external_data and tensor.data_location!=onnx.TensorProto.EXTERNAL;a=numpy_helper.to_array(tensor)
 rows.append({'name':tensor.name,'shape':list(a.shape),'dtype':a.dtype.str,'bytes':a.nbytes,'raw_bytes_sha256':hashlib.sha256(a.tobytes(order='C')).hexdigest()})
out={'model':str(p),'model_sha256':hashlib.sha256(p.read_bytes()).hexdigest(),'onnx':onnx.__version__,'numpy':np.__version__,'ir_version':model.ir_version,'opsets':[{'domain':x.domain,'version':x.version} for x in model.opset_import],'node_count':len(model.graph.node),'rows':rows,'elapsed_seconds':time.monotonic()-t,'peak_rss_bytes':resource.getrusage(resource.RUSAGE_SELF).ru_maxrss*1024};Path('/home/yamato/otama-homelab/scanner-kiosk/docs/design/research/correction-fsenet-artifact-identity-v060/onnx-initializers.json').write_text(json.dumps(out,indent=2)+'\n');print(len(rows))
