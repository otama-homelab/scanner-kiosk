import json,hashlib,sys
from pathlib import Path
import numpy as np
from PIL import Image
sys.path.insert(0,'/tmp/scanner-docshadow-v060')
from onnx_runner import DocShadowRunner
b=Path('/home/yamato/otama-homelab/scanner-kiosk/docs/design/research/correction-fsenet-cpu-v060');runs=json.loads((b/'native-runs.json').read_text());rows=[]
for x in runs:
 p=Path(x['source']);im=Image.open(p).convert('RGB');a=DocShadowRunner.preprocess(np.array(im.resize(im.size)));rows.append({'id':x['source_id'],'source_sha256':hashlib.sha256(p.read_bytes()).hexdigest(),'preprocessed_shape':list(a.shape),'dtype':str(a.dtype),'preprocessed_tensor_bytes_sha256':hashlib.sha256(a.tobytes(order='C')).hexdigest()})
(b/'old-onnx-native-input-identities.json').write_text(json.dumps(rows,indent=2)+'\n');print(len(rows))
