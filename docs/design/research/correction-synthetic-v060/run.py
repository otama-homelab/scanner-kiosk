from pathlib import Path
import subprocess,json
p=Path('/tmp/scanner-v060-synthetic');configs={'none':'/tmp/scanner-v040/none.json','docaligner':'/tmp/scanner-v040/docaligner.json','old-onnx':'/tmp/scanner-v040/uvdoc.json','mesh':'/tmp/scanner-v060/product-candidate.json'}
for arm,config in configs.items():
 for f in json.loads((p/'catalog.json').read_text())['fixtures']:
  output=p/(arm+'-'+f['id']);cmd=['/tmp/scanner-v050/bin/scanner-eval','benchmark','--catalog','scripts/fixtures/evaluation/catalog.json','--private-catalog',str(p/'catalog.json'),'--private-root',str(p/'corpus'),'--candidate',config,'--fixture',f['id'],'--output',str(output),'--cold','1','--warm','2','--timeout-seconds','30','--max-rss-mib','1024','--max-temp-disk-mib','256']
  with (p/(output.name+'.log')).open('w') as log:r=subprocess.run(cmd,stdout=log,stderr=subprocess.STDOUT)
  b=json.loads((output/'benchmark.json').read_text());print(arm,f['id'],b['status'],max(s['peak_rss_bytes'] for s in b['samples']),flush=True);assert r.returncode==0 and b['status']=='complete'
