import subprocess,time,json,sys,os
from pathlib import Path
root=Path('/tmp/scanner-fsenet-cpu-v060');b=Path('/home/yamato/otama-homelab/scanner-kiosk/docs/design/research/correction-fsenet-cpu-v060');name=sys.argv[1];cmd=sys.argv[2:];env=dict(os.environ);env['PYTHONPATH']=str(root/'source');env['ACCELERATE_USE_CPU']='true';p=subprocess.Popen(cmd,stdout=(b/(name+'.stdout.log')).open('w'),stderr=(b/(name+'.stderr.log')).open('w'),env=env,cwd=root);t=time.monotonic();peak=0;killed=None
while p.poll() is None:
 try:
  rss=int(next(x for x in Path(f'/proc/{p.pid}/status').read_text().splitlines() if x.startswith('VmRSS:')).split()[1])*1024;peak=max(peak,rss)
  if rss>2147483648:killed='diagnostic-rss-limit';p.kill()
 except (OSError,StopIteration):pass
 if time.monotonic()-t>180:killed='setup-process-timeout';p.kill()
 time.sleep(.1)
r={'command':cmd,'cwd':str(root),'environment_overrides':{'PYTHONPATH':env['PYTHONPATH'],'ACCELERATE_USE_CPU':'true'},'returncode':p.returncode,'killed':killed,'elapsed_seconds':time.monotonic()-t,'sampled_peak_rss_bytes':peak,'rss_cap_bytes':2147483648,'status':'pass' if p.returncode==0 else 'failed'};(b/(name+'.json')).write_text(json.dumps(r,indent=2)+'\n');print(r)
