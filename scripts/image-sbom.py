#!/usr/bin/env python3
"""Catalog an existing local image using an explicitly supplied official Syft.

Does not pull/publish images. Runtime and explicit build-stage inventories remain
separate; neither is vulnerability, license or release acceptance.
"""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile

p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--docker', default='docker')
p.add_argument('--syft', required=True)
p.add_argument('--image', required=True)
p.add_argument('--stage', choices=['runtime','frontend','build'], default='runtime')
p.add_argument('--output-dir', type=Path, required=True)
args = p.parse_args()
args.output_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
assert args.output_dir.stat().st_mode & 0o077==0, 'output directory must be private'
def run(command, **kwargs):
    return subprocess.run(command, check=True, capture_output=True, timeout=300, **kwargs).stdout
info = json.loads(run([args.docker,'image','inspect',args.image]))[0]
image_id = info['Id']
receipt = {'passed':False,'image_id':image_id,'scope':'squashed runtime image SBOM and package/license observations; no vulnerability scan, license approval, build-only dependency inventory or release acceptance','started_at':datetime.datetime.now(datetime.timezone.utc).isoformat()}
receipt['stage'] = args.stage
receipt['image_size_bytes'] = info['Size']
if args.stage != 'runtime':
    receipt['scope'] = 'squashed '+args.stage+' build-stage image inventory; not runtime contents, vulnerability/license approval, provenance or release acceptance'
try:
    syft = Path(args.syft).resolve()
    receipt['syft'] = {'version':json.loads(run([str(syft),'version','--output','json'])),'binary_sha256':hashlib.sha256(syft.read_bytes()).hexdigest()}
    receipt['script_sha256'] = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
    with tempfile.TemporaryDirectory(prefix='scanner-image-sbom-') as directory:
        archive = Path(directory)/'image.tar'
        with archive.open('wb') as output:
            # The Go build image includes toolchain/module caches (~5x runtime).
            save_timeout = 900 if args.stage == 'build' else 300
            receipt['archive_timeout_seconds'] = save_timeout
            process = subprocess.run([args.docker,'image','save',image_id],stdout=output,stderr=subprocess.PIPE,timeout=save_timeout)
        if process.returncode:
            raise RuntimeError('image save failed: '+process.stderr.decode(errors='replace')[-1000:])
        with tarfile.open(archive) as tar:
            manifest = json.load(tar.extractfile('manifest.json'))
            assert len(manifest)==1
            config = tar.extractfile(manifest[0]['Config']).read()
            assert 'sha256:'+hashlib.sha256(config).hexdigest()==image_id
        with archive.open('rb') as stream:
            receipt['docker_archive_sha256'] = hashlib.file_digest(stream,'sha256').hexdigest()
        raw, cdx = [args.output_dir/(args.stage+suffix) for suffix in ['.syft.json','.cdx.json']]
        config_path = Path(directory)/'syft.yaml'
        config_path.write_text('{}\n')
        # Suppress implicit online checks/enrichment; catalog only the local archive.
        env = dict(os.environ,SYFT_CHECK_FOR_APP_UPDATE='false')
        run([str(syft),'--config',str(config_path),'scan','docker-archive:'+str(archive),'--scope','squashed','--quiet','--output','syft-json='+str(raw),'--output','cyclonedx-json='+str(cdx)],env=env)
        inventory = json.loads(raw.read_text())
        packages = inventory['artifacts']
        types = {}
        for package in packages:
            types[package['type']] = types.get(package['type'],0)+1
        assert types.get('deb',0)>0 or types.get('apk',0)>0
        if args.stage == 'runtime':
            assert types.get('go-module',0)>0
            assert any(a['name']=='sane-airscan' for a in packages)
            assert any(a['name'] in {'tesseract-ocr-jpn','tesseract-ocr-data-jpn'} for a in packages)
        elif args.stage == 'frontend':
            assert types.get('npm',0)>0
            assert any(a['name']=='astro' for a in packages)
        else:
            assert types.get('go-module',0)>0
            assert any(a['name']=='libsane-dev' for a in packages)
        document = json.loads(cdx.read_text())
        assert document['bomFormat']=='CycloneDX' and document['components']
        licenses = [{'name':a['name'],'version':a['version'],'type':a['type'],'purl':a.get('purl'),'licenses':a.get('licenses',[])} for a in packages]
        license_path = args.output_dir/(args.stage+'-license-observations.json')
        license_path.write_text(json.dumps({'scope':'automatically detected metadata; unknowns and obligations require review','packages':licenses},indent=2)+'\n')
        receipt['package_types'] = types
        receipt['package_count'] = len(packages)
        receipt['packages_without_detected_license'] = sum(not a.get('licenses') for a in packages)
        receipt['artifacts'] = {path.name:hashlib.sha256(path.read_bytes()).hexdigest() for path in [raw,cdx,license_path]}
        receipt['passed'] = True
except Exception as error:
    receipt['error'] = str(error)
    raise
finally:
    receipt['finished_at'] = datetime.datetime.now(datetime.timezone.utc).isoformat()
    (args.output_dir/'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n')
    print(json.dumps({'passed':receipt['passed'],'package_count':receipt.get('package_count'),'output_dir':str(args.output_dir)}))
