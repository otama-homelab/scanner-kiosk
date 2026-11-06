#!/usr/bin/env python3
"""Collect exact candidate advisory reports in a bounded sandbox; never approve publication."""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import subprocess

WORK=Path('/work')
INPUTS={'image':'image-sbom/image.syft.json','onefile':'onefile-sbom/onefile.syft.json',
        'tesseract-image':'tesseract-image-sbom/image.syft.json',
        'native':'native-sbom/native-origin.cdx.json','native-cpe':'native-sbom/native-origin.cdx.json'}


def environment(online=False):
    return {'PATH':'/usr/local/bin:/usr/bin:/bin','HOME':'/work','TMPDIR':'/tmp','GOMAXPROCS':'2',
            'GRYPE_DB_CACHE_DIR':'/work/db','GRYPE_CHECK_FOR_APP_UPDATE':'false',
            'GRYPE_DB_AUTO_UPDATE':str(online).lower(),'GRYPE_DB_VALIDATE_AGE':'true',
            'GRYPE_DB_VALIDATE_BY_HASH_ON_START':'true'}


def acquire(source):
    spec=importlib.util.spec_from_file_location('application',source/'scripts/build-application.py')
    module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
    module.WORK=WORK
    (WORK/'bin').mkdir()
    module.install_tool(source,'grype')
    subprocess.run([str(WORK/'bin/grype'),'db','update'],env=environment(True),check=True,timeout=600)


def scan(candidate,output):
    if output.exists() or output.is_symlink():raise FileExistsError('security output exists')
    output.mkdir(mode=0o700)
    receipt={'publication_approved':False,'scans':{},'scans_completed':False,'advisory_threshold_passed':False}
    config=WORK/'grype.yaml';config.write_text('ignore: []\n')
    tool=str(WORK/'bin/grype')
    try:
        status=subprocess.check_output([tool,'db','status','--output','json'],env=environment(),timeout=30)
        receipt['database']=json.loads(status)
        if receipt['database'].get('valid') is not True:raise ValueError('invalid advisory database')
        with (WORK/'bin/grype').open('rb') as stream:receipt['tool_sha256']=hashlib.file_digest(stream,'sha256').hexdigest()
        for scope,name in INPUTS.items():
            sbom=candidate/name
            if sbom.is_symlink() or not sbom.is_file() or not 0<sbom.stat().st_size<=16<<20:
                raise ValueError('missing/bounded candidate inventory')
            with sbom.open('rb') as stream:digest=hashlib.file_digest(stream,'sha256').hexdigest()
            directory=output/scope;directory.mkdir()
            command=[tool,'--config',str(config),'sbom:'+str(sbom),'--fail-on','high',
                     '--output','json','--file',str(directory/'grype.json')]
            if scope=='native-cpe':command+=['--add-cpes-if-none']
            with (directory/'scan.log').open('wb') as log:
                process=subprocess.run(command,env=environment(),stdout=subprocess.DEVNULL,stderr=log,timeout=600)
            if process.returncode not in {0,2}:raise RuntimeError('advisory scan failed: '+scope)
            report=json.loads((directory/'grype.json').read_bytes())
            if not isinstance(report.get('matches'),list) or not report.get('descriptor'):
                raise ValueError('invalid advisory report')
            receipt['scans'][scope]={'input_sha256':digest,'scan_completed':True,
                'exit_code':process.returncode,'match_count':len(report['matches']),
                'artifacts_sha256':{p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in directory.iterdir()}}
        receipt['scans_completed']=True
        receipt['advisory_threshold_passed']=all(s['exit_code']==0 for s in receipt['scans'].values())
    finally:
        (output/'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n')
    return receipt


if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('phase',choices=['acquire','scan'])
    parser.add_argument('--source',type=Path,default=Path('/source'))
    parser.add_argument('--candidate',type=Path,default=Path('/candidate'))
    parser.add_argument('--output',type=Path,default=WORK/'reports')
    args=parser.parse_args()
    if args.phase=='acquire':acquire(args.source)
    else:print(json.dumps(scan(args.candidate,args.output)))
