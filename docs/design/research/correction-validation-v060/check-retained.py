import json,hashlib,sys
from pathlib import Path
import numpy as np
import torch
from PIL import Image
from transformers.models.uvdoc.image_processing_uvdoc import UVDocImageProcessor
sys.path.insert(0,'scripts/eval/correction')
from uvdoc_mesh_candidate import validate_mesh,MODEL_FILES,MODEL_CODE,PROCESSOR_CODE
h=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
checks=[]
for f in sorted(f for root in sys.argv[1:] for f in Path(root).glob('repetitions/*/artifact-catalog.json')):
 c=json.loads(f.read_text()); a={x['role']:x for x in c['artifacts']}; art=f.parent/'artifacts'; mp=art/a['generated']['id']; op=art/a['correction']['id']; source=art/c['original']['id']; b=json.loads(mp.read_text());m=np.array(b['mesh'],dtype='<f4');validate_mesh(m)
 assert list(m.shape)==b['mesh_shape'];assert hashlib.sha256(m.tobytes()).hexdigest()==b['mesh_sha256'];assert h(mp)==a['generated']['sha256'];assert h(op)==b['output_sha256']==a['correction']['sha256'];assert h(source)==b['source_sha256']==c['fixture_sha256'];assert b['model_files']==MODEL_FILES and b['model_code_sha256']==MODEL_CODE and b['processor_code_sha256']==PROCESSOR_CODE;assert b['apply_eligible'] is False
 processor=UVDocImageProcessor();inputs=processor(images=Image.open(source).convert('RGB'),return_tensors='pt')
 with torch.inference_mode(): actual=processor.post_process_document_rectification(torch.from_numpy(m),inputs['original_images'])[0]['images'].numpy()
 np.testing.assert_array_equal(actual,np.asarray(Image.open(op)));checks.append({'catalog':str(f),'mesh_sha256':b['mesh_sha256'],'sampling_exact':True})
assert checks
print(json.dumps(checks,indent=2))
