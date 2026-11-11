import os,sys,json,time,hashlib,subprocess,importlib.util
from pathlib import Path
import numpy as np,cv2
from PIL import Image
r=Path('/home/yamato/otama-homelab/scanner-kiosk');b=r/'docs/design/research/correction-docshadow-ocr-v060';tmp=Path('/tmp/scanner-docshadow-ocr-v060');(tmp/'images').mkdir(exist_ok=True)
spec=importlib.util.spec_from_file_location('existing_ocr_candidate',r/'scripts/eval/ocr/candidate.py');adapter=importlib.util.module_from_spec(spec);spec.loader.exec_module(adapter)
manifest=json.loads((r/'docs/design/research/correction-paired-v060/paired-truth.json').read_text());native={x['id']:x for x in json.loads((r/'docs/design/research/correction-docshadow-native-v060/runs.json').read_text())};small={x['id']:x for x in json.loads((r/'docs/design/research/correction-docshadow-cpu-v060/runs.json').read_text())}
lock=json.loads(Path('/home/yamato/.local/share/scanner46-ocr-85v1b2rc/tesseract-5.5.4-runtime.json').read_text());oraclefile=r/'scripts/fixtures/evaluation/ocr-oracles.json';oracles=json.loads(oraclefile.read_text());entries=oracles['entries'];lookup={x['fixture_id']:x for x in entries};rows=[];score=[]
env=dict(os.environ);env.update({'OMP_THREAD_LIMIT':'2','OMP_NUM_THREADS':'2','OPENBLAS_NUM_THREADS':'2'});allowed=sorted(os.sched_getaffinity(0));os.sched_setaffinity(0,allowed[:2])
for c in manifest['rows']:
 truth=lookup.get('fixture_ocr_'+c['id'].removeprefix('faint_')+'_001') if c['id'] not in ['blue','peach'] else None
 if truth and not c['id'].startswith('faint_'):assert truth['fixture_sha256']==c['clean_sha256']
 for kind in ['clean','shadow']:
  source=Path(c[kind]);assert hashlib.sha256(source.read_bytes()).hexdigest()==c[kind+'_sha256'];W,H=c['size']
  for arm in ['noop','native','512']:
   key=c['id']+'_'+kind+'_'+arm;conversion=None
   if arm=='noop':imagepath=source
   else:
    run=(native if arm=='native' else small)[c['id']+'_'+kind];npypath=Path(run['raw_output']);assert hashlib.sha256(npypath.read_bytes()).hexdigest()==run['raw_output_sha256'];a=np.load(npypath,allow_pickle=False)[0].transpose(1,2,0)
    if arm=='512':a=cv2.resize(a,(W,H))
    assert a.shape==(H,W,3) and np.isfinite(a).all()
    imagepath=tmp/'images'/(key+'.png');pixels=cv2.cvtColor(a,cv2.COLOR_RGB2BGR)*255;assert cv2.imwrite(str(imagepath),pixels)
    conversion={'algorithm':'official visualization cv2.resize when512 + cv2.cvtColor; standard OpenCV PNG float->8bit saturating encoding after255scale (official script has display only, no PNG exporter)','out_of_unit_interval_channels':int(np.count_nonzero((a<0)|(a>1))),'channel_count':int(a.size),'raw_npy_sha256':run['raw_output_sha256']}
   cmd=[lock['tesseract'],str(imagepath),'stdout','--tessdata-dir',lock['tessdata'],'-l','jpn+eng','--psm','1','tsv'];t=time.monotonic()
   try:proc=subprocess.run(cmd,capture_output=True,timeout=30,env=env);rc=proc.returncode;out=proc.stdout;err=proc.stderr
   except subprocess.TimeoutExpired as e:rc='timeout';out=e.stdout or b'';err=e.stderr or b''
   (b/(key+'.tsv')).write_bytes(out);(b/(key+'.stderr.log')).write_bytes(err)
   row={'id':key,'fixture':c['id'],'source_kind':kind,'arm':arm,'command':cmd,'returncode':rc,'elapsed_seconds':time.monotonic()-t,'image_sha256':hashlib.sha256(imagepath.read_bytes()).hexdigest(),'source_sha256':c[kind+'_sha256'],'tsv_sha256':hashlib.sha256(out).hexdigest(),'conversion':conversion,'reference_status':'renderer-text-preserved' if truth else 'no-transcription: imagegen pixels not text truth'}
   if rc==0:
    spans=adapter.parse_tsv(out.decode(),W,H);text=''.join(x['text'] for x in spans);(b/(key+'.txt')).write_text(text);row['text_sha256']=hashlib.sha256(text.encode()).hexdigest();row['recognized_codepoints']=len(text);row['span_count']=len(spans)
    if truth:
     page=truth['pages'][0];assert page['width']==W and page['height']==H
     score.append({'id':key,'oracle':page,'text':text,'positions':{'page_id':page['page_id'],'width':W,'height':H,'spans':spans}})
   rows.append(row);(b/'runs.json').write_text(json.dumps(rows,indent=2)+'\n');print(key,rc,flush=True)
(tmp/'score-input.json').write_text(json.dumps(score,ensure_ascii=False));print('SCORE',len(score))
