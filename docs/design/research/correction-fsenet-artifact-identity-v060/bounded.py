import subprocess,time,json,sys
from pathlib import Path
b=Path('/home/yamato/otama-homelab/scanner-kiosk/docs/design/research/correction-fsenet-artifact-identity-v060');name=sys.argv[1];cmd=sys.argv[2:];start=time.monotonic();out=(b/(name+'.stdout.log')).open('w');err=(b/(name+'.stderr.log')).open('w');p=subprocess.Popen(cmd,stdout=out,stderr=err);peak=0;killed=None
while p.poll() is None:
 try:
  rss=int(next(x for x in Path(f'/proc/{p.pid}/status').read_text().splitlines() if x.startswith('VmRSS:')).split()[1])*1024;peak=max(peak,rss)
  if rss>2147483648:killed='RSS-limit';p.kill()
 except (OSError,StopIteration):pass
 if time.monotonic()-start>180:killed='180s-timeout';p.kill()
 time.sleep(.1)
out.close();err.close();(b/(name+'-process.json')).write_text(json.dumps({'command':cmd,'returncode':p.returncode,'killed':killed,'elapsed_seconds':time.monotonic()-start,'sampled_peak_rss_bytes':peak,'rss_cap_bytes':2147483648,'status':'pass' if p.returncode==0 else 'failed'},indent=2)+'\n');print(name,p.returncode)
