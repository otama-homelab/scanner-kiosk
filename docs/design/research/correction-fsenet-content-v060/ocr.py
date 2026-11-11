import os,sys,json,hashlib,subprocess,time,importlib.util
from pathlib import Path
r=Path('/home/yamato/otama-homelab/scanner-kiosk');b=r/'docs/design/research/correction-fsenet-content-v060';w=Path('/tmp/scanner-fsenet-content-v060');data=json.loads((b/'inputs.json').read_text());entries={x['fixture_id']:x for x in json.loads((r/'scripts/fixtures/evaluation/ocr-oracles.json').read_text())['entries']};spec=importlib.util.spec_from_file_location('existing_ocr_candidate',r/'scripts/eval/ocr/candidate.py');m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m);lock=json.loads(Path('/home/yamato/.local/share/scanner46-ocr-85v1b2rc/tesseract-5.5.4-runtime.json').read_text());env=dict(os.environ);env.update({'OMP_THREAD_LIMIT':'2','OMP_NUM_THREADS':'2'});os.sched_setaffinity(0,sorted(os.sched_getaffinity(0))[:2]);rows=[];score=[]
for c in data:
 oracle=entries.get('fixture_ocr_'+c['fixture'].removeprefix('faint_')+'_001') if c['fixture'] not in ['blue','peach'] else None
 for arm,field in [('noop512','baseline512'),('fsenet512','prediction512')]:
  key=c['source_id']+'_'+arm;p=Path(c[field]);assert hashlib.sha256(p.read_bytes()).hexdigest()==c[field+'_sha256'];cmd=[lock['tesseract'],str(p),'stdout','--tessdata-dir',lock['tessdata'],'-l','jpn+eng','--psm','1','tsv'];start=time.monotonic();proc=subprocess.Popen(cmd,stdout=subprocess.PIPE,stderr=subprocess.PIPE,env=env);peak=0;killed=None
  while proc.poll() is None:
   try:
    pss=int(next(x for x in Path(f'/proc/{proc.pid}/smaps_rollup').read_text().splitlines() if x.startswith('Pss:')).split()[1])*1024;peak=max(peak,pss)
    if pss>2147483648:killed='sampled-PSS-limit';proc.kill()
   except (OSError,StopIteration):pass
   if time.monotonic()-start>30:killed='timeout';proc.kill()
   time.sleep(.1)
  out,err=proc.communicate();(b/(key+'.tsv')).write_bytes(out);(b/(key+'.stderr.log')).write_bytes(err);row={'id':key,'fixture':c['fixture'],'source_kind':c['source_kind'],'arm':arm,'command':cmd,'returncode':proc.returncode,'killed':killed,'sampled_process_peak_pss_bytes':peak,'elapsed_seconds':time.monotonic()-start,'image_sha256':c[field+'_sha256'],'tsv_sha256':hashlib.sha256(out).hexdigest(),'text_truth_status':'existing-renderer' if oracle else 'unknown-no-independent-transcription'}
  if proc.returncode==0:
   spans=m.parse_tsv(out.decode(),512,512);text=''.join(x['text'] for x in spans);(b/(key+'.txt')).write_text(text);row['text_sha256']=hashlib.sha256(text.encode()).hexdigest()
   if oracle:
    expected=['2026','１２３','123'] if 'mixed' in c['fixture'] else ['1200'];row['literal_digit_run_presence']={x:x in text for x in expected};row['literal_scope']='fullpage literal recognition observation, not physical glyph absence count';score.append({'id':key,'oracle':oracle['pages'][0],'text':text,'positions':{'page_id':oracle['pages'][0]['page_id'],'width':512,'height':512,'spans':spans}})
  rows.append(row);(b/'ocr-runs.json').write_text(json.dumps(rows,indent=2,ensure_ascii=False)+'\n');print(key,proc.returncode,flush=True)
(w/'ocr-score-input.json').write_text(json.dumps(score,ensure_ascii=False))
