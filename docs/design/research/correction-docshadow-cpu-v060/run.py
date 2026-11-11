import json,subprocess,time,os,hashlib
from pathlib import Path
repo=Path('/home/yamato/otama-homelab/scanner-kiosk');e=repo/'docs/design/research/correction-docshadow-cpu-v060';m=json.loads((repo/'docs/design/research/correction-paired-v060/paired-truth.json').read_text()); rows=[]
for r in m['rows']:
 for kind in ('shadow','clean'):
  p=Path(r[kind]);assert hashlib.sha256(p.read_bytes()).hexdigest()==r[kind+'_sha256']
  case={'id':r['id']+'_'+kind,'path':str(p)};cmd=['/tmp/scanner-correction-next-venv/bin/python','/tmp/scanner-docshadow-v060/evaluate.py',json.dumps(case),'512'];t=time.monotonic();proc=subprocess.Popen(cmd,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
  killed=None;peak=0
  while proc.poll() is None:
   try:
    status=Path(f'/proc/{proc.pid}/status').read_text();rss=int(next(x for x in status.splitlines() if x.startswith('VmRSS:')).split()[1])*1024;peak=max(peak,rss)
    if rss>2147483648:killed='diagnostic-rss-limit';proc.kill()
   except (OSError,StopIteration):pass
   if time.monotonic()-t>100:killed='process-timeout';proc.kill()
   time.sleep(.1)
  stdout,stderr=proc.communicate();(e/(case['id']+'.stderr.log')).write_text(stderr);(e/(case['id']+'.stdout.json')).write_text(stdout)
  result=json.loads(stdout) if proc.returncode==0 else {'id':case['id'],'status':killed or 'process-failed','returncode':proc.returncode}
  result.update({'command':cmd,'elapsed_seconds':time.monotonic()-t,'sampled_peak_rss_bytes':peak});rows.append(result);(e/'runs.json').write_text(json.dumps(rows,indent=2)+'\n');print(case['id'],result['status'],flush=True)
  if killed:break
 if rows[-1]['status']!='measured':break
