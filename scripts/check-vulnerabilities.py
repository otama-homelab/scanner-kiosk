#!/usr/bin/env python3
"""Scan an existing Syft SBOM; a completed scan is distinct from a clean gate.

Preserves all severities/fix states. No ignore rule, VEX or image pull is used.
The High threshold is a local candidate check, not release authorization.
"""
import argparse
from collections import Counter
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile

p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--grype', type=Path, required=True)
p.add_argument('--cache-dir', type=Path, required=True)
p.add_argument('--sbom', type=Path, required=True)
p.add_argument('--output-dir', type=Path, required=True)
args = p.parse_args()
args.output_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
assert args.output_dir.stat().st_mode & 0o077==0
args.cache_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
sha = lambda path: hashlib.sha256(path.read_bytes()).hexdigest()
input_hash = sha(args.sbom)
inventory = json.loads(args.sbom.read_text())
assert inventory['artifacts'], 'empty SBOM'
receipt = {'passed':False,'scan_completed':False,'started_at':datetime.now(timezone.utc).isoformat(),'scope':'advisory matching against supplied Syft inventory; local High threshold only, not exploitability review or release acceptance','input_sbom_sha256':input_hash,'sbom_source':inventory.get('source'),'script_sha256':sha(Path(__file__)),'threshold':'High','ignore_rules':[],'fix_state_filter':None}
try:
    tool = str(args.grype.resolve())
    # Populate/update this cache with `grype db update` first, then scan serially.
    env = {'PATH':os.environ['PATH'],'GRYPE_DB_CACHE_DIR':str(args.cache_dir.resolve()),'GRYPE_CHECK_FOR_APP_UPDATE':'false','GRYPE_DB_AUTO_UPDATE':'false','GRYPE_DB_VALIDATE_AGE':'true'}
    receipt['tool'] = {'binary_sha256':sha(args.grype),'version':json.loads(subprocess.run([tool,'version','--output','json'],env=env,capture_output=True,check=True,timeout=30).stdout)}
    with tempfile.TemporaryDirectory(prefix='scanner-grype-') as directory:
        config = Path(directory)/'grype.yaml'
        config.write_text('ignore: []\n')
        report = args.output_dir/'grype.json'
        command = [tool,'--config',str(config),'sbom:'+str(args.sbom.resolve()),'--fail-on','high','--output','json','--file',str(report.resolve())]
        try:
            process = subprocess.run(command,env=env,capture_output=True,timeout=600)
        except subprocess.TimeoutExpired as error:
            (args.output_dir/'scan.log').write_bytes(error.stderr or b'')
            raise
        (args.output_dir/'scan.log').write_bytes(process.stderr)
        receipt['exit_code'] = process.returncode
        if process.returncode not in {0,2}:
            raise RuntimeError('Grype scan failed: '+process.stderr.decode(errors='replace')[-1500:])
        result = json.loads(report.read_text())
        matches = result['matches']
        assert isinstance(matches,list) and result.get('descriptor')
        status = subprocess.run([tool,'--config',str(config),'db','status','--output','json'],env=env,capture_output=True,check=True,timeout=30)
        receipt['database'] = json.loads(status.stdout)
        assert receipt['database']['valid'], 'vulnerability database is invalid'
        with Path(receipt['database']['path']).open('rb') as stream:
            receipt['database_file_sha256'] = hashlib.file_digest(stream,'sha256').hexdigest()
        receipt['descriptor'] = result['descriptor']
        receipt['severity_counts'] = dict(Counter(m['vulnerability']['severity'] for m in matches))
        receipt['fix_state_counts'] = dict(Counter(m['vulnerability'].get('fix',{}).get('state','unknown') for m in matches))
        receipt['match_count'] = len(matches)
        receipt['ignored_match_count'] = len(result.get('ignoredMatches',[]))
        summary = [{'id':m['vulnerability']['id'],'severity':m['vulnerability']['severity'],'package':m['artifact']['name'],'installed':m['artifact']['version'],'type':m['artifact']['type'],'fix':m['vulnerability'].get('fix'),'data_source':m['vulnerability'].get('dataSource'),'match_details':m.get('matchDetails',[])} for m in matches]
        (args.output_dir/'findings.json').write_text(json.dumps(summary,indent=2)+'\n')
        assert sha(args.sbom)==input_hash, 'SBOM changed during scan'
        receipt['scan_completed'] = True
        receipt['passed'] = process.returncode==0
        receipt['artifacts'] = {path.name:sha(path) for path in [report,args.output_dir/'findings.json',args.output_dir/'scan.log']}
except Exception as error:
    receipt['error'] = str(error)
    raise
finally:
    receipt['finished_at'] = datetime.now(timezone.utc).isoformat()
    (args.output_dir/'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n')
    print(json.dumps({k:receipt.get(k) for k in ['passed','scan_completed','match_count','severity_counts','error']}))
raise SystemExit(0 if receipt['passed'] else 2)
