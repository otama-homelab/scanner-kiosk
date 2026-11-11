import json,subprocess,time,os,hashlib
from pathlib import Path
repo=Path('/home/yamato/otama-homelab/scanner-kiosk');e=repo/'docs/design/research/correction-docshadow-native-v060';prior=repo/'docs/design/research/correction-docshadow-cpu-v060';manifest=json.loads((repo/'docs/design/research/correction-paired-v060/paired-truth.json').read_text());rows=[]
for r in manifest['rows']:
 for kind in ('shadow','clean'):
  p=Path(r[kind]);assert hashlib.sha256(p.read_bytes()).hexdigest()==r[kind+'_sha256']
  case={'id':r['id']+'_'+kind,'path':str(p)}
  if case['id']=='mixed_shadow':
   result=json.loads((prior/'native.stdout.json').read_text());meta=json.loads((prior/'native-attempt.json').read_text());assert result['input_sha256']==r[kind+'_sha256'];assert result['model_size']==r['size'];assert result['providers']==['CPUExecutionProvider'];assert hashlib.sha256(Path(result['raw_output']).read_bytes()).hexdigest()==result['raw_output_sha256']
   result.update({'reused':True,'scope':'native-resolution synthetic diagnostic; prior actual shape overrides old generic scope label','sampled_peak_rss_bytes':meta['sampled_peak_rss_bytes'],'command':meta['command'],'prior_receipt':str(prior/'native.stdout.json'),'prior_receipt_sha256':hashlib.sha256((prior/'native.stdout.json').read_bytes()).hexdigest()})
  else:
   cmd=['/tmp/scanner-correction-next-venv/bin/python','/tmp/scanner-docshadow-native-v060/evaluate.py',json.dumps(case),'0'];t=time.monotonic();env=dict(os.environ);env['PYTHONPATH']='/tmp/scanner-docshadow-v060';proc=subprocess.Popen(cmd,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,env=env)
   killed=None;peak=0
   while proc.poll() is None:
    try:
     status=Path(f'/proc/{proc.pid}/status').read_text();rss=int(next(x for x in status.splitlines() if x.startswith('VmRSS:')).split()[1])*1024;peak=max(peak,rss)
     if rss>2147483648:killed='diagnostic-rss-limit';proc.kill()
    except (OSError,StopIteration):pass
    if time.monotonic()-t>100:killed='process-timeout';proc.kill()
    time.sleep(.1)
   stdout,stderr=proc.communicate();(e/(case['id']+'.stderr.log')).write_text(stderr);(e/(case['id']+'.stdout.json')).write_text(stdout)
   result=json.loads(stdout) if proc.returncode==0 else {'id':case['id'],'status':killed or 'process-failed','returncode':proc.returncode,'input_sha256':r[kind+'_sha256'],'original_size':r['size']}
   result.update({'reused':False,'command':cmd,'elapsed_seconds':time.monotonic()-t,'sampled_peak_rss_bytes':peak})
  rows.append(result);(e/'runs.json').write_text(json.dumps(rows,indent=2)+'\n');print(case['id'],result['status'],'reused' if result['reused'] else 'new',flush=True)
