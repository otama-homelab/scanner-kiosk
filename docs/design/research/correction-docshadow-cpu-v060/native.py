import subprocess,time,json
from pathlib import Path
case={'id':'mixed_shadow','path':'/tmp/scanner-v060-paired/shadow/mixed.png'}
cmd=['/tmp/scanner-correction-next-venv/bin/python','/tmp/scanner-docshadow-v060/evaluate.py',json.dumps(case),'0'];p=subprocess.Popen(cmd,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True);t=time.monotonic();peak=0;killed=None
while p.poll() is None:
 try:
  rss=int(next(x for x in Path(f'/proc/{p.pid}/status').read_text().splitlines() if x.startswith('VmRSS:')).split()[1])*1024;peak=max(peak,rss)
  if rss>2147483648:killed='diagnostic-rss-limit';p.kill()
 except (OSError,StopIteration):pass
 if time.monotonic()-t>100:killed='process-timeout';p.kill()
 time.sleep(.1)
o,e=p.communicate();b=Path('/home/yamato/otama-homelab/scanner-kiosk/docs/design/research/correction-docshadow-cpu-v060');(b/'native.stdout.json').write_text(o);(b/'native.stderr.log').write_text(e);(b/'native-attempt.json').write_text(json.dumps({'command':cmd,'returncode':p.returncode,'sampled_peak_rss_bytes':peak,'killed':killed,'elapsed_seconds':time.monotonic()-t,'status':'measured' if p.returncode==0 else 'failed'},indent=2)+'\n');print(p.returncode,killed,e[-1000:])
