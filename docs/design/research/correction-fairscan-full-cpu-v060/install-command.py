from pathlib import Path
import zipfile,email,json,hashlib,urllib.request,concurrent.futures,subprocess,time,os,signal
root=Path('/tmp/scanner-fairscan-fullcpu.AzeA6f');old=Path('/tmp/scanner-fairscan-jvm.k0rsMW');out=Path('/home/yamato/otama-homelab/scanner-kiosk/docs/design/research/correction-fairscan-full-cpu-v060');wheels=sorted((root/'wheels').glob('*.whl'))
def check(p):
 with zipfile.ZipFile(p) as z:
  n=next(n for n in z.namelist() if n.endswith('.dist-info/METADATA'));m=email.message_from_bytes(z.read(n))
 name=m['Name'];version=m['Version'];digest=hashlib.sha256(p.read_bytes()).hexdigest()
 assert not any(x in name.lower() for x in ['nvidia','cuda','cudnn'])
 url=f'https://pypi.org/pypi/{name}/{version}/json'
 with urllib.request.urlopen(url,timeout=30) as r:d=json.load(r)
 a=next(a for a in d['urls'] if a['filename']==p.name);assert a['digests']['sha256']==digest
 return {'name':name,'version':version,'wheel':p.name,'bytes':p.stat().st_size,'sha256':digest,'license':m['License'],'license_expression':m['License-Expression'],'pypi_metadata':url,'url':a['url']}
with concurrent.futures.ThreadPoolExecutor(max_workers=6) as pool:records=list(pool.map(check,wheels))
assert sum(r['bytes'] for r in records)<512*1024**2 and all(r['bytes']<256*1024**2 for r in records)
(out/'wheels.json').write_text(json.dumps({'wheels':records,'total_bytes':sum(r['bytes'] for r in records),'gpu_packages':[]},indent=2)+'\n')
cmd=['taskset','-c',','.join(map(str,sorted(os.sched_getaffinity(0))[:2])),str(root/'venv/bin/python'),'-m','pip','install','--no-index','--no-deps','--no-cache-dir']+[str(p) for p in wheels]
start=time.monotonic();reason=None
with (out/'install.log').open('w') as f:
 p=subprocess.Popen(cmd,stdout=f,stderr=subprocess.STDOUT,start_new_session=True)
 while p.poll() is None:
  total=sum(q.stat().st_size for r in [root,old] for q in r.rglob('*') if q.is_file())
  if total>3*1024**3:reason='combined_setup_cap_3GiB'
  elif time.monotonic()-start>180:reason='install_timeout_180s'
  if reason:os.killpg(p.pid,signal.SIGKILL);break
  time.sleep(.25)
 rc=p.wait()
(out/'install.json').write_text(json.dumps({'command':cmd,'exit_code':rc,'stop_reason':reason,'elapsed_s':time.monotonic()-start,'combined_setup_bytes':sum(q.stat().st_size for r in [root,old] for q in r.rglob('*') if q.is_file())},indent=2)+'\n');print(rc,reason)
