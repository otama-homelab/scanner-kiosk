#!/usr/bin/env python3
"""Inventory declared build dependencies and verified Go module notice files.

Uses a manifest-only temporary directory, never host node_modules/captures/env.
License evidence is retained without inventing a project license or legal pass.
"""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--syft', type=Path, required=True)
p.add_argument('--runtime-sbom', type=Path, required=True)
p.add_argument('--output-dir', type=Path, required=True)
args = p.parse_args()
root = Path(__file__).resolve().parents[1]
args.output_dir = args.output_dir.resolve()
args.output_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
assert args.output_dir.stat().st_mode & 0o077==0
sha = lambda path: hashlib.sha256(path.read_bytes()).hexdigest()
inputs = ['go.mod','go.sum','web/package.json','web/package-lock.json']
before = {name:sha(root/name) for name in inputs}
receipt = {'passed':False,'started_at':datetime.now(timezone.utc).isoformat(),'source_sha256':before,'script_sha256':sha(Path(__file__)),'runtime_sbom_sha256':sha(args.runtime_sbom),'scope':'declared lockfile dependencies, including optional foreign-platform packages; verified runtime Go module notice files, not installed build image inventory or legal approval'}
env = dict(os.environ,GOTOOLCHAIN='go1.27.4',SYFT_CHECK_FOR_APP_UPDATE='false')
def run(command, cwd=root):
    return subprocess.run(command,cwd=cwd,env=env,capture_output=True,check=True,timeout=180).stdout
try:
    tool = str(args.syft.resolve())
    receipt['syft'] = {'version':json.loads(run([tool,'version','--output','json'])),'binary_sha256':sha(args.syft)}
    with tempfile.TemporaryDirectory(prefix='scanner-build-inventory-') as directory:
        stage = Path(directory)/'manifests'
        (stage/'web').mkdir(parents=True)
        for name in inputs:
            shutil.copyfile(root/name,stage/name)
        config = Path(directory)/'syft.yaml'
        config.write_text('{}\n')
        raw, cdx = [args.output_dir/name for name in ['declared-build.syft.json','declared-build.cdx.json']]
        run([tool,'--config',str(config),'scan','dir:'+str(stage),'--override-default-catalogers','javascript-lock-cataloger','--override-default-catalogers','go-module-file-cataloger','--quiet','--output','syft-json='+str(raw),'--output','cyclonedx-json='+str(cdx)])
        inventory = json.loads(raw.read_text())
        artifacts = inventory['artifacts']
        lock = json.loads((root/'web/package-lock.json').read_text())
        expected = set()
        declared = []
        for path, package in lock['packages'].items():
            if not path:
                continue
            name = package.get('name') or path.rsplit('node_modules/',1)[-1]
            expected.add((name,package['version']))
            declared.append({'path':path,'name':name,'version':package['version'],'license':package.get('license'),'integrity':package.get('integrity'),'resolved':package.get('resolved'),'optional':package.get('optional',False),'dev':package.get('dev',False),'os':package.get('os'),'cpu':package.get('cpu')})
        actual = {(a['name'],a['version']) for a in artifacts if a['type']=='npm'}
        assert expected.issubset(actual), 'lockfile package missing from generated SBOM'
        (args.output_dir/'npm-lock-license-observations.json').write_text(json.dumps({'scope':'lockfile metadata; optional foreign-platform packages included; not legal approval','packages':declared},indent=2)+'\n')
        runtime = json.loads(args.runtime_sbom.read_text())
        modules = sorted({(a['name'],a['version']) for a in runtime['artifacts'] if a['type']=='go-module' and a['name'] not in {'scanner-kiosk','stdlib'}})
        sums = {(line.split()[0],line.split()[1]):line.split()[2] for line in (root/'go.sum').read_text().splitlines()}
        downloaded = []
        for name, version in modules:
            # Explicit version outside the app cannot rewrite its go.mod/go.sum.
            item = json.loads(run(['go','mod','download','-json',name+'@'+version],cwd=directory))
            assert item['Sum']==sums[(name,version)] and item['GoModSum']==sums[(name,version+'/go.mod')]
            downloaded.append(item)
        # Detect modifications to the extracted cache before copying notices.
        verified = run(['go','mod','verify']).decode().strip()
        assert verified=='all modules verified'
        notices = []
        for item in downloaded:
            source = Path(item['Dir'])
            destination = args.output_dir/'go-notices'/hashlib.sha256((item['Path']+'@'+item['Version']).encode()).hexdigest()[:16]
            entries = []
            for file in sorted(source.rglob('*')):
                if not file.is_file() or not file.name.upper().startswith(('LICENSE','LICENCE','COPYING','NOTICE','COPYRIGHT','AUTHORS','PATENTS')):
                    continue
                assert not file.is_symlink() and file.resolve().is_relative_to(source.resolve())
                relative = file.relative_to(source)
                target = destination/relative
                target.parent.mkdir(parents=True,exist_ok=True)
                shutil.copyfile(file,target)
                entries.append({'source_relative_path':str(relative),'bundled_path':str(target.relative_to(args.output_dir)),'sha256':sha(target)})
            notices.append({'module':item['Path'],'version':item['Version'],'sum':item['Sum'],'go_mod_sum':item['GoModSum'],'files':entries})
        # Go runtime license evidence comes from the exact local build toolchain.
        assert run(['go','version']).decode().split()[2]=='go1.27.4'
        goroot = Path(run(['go','env','GOROOT']).decode().strip())
        standard = []
        for filename in ['LICENSE','PATENTS']:
            file = goroot/filename
            if file.is_file():
                target = args.output_dir/'go-notices'/'stdlib-go1.27.4'/filename
                target.parent.mkdir(parents=True,exist_ok=True)
                shutil.copyfile(file,target)
                standard.append({'bundled_path':str(target.relative_to(args.output_dir)),'sha256':sha(target)})
        assert standard
        (args.output_dir/'go-notice-index.json').write_text(json.dumps({'scope':'notice files by basename from verified module cache; legal scope/obligations require review','modules':notices,'stdlib':standard,'project_license':'not selected; scanner-kiosk entries remain unresolved'},indent=2)+'\n')
        receipt.update(npm_lock_entries=len(declared),npm_unique_packages=len(expected),npm_missing_license_metadata=sum(not d['license'] for d in declared),go_runtime_third_party_modules=len(modules),go_modules_with_notice_files=sum(bool(n['files']) for n in notices),go_module_cache_verification=verified,package_count=len(artifacts))
        assert before=={name:sha(root/name) for name in inputs}, 'source manifests changed'
        receipt['artifacts'] = {str(path.relative_to(args.output_dir)):sha(path) for path in sorted(args.output_dir.rglob('*')) if path.is_file() and path.name!='receipt.json'}
        receipt['passed'] = True
except Exception as error:
    receipt['error'] = str(error)
    raise
finally:
    receipt['finished_at'] = datetime.now(timezone.utc).isoformat()
    (args.output_dir/'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n')
    print(json.dumps({k:receipt.get(k) for k in ['passed','package_count','npm_lock_entries','go_runtime_third_party_modules','go_modules_with_notice_files','error']}))
