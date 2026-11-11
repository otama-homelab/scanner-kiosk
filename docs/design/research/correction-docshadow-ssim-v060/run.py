import json,subprocess,time,hashlib
from pathlib import Path
w=Path('/tmp/scanner-docshadow-ssim-v060');b=Path('/home/yamato/otama-homelab/scanner-kiosk/docs/design/research/correction-docshadow-ssim-v060');cases=json.loads((w/'cases.json').read_text());rows=[]
for c in cases:
 cmd=[str(w/'venv/bin/python'),str(w/'metric.py'),json.dumps(c)];start=time.monotonic();p=subprocess.Popen(cmd,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True);peak=0;killed=None
 while p.poll() is None:
  try:
   rss=int(next(x for x in Path(f'/proc/{p.pid}/status').read_text().splitlines() if x.startswith('VmRSS:')).split()[1])*1024;peak=max(peak,rss)
   if rss>2147483648:killed='diagnostic-rss-limit';p.kill()
  except (OSError,StopIteration):pass
  if time.monotonic()-start>30:killed='process-timeout';p.kill()
  time.sleep(.1)
 stdout,stderr=p.communicate();(b/(c['id']+'.stdout.json')).write_text(stdout);(b/(c['id']+'.stderr.log')).write_text(stderr);row=json.loads(stdout) if p.returncode==0 else {'id':c['id'],'status':killed or 'failed','returncode':p.returncode};row.update({'command':cmd,'elapsed_seconds':time.monotonic()-start,'sampled_peak_rss_bytes':peak,'fixture':c['fixture'],'arm':c['arm'],'source_kind':c['source_kind']});rows.append(row);(b/'runs.json').write_text(json.dumps(rows,indent=2)+'\n');print(c['id'],row.get('ssim',row['status']),flush=True)
