import json,urllib.request,hashlib,time,shutil
from pathlib import Path
r=Path('/home/yamato/otama-homelab/scanner-kiosk');b=r/'docs/design/research/correction-fsenet-cpu-v060';w=Path('/tmp/scanner-fsenet-cpu-v060');(w/'wheels').mkdir(exist_ok=True);resolve=json.loads((b/'dependency-resolution.json').read_text());records=[];downloaded=0
for item in resolve['install']:
 name=item['metadata']['name'];v=item['metadata']['version'];url=item['download_info']['url'];fn=url.split('/')[-1];sha=item['download_info']['archive_info']['hashes']['sha256'];release=json.load(urllib.request.urlopen('https://pypi.org/pypi/'+name+'/'+v+'/json',timeout=30));match=[x for x in release['urls'] if x['filename']==fn];assert len(match)==1 and match[0]['digests']['sha256']==sha;assert fn.endswith('.whl');p=w/'wheels'/fn;old=Path('/tmp/scanner-docshadow-ssim-v060/wheels')/fn;reused=old.exists() and hashlib.sha256(old.read_bytes()).hexdigest()==sha
 if reused:shutil.copyfile(old,p)
 else:
  n=0
  with urllib.request.urlopen(url,timeout=30) as a,p.open('wb') as f:
   while x:=a.read(1024*1024):
    n+=len(x);downloaded+=len(x)
    if downloaded>536870912:raise ValueError('extra dependency download cap')
    f.write(x)
 assert hashlib.sha256(p.read_bytes()).hexdigest()==sha
 records.append({'name':name,'version':v,'wheel':fn,'sha256':sha,'bytes':p.stat().st_size,'reused_existing_verified_wheel':reused,'official_url':url});print(name,v,flush=True)
(b/'wheel-identities.json').write_text(json.dumps({'files':records,'new_download_bytes':downloaded,'total_wheel_bytes':sum(x['bytes'] for x in records),'all_pypi_sha_verified':True,'source_builds':0},indent=2)+'\n')
req='\n'.join(x['name']+'=='+x['version']+' --hash=sha256:'+x['sha256'] for x in records)+'\n';(w/'requirements.lock').write_text(req);(b/'requirements.lock').write_text(req)
release=json.loads((b/'release.json').read_text());asset=next(x for x in release['assets'] if x['name']=='SD7K.pth');assert asset['size']<268435456;model=w/'SD7K.pth';n=0;t=time.monotonic()
with urllib.request.urlopen(asset['browser_download_url'],timeout=30) as a,model.open('wb') as f:
 while x:=a.read(1024*1024):
  n+=len(x)
  if n>268435456:raise ValueError('model cap')
  f.write(x)
assert n==asset['size'];(b/'model-acquisition.json').write_text(json.dumps({'url':asset['browser_download_url'],'bytes':n,'sha256':hashlib.sha256(model.read_bytes()).hexdigest(),'release_id':release['id'],'asset_id':asset['id'],'path':str(model),'elapsed_seconds':time.monotonic()-t,'max_bytes':268435456},indent=2)+'\n');print('model',n)
