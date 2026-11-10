from pathlib import Path
import json,subprocess,hashlib
p=Path('/tmp/scanner-v060');cat=json.loads(Path('/tmp/scanner-v040/private-catalog.json').read_text())
for f in cat['fixtures']:
 b=Path('/tmp/scanner-v040/corpus',f['relative_path']).read_bytes();assert len(b)==f['bytes'] and hashlib.sha256(b).hexdigest()==f['sha256']
(p/'corpus-hash-check.json').write_text(json.dumps({'verified':len(cat['fixtures']),'fixtures':cat['fixtures']},indent=2))
for mode,limit in [('diagnostic',2048),('product',1024)]:
 c=json.loads((p/'cpu-candidate.json').read_text());c['operator_steps']=[f'CPU-only offline2threads; {mode} RSS{limit}MiB; no apply; installed package gate separate; quality truth incomplete.'];config=p/f'{mode}-candidate.json';config.write_text(json.dumps(c,indent=2))
 for f in cat['fixtures']:
  out=p/(mode+'-'+f['id']);cmd=['/tmp/scanner-v050/bin/scanner-eval','benchmark','--catalog','scripts/fixtures/evaluation/catalog.json','--private-catalog','/tmp/scanner-v040/private-catalog.json','--private-root','/tmp/scanner-v040/corpus','--candidate',str(config),'--fixture',f['id'],'--output',str(out),'--cold','1','--warm','2','--timeout-seconds','30','--max-rss-mib',str(limit),'--max-temp-disk-mib','256']
  with (p/(out.name+'.stdout')).open('w') as log:r=subprocess.run(cmd,stdout=log,stderr=subprocess.STDOUT)
  b=json.loads((out/'benchmark.json').read_text());print(mode,f['id'],r.returncode,b['status'],max(s['peak_rss_bytes'] for s in b['samples']),flush=True)
