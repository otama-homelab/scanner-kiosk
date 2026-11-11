from pathlib import Path
import subprocess,time,os,signal,json
root=Path('/tmp/scanner-fairscan-fullcpu.AzeA6f');old=Path('/tmp/scanner-fairscan-jvm.k0rsMW');out=Path('/home/yamato/otama-homelab/scanner-kiosk/docs/design/research/correction-fairscan-full-cpu-v060');wheels=root/'wheels';wheels.mkdir(exist_ok=True)
cmd=[str(root/'venv/bin/python'),'-m','pip','download','--only-binary=:all:','--no-cache-dir','--index-url','https://pypi.org/simple','--dest',str(wheels),'tensorflow-cpu==2.19.1','opencv-python-headless==4.12.0.88','numpy==2.1.3']
start=time.monotonic();reason=None
with (out/'download.log').open('w') as f:
 p=subprocess.Popen(cmd,stdout=f,stderr=subprocess.STDOUT,start_new_session=True)
 while p.poll() is None:
  files=list(wheels.glob('*.whl'));sizes=[q.stat().st_size for q in files];total=sum(q.stat().st_size for r in [root,old] for q in r.rglob('*') if q.is_file())
  if any(x>256*1024**2 for x in sizes):reason='wheel_cap_256MiB'
  elif sum(sizes)>512*1024**2:reason='total_wheel_cap_512MiB'
  elif total>3*1024**3:reason='combined_setup_cap_3GiB'
  elif time.monotonic()-start>180:reason='setup_timeout_180s'
  if reason:os.killpg(p.pid,signal.SIGKILL);break
  time.sleep(.25)
 rc=p.wait()
(out/'download.json').write_text(json.dumps({'command':cmd,'exit_code':rc,'stop_reason':reason,'elapsed_s':time.monotonic()-start},indent=2)+'\n');print(rc,reason)
