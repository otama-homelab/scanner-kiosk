import subprocess,time,json,os,signal
from pathlib import Path
w=Path('/tmp/scanner-fsenet-cpu-v060');b=Path('/home/yamato/otama-homelab/scanner-kiosk/docs/design/research/correction-fsenet-cpu-v060');cmd=[str(w/'venv/bin/python'),str(w/'run_official_512.py')];env=dict(os.environ);env.update({'PYTHONPATH':str(w/'source'),'ACCELERATE_USE_CPU':'true','PYTHONDONTWRITEBYTECODE':'1'});out=(b/'official-infer-512.stdout.log').open('w');err=(b/'official-infer-512.stderr.log').open('w');p=subprocess.Popen(cmd,stdout=out,stderr=err,env=env,cwd=w/'run512',start_new_session=True);start=time.monotonic();peakrss=0;peakpss=0;peakchildren=0;killed=None
while p.poll() is None:
 try:
  rss=int(next(x for x in Path(f'/proc/{p.pid}/status').read_text().splitlines() if x.startswith('VmRSS:')).split()[1])*1024;peakrss=max(peakrss,rss)
  todo=[p.pid];seen=set();pss=0
  while todo:
   pid=todo.pop()
   if pid in seen:continue
   seen.add(pid)
   try:
    todo += [int(x) for x in Path(f'/proc/{pid}/task/{pid}/children').read_text().split()]
    pss+=int(next(x for x in Path(f'/proc/{pid}/smaps_rollup').read_text().splitlines() if x.startswith('Pss:')).split()[1])*1024
   except (OSError,StopIteration):pass
  peakpss=max(peakpss,pss);peakchildren=max(peakchildren,len(seen)-1)
  if pss>2147483648:killed='diagnostic-aggregate-PSS-limit';os.killpg(p.pid,signal.SIGKILL)
 except (OSError,StopIteration):pass
 if time.monotonic()-start>180:killed='total-process-180s-timeout';os.killpg(p.pid,signal.SIGKILL)
 time.sleep(.1)
out.close();err.close();result={'command':cmd,'cwd':str(w),'environment':{k:env[k] for k in ['PYTHONPATH','ACCELERATE_USE_CPU','PYTHONDONTWRITEBYTECODE']},'returncode':p.returncode,'status':'pass' if p.returncode==0 else 'failed','killed':killed,'elapsed_seconds':time.monotonic()-start,'sampled_parent_peak_rss_bytes':peakrss,'sampled_aggregate_peak_pss_bytes':peakpss,'max_child_count':peakchildren,'resource_enforcement':'aggregate PSS across parent+workers sampled ~0.1s; parentRSS separately observed, not liveRSS kill','aggregate_pss_cap_bytes':2147483648,'total_process_timeout_seconds':180,'per_top_model_forward_alarm_seconds':30};(b/'official-infer-512.json').write_text(json.dumps(result,indent=2)+'\n');print(result)
