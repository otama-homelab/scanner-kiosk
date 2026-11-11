import json,hashlib
from pathlib import Path
import cv2,numpy as np
r=Path('/home/yamato/otama-homelab/scanner-kiosk');w=Path('/tmp/scanner-docshadow-ssim-v060');(w/'resized').mkdir(exist_ok=True);m=json.loads((r/'docs/design/research/correction-paired-v060/paired-truth.json').read_text());native={x['id']:x for x in json.loads((r/'docs/design/research/correction-docshadow-native-v060/runs.json').read_text())};small={x['id']:x for x in json.loads((r/'docs/design/research/correction-docshadow-cpu-v060/runs.json').read_text())};cases=[]
for x in m['rows']:
 ref=Path(x['clean']);assert hashlib.sha256(ref.read_bytes()).hexdigest()==x['clean_sha256']
 for kind in ['clean','shadow']:
  original=Path(x[kind]);assert hashlib.sha256(original.read_bytes()).hexdigest()==x[kind+'_sha256']
  for arm in ['noop','native','512']:
   id=x['id']+'_'+kind+'_'+arm
   if arm=='noop':p=original
   else:
    a=(native if arm=='native' else small)[x['id']+'_'+kind];p=Path(a['raw_output']);assert hashlib.sha256(p.read_bytes()).hexdigest()==a['raw_output_sha256']
    if arm=='512':
     data=np.load(p,allow_pickle=False)[0].transpose(1,2,0);data=cv2.resize(data,tuple(x['size']));p=w/'resized'/(id+'.npy');np.save(p,data,allow_pickle=False)
   cases.append({'id':id,'reference':str(ref),'input':str(p),'input_sha256':hashlib.sha256(p.read_bytes()).hexdigest(),'arm':arm,'source_kind':kind,'fixture':x['id']})
(w/'cases.json').write_text(json.dumps(cases,indent=2)+'\n');print(len(cases))
