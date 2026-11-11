from pathlib import Path
import json,hashlib
import cv2,numpy as np
from PIL import Image
b=Path('/home/yamato/otama-homelab/scanner-kiosk/docs/design/research/correction-fsenet-content-v060');inputs={x['source_id']:x for x in json.loads((b/'inputs.json').read_text())};mapped=json.loads((b/'mapped-regions.json').read_text());rows=[]
for region in mapped:
 c=inputs[region['source_id']];box=region['mapped_crop_box'];ref=np.asarray(Image.open(c['official_target512']).convert('RGB').crop(tuple(box)),dtype=np.float32)/255
 for arm,key in [('noop512','baseline512'),('fsenet512','prediction512')]:
  pred=np.asarray(Image.open(c[key]).convert('RGB').crop(tuple(box)),dtype=np.float32)/255;assert pred.shape==ref.shape and pred.size>0;rows.append({**region,'arm':arm,'crop_shape':list(pred.shape),'psnr_db':cv2.PSNR(pred,ref,R=1.0),'channel_mae':cv2.norm(pred,ref,cv2.NORM_L1)/ref.size})
(b/'roi-metrics.json').write_text(json.dumps({'scope':'Pillow standard crop at existing MapSourcePoint coordinates, official512GT/prediction decoded8bit PNG; no semantic missing count inferred','metric':'existing OpenCV4.12 PSNR(R=1) and norm(L1)/channelcount, no new thresholds','rows':rows},indent=2)+'\n');print(len(rows))
