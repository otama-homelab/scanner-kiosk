#!/usr/bin/env python3
"""Compare actual product build import graphs with one Go advisory's imports.

This is package-presence evidence only, not general call-graph analysis, a VEX
waiver or permission to remove module-level findings from the scanner report.
"""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import subprocess

p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--advisory',type=Path,required=True)
p.add_argument('--receipt',type=Path,required=True)
args = p.parse_args()
root = Path(__file__).resolve().parents[1]
sha = lambda path: hashlib.sha256(path.read_bytes()).hexdigest()
files = [root/'go.mod',root/'go.sum',*root.glob('cmd/**/*.go'),*root.glob('internal/**/*.go'),*root.glob('pkg/**/*.go')]
before = {str(path.relative_to(root)):sha(path) for path in sorted(files)}
advisory = json.loads(args.advisory.read_text())
assert advisory['id']=='GO-2026-5932'
affected = sorted({i['path'] for a in advisory['affected'] for i in a['ecosystem_specific']['imports']})
assert affected and all(i.startswith('golang.org/x/crypto/openpgp') for i in affected)
receipt = {'passed':False,'scope':'absence of specific advisory import paths in linux/amd64 product builds; no module finding suppression or general vulnerability/call-graph acceptance','timestamp_utc':datetime.now(timezone.utc).isoformat(),'advisory_id':advisory['id'],'advisory_sha256':sha(args.advisory),'advisory_url':'https://vuln.go.dev/ID/'+advisory['id']+'.json','affected_imports':affected,'source_sha256':before,'script_sha256':sha(Path(__file__)),'builds':[]}
try:
    for target,tags,cgo in [('./cmd/scanner','webui','0'),('./cmd/sane-parameters','saneparams','1')]:
        env = dict(os.environ,GOTOOLCHAIN='go1.27.3',GOOS='linux',GOARCH='amd64',CGO_ENABLED=cgo)
        command = ['go','list','-deps','-tags',tags,target]
        result = subprocess.run(command,cwd=root,env=env,capture_output=True,check=True,timeout=120)
        packages = sorted(set(result.stdout.decode().splitlines()))
        matches = sorted(set(packages).intersection(affected))
        receipt['builds'].append({'target':target,'tags':tags,'cgo_enabled':cgo,'packages':packages,'affected_imports_present':matches,'crypto_imports':[i for i in packages if i.startswith('golang.org/x/crypto/')]})
        assert not matches, 'affected import present; requires further analysis'
    assert before=={str(path.relative_to(root)):sha(path) for path in sorted(files)}
    receipt['passed'] = True
except Exception as error:
    receipt['error'] = str(error)
    raise
finally:
    args.receipt.write_text(json.dumps(receipt,indent=2)+'\n')
    print(json.dumps({'passed':receipt['passed'],'advisory':advisory['id'],'builds':len(receipt['builds'])}))
