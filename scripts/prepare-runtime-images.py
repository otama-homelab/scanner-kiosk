#!/usr/bin/env python3
"""Prepare exact allowlisted Docker contexts for the two runtime roles, offline."""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import stat

spec=importlib.util.spec_from_file_location('partition_native',Path(__file__).with_name('partition-native.py'))
partition=importlib.util.module_from_spec(spec);spec.loader.exec_module(partition)
ROLES={'core':('Dockerfile','scanner'),'tesseract':('Dockerfile.tesseract','scanner-tesseract-api')}


def regular(path,limit):
    try:
        fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
        with os.fdopen(fd,'rb') as stream:
            before=os.fstat(stream.fileno())
            partition.require(stat.S_ISREG(before.st_mode) and 0<before.st_size<=limit,'invalid runtime context input')
            digest=hashlib.file_digest(stream,'sha256').hexdigest();after=os.fstat(stream.fileno())
            partition.require((before.st_size,before.st_mtime_ns)==(after.st_size,after.st_mtime_ns),'runtime context input changed')
            return {'bytes':before.st_size,'sha256':digest}
    except OSError as error:raise ValueError('runtime context input unavailable or unsafe') from error


def small(path,limit=8<<20):
    try:
        fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
        with os.fdopen(fd,'rb') as stream:
            before=os.fstat(stream.fileno())
            partition.require(stat.S_ISREG(before.st_mode) and 0<before.st_size<=limit,'invalid runtime metadata')
            raw=stream.read(limit+1);after=os.fstat(stream.fileno())
            partition.require(len(raw)==before.st_size and (before.st_size,before.st_mtime_ns)==(after.st_size,after.st_mtime_ns),
                              'runtime metadata changed')
            return raw,{'bytes':len(raw),'sha256':hashlib.sha256(raw).hexdigest()}
    except OSError as error:raise ValueError('runtime metadata unavailable or unsafe') from error


def prepare(source,partitions,app,service,revision,output):
    if output.exists() or output.is_symlink():raise FileExistsError('runtime image contexts exist')
    partition.require(re.fullmatch(r'(?:[0-9a-f]{40}|[0-9a-f]{64})',revision),'invalid runtime source revision')
    source=source.resolve(strict=True);partitions=partitions.resolve(strict=True)
    partition.require(not output.resolve().is_relative_to(source) and not output.resolve().is_relative_to(partitions),
                      'runtime output must be separate from inputs')
    raw,_=small(partitions/'partition.json');parent=json.loads(raw)
    partition.require(set(parent['roles'])==set(ROLES) and re.fullmatch('[0-9a-f]{64}',parent['parent_manifest_sha256']),
                      'incomplete native partition handoff')
    binaries={'core':app,'tesseract':service};inputs={}
    for role,(recipe,name) in ROLES.items():
        root=partitions/role;control={}
        for filename,limit in [('native-manifest.json',8<<20),('native-provenance.json',8<<20)]:
            control[filename]=small(root/filename,limit)
        row=parent['roles'][role]
        partition.require(control['native-manifest.json'][1]['sha256']==row['native_manifest_sha256']
                          and control['native-provenance.json'][1]['sha256']==row['provenance_sha256'],
                          'native partition controls changed')
        origin=json.loads(control['native-provenance.json'][0])
        partition.require(origin['partition']['role']==role and origin['partition']['parent_manifest_sha256']==parent['parent_manifest_sha256'],
                          'native role source differs')
        files,_=partition.native.assembler.plan(root/'native-manifest.json',origin)
        partition.native.verify_native(root/'native',files)
        controls={destination:small(source/original,limit) for destination,original,limit in [
            ('Dockerfile',recipe,128<<10),('.dockerignore','.dockerignore',64<<10),('LICENSE','LICENSE',64<<10)]}
        inputs[role]={'files':files,'native_controls':control,'controls':controls,
                      'binary':{'path':name,**regular(binaries[role],128<<20)}}
    proof={'source_revision':revision,'parent_manifest_sha256':parent['parent_manifest_sha256'],
           'roles':{},'publication_approved':False}
    output.mkdir(mode=0o700)
    try:
        for role,data in inputs.items():
            target=output/role;target.mkdir(mode=0o700);native=target/'native';native.mkdir(mode=0o755)
            for name,entry in data['files'].items():
                if not entry.get('link'):partition.copy_file(partitions/role/'native',native,name,entry)
            for name,entry in data['files'].items():
                if entry.get('link'):
                    (native/name).parent.mkdir(parents=True,exist_ok=True,mode=0o755)
                    (native/name).symlink_to(entry['link'])
            for name,(raw,_) in (data['controls']|data['native_controls']).items():
                (target/name).write_bytes(raw);(target/name).chmod(0o444)
            binary=data['binary'];path=binaries[role]
            partition.copy_file(path.parent,target,path.name,
                                {'size':binary['bytes'],'sha256':binary['sha256'],'mode':0o555})
            if path.name!=binary['path']:(target/path.name).rename(target/binary['path'])
            proof['roles'][role]={'binary':binary,
                'native_manifest_sha256':data['native_controls']['native-manifest.json'][1]['sha256'],
                'native_provenance_sha256':data['native_controls']['native-provenance.json'][1]['sha256'],
                'dockerfile_sha256':data['controls']['Dockerfile'][1]['sha256'],
                'dockerignore_sha256':data['controls']['.dockerignore'][1]['sha256'],
                'license_sha256':data['controls']['LICENSE'][1]['sha256'],
                'native_entries':len(data['files']),'native_bytes':sum(e['size'] for e in data['files'].values())}
        (output/'runtime-image-inputs.json').write_text(json.dumps(proof,indent=2,sort_keys=True)+'\n')
    except Exception:
        shutil.rmtree(output);raise
    return proof


if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    for name in ['source','partitions','app','service','output']:parser.add_argument('--'+name,type=Path,required=True)
    parser.add_argument('--revision',required=True);args=parser.parse_args()
    print(json.dumps(prepare(args.source,args.partitions,args.app,args.service,args.revision,args.output),sort_keys=True))
