import os,json,hashlib
from pathlib import Path
os.environ['OMP_NUM_THREADS']='2';os.environ['OPENBLAS_NUM_THREADS']='2';os.environ['MKL_NUM_THREADS']='2';os.sched_setaffinity(0,sorted(os.sched_getaffinity(0))[:2])
from config import Config
from data import get_validation_data
from torchvision.utils import save_image
r=Path('/home/yamato/otama-homelab/scanner-kiosk');w=Path('/tmp/scanner-fsenet-content-v060');(w/'baseline512').mkdir(exist_ok=True);base=Path('/tmp/scanner-fsenet-cpu-v060');c=Config(str(base/'run512/config.yml'));d=get_validation_data(c.TRAINING.VAL_DIR,{'w':c.TRAINING.PS_W,'h':c.TRAINING.PS_H,'ori':c.TRAINING.ORI});cases=json.loads((base/'cases.json').read_text());out=[]
for i,case in enumerate(cases):
 inp,tar,name=d[i];assert name==case['id'];p=w/'baseline512'/(name+'.png');save_image(inp,p);gt=base/'run512/result'/(name+'_gt.png');pred=base/'run512/result'/(name+'_pred.png');assert gt.exists() and pred.exists();out.append({**case,'baseline512':str(p),'baseline512_sha256':hashlib.sha256(p.read_bytes()).hexdigest(),'prediction512':str(pred),'prediction512_sha256':hashlib.sha256(pred.read_bytes()).hexdigest(),'official_target512':str(gt),'official_target512_sha256':hashlib.sha256(gt.read_bytes()).hexdigest()})
Path('/home/yamato/otama-homelab/scanner-kiosk/docs/design/research/correction-fsenet-content-v060/inputs.json').write_text(json.dumps(out,indent=2)+'\n');print(len(out))
