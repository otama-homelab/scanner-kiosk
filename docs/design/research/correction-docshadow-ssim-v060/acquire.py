import os,subprocess,time,json
from pathlib import Path
w=Path('/tmp/scanner-docshadow-ssim-v060');e=Path('/home/yamato/otama-homelab/scanner-kiosk/docs/design/research/correction-docshadow-ssim-v060');cmd=[str(w/'venv/bin/python'),'-m','pip','download','--no-cache-dir','--only-binary=:all:','--index-url','https://pypi.org/simple','--dest',str(w/'wheels'),'scikit-image==0.25.2'];env=dict(os.environ);env['TMPDIR']=str(w/'pip-tmp');out=(e/'download.log').open('w');p=subprocess.Popen(cmd,stdout=out,stderr=subprocess.STDOUT,env=env);peak=0;killed=False;t=time.monotonic()
while p.poll() is None:
 size=sum(x.stat().st_size for tree in [w/'wheels',w/'pip-tmp'] for x in tree.rglob('*') if x.is_file());peak=max(peak,size)
 if size>268435456 or time.monotonic()-t>180:killed=True;p.kill()
 time.sleep(.1)
out.close();r={'command':cmd,'returncode':p.returncode,'killed':killed,'peak_download_and_staging_bytes':peak,'download_cap_bytes':268435456,'elapsed_seconds':time.monotonic()-t};(e/'download.json').write_text(json.dumps(r,indent=2)+'\n');assert p.returncode==0 and not killed;print(r)
