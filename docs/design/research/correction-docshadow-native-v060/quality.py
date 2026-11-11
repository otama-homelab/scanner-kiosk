import json,hashlib
from pathlib import Path
import cv2,numpy as np
from PIL import Image
r=Path('/home/yamato/otama-homelab/scanner-kiosk');b=r/'docs/design/research/correction-docshadow-native-v060';manifest=json.loads((r/'docs/design/research/correction-paired-v060/paired-truth.json').read_text());runs={x['id']:x for x in json.loads((b/'runs.json').read_text())};prior={x['id']:x for x in json.loads((r/'docs/design/research/correction-docshadow-cpu-v060/quality.json').read_text())['rows']};out=[]
for case in manifest['rows']:
 ref=np.asarray(Image.open(case['clean']).convert('RGB'),dtype=np.float32)/255;shadow=np.asarray(Image.open(case['shadow']).convert('RGB'),dtype=np.float32)/255
 row={'id':case['id'],'native_size':case['size'],'shadow_noop_psnr_db':cv2.PSNR(shadow,ref,R=1.0),'clean_noop_channel_mae':0,'previous_512_shadow_psnr_db':prior[case['id']]['shadow_psnr_db'],'previous_512_clean_channel_mae':prior[case['id']]['clean_channel_mae']}
 for kind in ['shadow','clean']:
  run=runs[case['id']+'_'+kind]
  if run['status']!='measured': row[kind+'_status']=run['status'];continue
  raw=Path(run['raw_output']);assert hashlib.sha256(raw.read_bytes()).hexdigest()==run['raw_output_sha256'];pred=np.load(raw,allow_pickle=False)[0].transpose(1,2,0)
  assert pred.shape==ref.shape and np.isfinite(pred).all();row[kind+'_status']='measured';row[kind+'_psnr_db']=cv2.PSNR(pred,ref,R=1.0);row[kind+'_channel_mae']=cv2.norm(pred,ref,cv2.NORM_L1)/ref.size
 if row['shadow_status']=='measured':
  row['shadow_psnr_improvement_db']=row['shadow_psnr_db']-row['shadow_noop_psnr_db'];row['shadow_psnr_threshold_pass']=row['shadow_psnr_improvement_db']>=1;row['native_vs512_shadow_psnr_delta_db']=row['shadow_psnr_db']-row['previous_512_shadow_psnr_db']
 if row['clean_status']=='measured':row['clean_mae_threshold_pass']=row['clean_channel_mae']<=1/255;row['native_vs512_clean_mae_delta']=row['clean_channel_mae']-row['previous_512_clean_channel_mae']
 out.append(row)
d={'scope':'Native-resolution OLD DocShadow synthetic-only, exact same paired pixels; no resizing/clipping of input/output; not FSENet or conversion parity.','metric_implementation':'Installed OpenCV4.12.0 cv2.PSNR(R=1), cv2.norm(NORM_L1)/channel-count.','ssim':'not-measured: standard implementations absent in four existing evaluation environments; no new dependency or formula','thresholds':manifest['thresholds'],'rows':out};(b/'quality.json').write_text(json.dumps(d,indent=2)+'\n');print(json.dumps(out,indent=2))
