import json,hashlib
from pathlib import Path
import cv2,numpy as np
from PIL import Image
r=Path('/home/yamato/otama-homelab/scanner-kiosk');b=r/'docs/design/research/correction-docshadow-cpu-v060';m=json.loads((r/'docs/design/research/correction-paired-v060/paired-truth.json').read_text());out=[]
for case in m['rows']:
 ref=np.asarray(Image.open(case['clean']).convert('RGB'),dtype=np.float32)/255
 shadow=np.asarray(Image.open(case['shadow']).convert('RGB'),dtype=np.float32)/255
 row={'id':case['id'],'original_size':case['size'],'model_size':[512,512],'baseline_psnr_db':cv2.PSNR(shadow,ref,R=1.0)}
 for kind in ['shadow','clean']:
  raw=Path('/tmp/scanner-docshadow-v060/outputs')/(case['id']+'_'+kind+'.npy');pred=np.load(raw,allow_pickle=False)[0].transpose(1,2,0)
  # Exact official infer.py visualization resize; diagnostic float values remain unclipped.
  pred=cv2.resize(pred,(ref.shape[1],ref.shape[0]))
  row[kind+'_psnr_db']=cv2.PSNR(pred,ref,R=1.0)
  row[kind+'_channel_mae']=cv2.norm(pred,ref,cv2.NORM_L1)/ref.size
 shadow_resize=cv2.resize(np.asarray(Image.open(case['shadow']).convert('RGB').resize((512,512)),dtype=np.float32)/255,(ref.shape[1],ref.shape[0]))
 clean_resize=cv2.resize(np.asarray(Image.open(case['clean']).convert('RGB').resize((512,512)),dtype=np.float32)/255,(ref.shape[1],ref.shape[0]))
 row['resize_only_shadow_psnr_db']=cv2.PSNR(shadow_resize,ref,R=1.0)
 row['resize_only_clean_channel_mae']=cv2.norm(clean_resize,ref,cv2.NORM_L1)/ref.size
 row['shadow_gain_over_resize_only_db']=row['shadow_psnr_db']-row['resize_only_shadow_psnr_db']
 row['shadow_psnr_improvement_db']=row['shadow_psnr_db']-row['baseline_psnr_db'];row['shadow_psnr_threshold_pass']=row['shadow_psnr_improvement_db']>=1
 row['clean_mae_threshold_pass']=row['clean_channel_mae']<=1/255
 row['ssim']='not-measured: no installed standard SSIM implementation'
 out.append(row)
d={'scope':'512x512 official-CLI-resize synthetic diagnostic restored via official cv2.resize; not native-resolution inference, not FSENet, not full qualification','metric_implementation':'Installed OpenCV4.12.0 cv2.PSNR(R=1), cv2.norm(NORM_L1)/channel-count; no custom PSNR/SSIM implementation','clipping':'none, raw float outputs retained','thresholds':m['thresholds'],'rows':out};(b/'quality.json').write_text(json.dumps(d,indent=2)+'\n');print(json.dumps(out,indent=2))
