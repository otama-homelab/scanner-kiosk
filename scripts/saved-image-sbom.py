#!/usr/bin/env python3
"""Catalog a saved runtime image offline, retaining the separate base inventory.

Run in a bounded sandbox with read-only image/tool inputs and no Docker socket.
"""
import argparse
from collections import Counter
import hashlib
import json
from pathlib import Path, PurePosixPath
import shutil
import stat
import subprocess
import tarfile
import tempfile


def verify_archive(path, info):
    size=path.lstat()
    if not stat.S_ISREG(size.st_mode) or not 0<size.st_size<=512<<20:
        raise ValueError('image archive outside bounds')
    with tarfile.open(path,'r:') as archive:
        members={};total=0
        for member in archive:
            total+=member.size
            if (len(members)>=4096 or total>512<<20 or member.name in members
                    or not (member.isfile() or member.isdir()) or member.issparse() or member.size<0
                    or PurePosixPath(member.name).is_absolute() or '..' in PurePosixPath(member.name).parts):
                raise ValueError('invalid image archive member')
            members[member.name]=member
        def metadata(name):
            member=members[name]
            if not member.isfile() or member.size>65536:raise ValueError('invalid image metadata')
            return archive.extractfile(member).read()
        manifest=json.loads(metadata('manifest.json'))
        if len(manifest)!=1:raise ValueError('expected one saved image')
        config=metadata(manifest[0]['Config'])
        if 'sha256:'+hashlib.sha256(config).hexdigest()!=info['Id']:
            raise ValueError('image configuration digest mismatch')
        diffs=json.loads(config)['rootfs']['diff_ids']
        layers=manifest[0]['Layers']
        if diffs!=info['RootFS']['Layers'] or len(layers)!=len(diffs):
            raise ValueError('image layer mapping mismatch')
        for name,digest in zip(layers,diffs):
            if not members[name].isfile():raise ValueError('image layer must be regular')
            with archive.extractfile(members[name]) as stream:
                if 'sha256:'+hashlib.file_digest(stream,'sha256').hexdigest()!=digest:
                    raise ValueError('image layer digest mismatch')


def catalog(archive, metadata, syft, output):
    if output.exists() or output.is_symlink():raise FileExistsError('image inventory output exists')
    info=json.loads(metadata.read_bytes())
    if len(info)!=1:raise ValueError('expected one image configuration')
    info=info[0]
    verify_archive(archive,info)
    with tempfile.TemporaryDirectory(prefix='saved-image-sbom-',dir=output.parent) as directory:
        work=Path(directory);bundle=work/'bundle';bundle.mkdir()
        config=work/'syft.yaml';config.write_text('{}\n')
        env={'PATH':'/usr/bin:/bin','HOME':str(work),'TMPDIR':str(work),'GOMAXPROCS':'2',
             'SYFT_PARALLELISM':'2','SYFT_CHECK_FOR_APP_UPDATE':'false'}
        with (bundle/'catalog.stderr').open('wb') as diagnostics:
            subprocess.run([str(syft),'--config',str(config),'scan','docker-archive:'+str(archive),
                '--scope','squashed','--quiet','--output','syft-json='+str(bundle/'image.syft.json'),
                '--output','cyclonedx-json='+str(bundle/'image.cdx.json')],env=env,
                check=True,timeout=180,stdout=subprocess.DEVNULL,stderr=diagnostics)
        inventory=json.loads((bundle/'image.syft.json').read_bytes())
        types=dict(Counter(p['type'] for p in inventory['artifacts']))
        if (inventory['source']['metadata']['imageID']!=info['Id'] or not types.get('deb')
                or not types.get('go-module')):raise ValueError('image/base inventory missing or mismatched')
        with archive.open('rb') as stream:digest=hashlib.file_digest(stream,'sha256').hexdigest()
        with syft.open('rb') as stream:tool_digest=hashlib.file_digest(stream,'sha256').hexdigest()
        receipt={'passed':True,'image_id':info['Id'],'all_layer_diff_ids_verified':True,
                 'image_archive_sha256':digest,'image_package_types':types,'syft_sha256':tool_digest,
                 'scope':'Saved image and Go module inventory; native selected-origin inventory remains separate',
                 'publication_approved':False,'artifacts_sha256':{p.name:hashlib.sha256(p.read_bytes()).hexdigest()
                     for p in bundle.iterdir()}}
        (bundle/'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n')
        output.mkdir(mode=0o700)
        try:shutil.copytree(bundle,output,dirs_exist_ok=True)
        except Exception:
            shutil.rmtree(output)
            raise
    return receipt


if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    for name in ['archive','metadata','syft','output']:parser.add_argument('--'+name,type=Path,required=True)
    args=parser.parse_args()
    print(json.dumps(catalog(args.archive,args.metadata,args.syft,args.output)))
