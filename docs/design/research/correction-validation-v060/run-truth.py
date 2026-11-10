from pathlib import Path
import subprocess,json
p=Path('/tmp/scanner-v060')
for r in json.loads((p/'truth-manifest.json').read_text())['rows']:
 name='fixture_uvdoc_truth_'+r['id'];out=p/('truth-'+r['id']);cmd=['/tmp/scanner-v050/bin/scanner-eval','benchmark','--catalog','scripts/fixtures/evaluation/catalog.json','--private-catalog',str(p/'truth-catalog.json'),'--private-root',str(p/'uvdoc-subset/img'),'--candidate',str(p/'diagnostic-candidate.json'),'--fixture',name,'--output',str(out),'--cold','1','--warm','2','--timeout-seconds','30','--max-rss-mib','2048','--max-temp-disk-mib','256']
 with (p/('truth-'+r['id']+'.stdout')).open('w') as log:result=subprocess.run(cmd,stdout=log,stderr=subprocess.STDOUT)
 assert result.returncode==0
 print(name,'retained',flush=True)
