import os,json,hashlib,signal
from pathlib import Path
os.environ['OMP_NUM_THREADS']='2';os.environ['OPENBLAS_NUM_THREADS']='2';os.environ['MKL_NUM_THREADS']='2';os.sched_setaffinity(0,sorted(os.sched_getaffinity(0))[:2]);signal.alarm(180)
import onnx
from onnx import numpy_helper
b=Path('/home/yamato/otama-homelab/scanner-kiosk/docs/design/research/correction-fsenet-artifact-identity-v060');model=onnx.load('/tmp/scanner-docshadow-v060/docshadow_sd7k.onnx',load_external_data=False);initializers={x.name:x for x in model.graph.initializer};rows=json.loads((b/'coefficient-rows.json').read_text())['rows'];mapping=[]
# Source explicitly uses row0 then row1 as the two final multipliers; no fitted permutation.
for row,name in zip(rows,['onnx::Mul_12850','onnx::Mul_12854']):
 a=numpy_helper.to_array(initializers[name]);sha=hashlib.sha256(a.tobytes(order='C')).hexdigest();consumers=[{'name':n.name,'op_type':n.op_type,'inputs':list(n.input),'outputs':list(n.output)} for n in model.graph.node if name in n.input];assert len(consumers)==1 and consumers[0]['op_type']=='Mul';assert list(a.shape)==row['broadcast_shape'] and a.dtype.str==row['dtype'] and sha==row['raw_bytes_sha256'];mapping.append({'source_expression':row['source_expression'],'initializer':name,'shape':list(a.shape),'dtype':a.dtype.str,'bytes':a.nbytes,'raw_bytes_sha256':sha,'source_row_byte_match':True,'consumers':consumers})
outputs={o for x in mapping for n in x['consumers'] for o in n['outputs']};combining=[{'name':n.name,'op_type':n.op_type,'inputs':list(n.input),'outputs':list(n.output)} for n in model.graph.node if len(set(n.input)&outputs)==2];assert len(combining)==1 and combining[0]['op_type']=='Add'
(b/'folded-coefficient-identity.json').write_text(json.dumps({'scope':'Explicit source row0/row1 broadcast equals two graph constants byte-for-byte and graph Mul/Add consumption. No runtime transformation, fitting, exporter, or inference.','mapping':mapping,'combining_nodes':combining},indent=2)+'\n');print(mapping,combining)
