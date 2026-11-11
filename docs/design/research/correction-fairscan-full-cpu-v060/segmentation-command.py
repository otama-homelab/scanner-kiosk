from pathlib import Path
import json,subprocess,os,signal,time,hashlib
root=Path('/tmp/scanner-fairscan-fullcpu.AzeA6f');old=Path('/tmp/scanner-fairscan-jvm.k0rsMW');src=root/'source';out=Path('/home/yamato/otama-homelab/scanner-kiosk/docs/design/research/correction-fairscan-full-cpu-v060')
env=os.environ.copy();env.update(OMP_NUM_THREADS='2',OPENBLAS_NUM_THREADS='2',TF_NUM_INTEROP_THREADS='2',TF_NUM_INTRAOP_THREADS='2',CUDA_VISIBLE_DEVICES='-1',TMPDIR=str(root/'run-temp'))
(root/'run-temp').mkdir(exist_ok=True)
cmd=['taskset','-c',','.join(map(str,sorted(os.sched_getaffinity(0))[:2])),str(root/'venv/bin/python'),'-u','generate_masks.py']
start=time.monotonic();peak=0;reason=None
with (out/'segmentation.log').open('w') as log:
 p=subprocess.Popen(cmd,cwd=src/'evaluation/python',env=env,stdout=log,stderr=subprocess.STDOUT,start_new_session=True)
 while p.poll() is None:
  rss=0
  for proc in Path('/proc').iterdir():
   if proc.name.isdigit():
    try:
     stat=(proc/'stat').read_text().split()
     if int(stat[5])==p.pid:rss+=int(stat[23])*os.sysconf('SC_PAGE_SIZE')
    except (OSError,ValueError,IndexError):pass
  peak=max(peak,rss);runbytes=sum(q.stat().st_size for r in [root/'run-temp',src/'evaluation/dataset/masks',src/'evaluation/reports'] for q in r.rglob('*') if q.is_file())
  if time.monotonic()-start>30:reason='total_process_30s_stronger_than_per_forward_cap'
  elif rss>2*1024**3:reason='process_group_rss_2GiB'
  elif runbytes>256*1024**2:reason='run_temp_256MiB'
  if reason:os.killpg(p.pid,signal.SIGKILL);break
  time.sleep(.05)
 rc=p.wait()
masks=[{'path':str(q),'bytes':q.stat().st_size,'sha256':hashlib.sha256(q.read_bytes()).hexdigest()} for q in sorted((src/'evaluation/dataset/masks').glob('*.png'))]
d={'command':cmd,'cwd':str(src/'evaluation/python'),'exit_code':rc,'stop_reason':reason,'elapsed_s':time.monotonic()-start,'peak_process_group_rss_bytes':peak,'scope':'unchanged official tensorflow.lite.Interpreter script; full process30s bounds each forward too','masks':masks}
(out/'segmentation.json').write_text(json.dumps(d,indent=2)+'\n');print(json.dumps(d))
