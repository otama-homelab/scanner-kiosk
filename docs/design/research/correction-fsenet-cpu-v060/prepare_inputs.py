import json,hashlib,shutil
from pathlib import Path
import yaml
r=Path('/home/yamato/otama-homelab/scanner-kiosk');w=Path('/tmp/scanner-fsenet-cpu-v060');m=json.loads((r/'docs/design/research/correction-paired-v060/paired-truth.json').read_text());p=w/'dataset';(p/'input').mkdir(parents=True,exist_ok=True);(p/'target').mkdir(exist_ok=True);items=[]
for row in m['rows']:
 for kind in ['shadow','clean']:
  name=f'{len(items):02d}_{row["id"]}_{kind}.png';src=Path(row[kind]);ref=Path(row['clean']);assert hashlib.sha256(src.read_bytes()).hexdigest()==row[kind+'_sha256'] and hashlib.sha256(ref.read_bytes()).hexdigest()==row['clean_sha256'];shutil.copyfile(src,p/'input'/name);shutil.copyfile(ref,p/'target'/name);items.append({'id':name[:-4],'source_id':row['id']+'_'+kind,'source_kind':kind,'fixture':row['id'],'source':str(src),'source_sha256':row[kind+'_sha256'],'reference':str(ref),'reference_sha256':row['clean_sha256'],'size':row['size'],'filename':name})
conf=yaml.safe_load((w/'source/config.yml').read_text());conf['TRAINING']['VAL_DIR']=str(p);conf['TRAINING']['ORI']=True;conf['TESTING']['WEIGHT']=str(w/'SD7K.pth');(w/'config.yml').write_text(yaml.safe_dump(conf));(w/'cases.json').write_text(json.dumps(items,indent=2)+'\n');print(len(items))
