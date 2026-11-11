from pathlib import Path
import urllib.request,urllib.error,time,hashlib,json
w=Path('/tmp/scanner-fsenet-artifact-identity-v060');b=Path('/home/yamato/otama-homelab/scanner-kiosk/docs/design/research/correction-fsenet-artifact-identity-v060');u='https://drive.google.com/uc?export=download&confirm=t&id=1a1dGcSFYB1ocR5KohSTjXIQcpQ0w05V7';t=time.monotonic();out={'url':u,'source':'pinned upstream DocShadow/utils/utils.py MODEL_URLS.sd7k','max_bytes':268435456,'new_inference':False}
try:
 with urllib.request.urlopen(u,timeout=30) as a:
  out['http_status']=a.status;out['resolved_url']=a.url;out['content_type']=a.headers.get('Content-Type');out['content_length']=a.headers.get('Content-Length');out['content_disposition']=a.headers.get('Content-Disposition')
  content=a.read(1024*1024)
  if 'text/html' in (a.headers.get('Content-Type') or '') or content.lstrip().startswith(b'<!DOCTYPE html'):
   out['status']='html-not-checkpoint';(b/'source-checkpoint-response.html').write_bytes(content);out['response_bytes']=len(content);out['response_sha256']=hashlib.sha256(content).hexdigest()
  else:
   p=w/'exporter-sd7k.pth';n=len(content)
   with p.open('wb') as f:
    f.write(content)
    while x:=a.read(1024*1024):
     n+=len(x)
     if n>268435456:raise ValueError('checkpoint download cap')
     f.write(x)
   out.update({'status':'downloaded-unloaded','bytes':n,'sha256':hashlib.sha256(p.read_bytes()).hexdigest(),'path':str(p)})
except urllib.error.HTTPError as e:
 out.update({'status':'http-error','http_status':e.code,'error':str(e)})
except Exception as e:out.update({'status':'download-error','error':str(e)})
out['elapsed_seconds']=time.monotonic()-t;(b/'source-checkpoint-acquisition.json').write_text(json.dumps(out,indent=2)+'\n');print(out)
