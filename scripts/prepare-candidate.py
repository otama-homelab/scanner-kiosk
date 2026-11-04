#!/usr/bin/env python3
"""Assemble generated candidate materials offline and invoke the existing sealed stage.

Use read-only exact-run inputs, bounded scratch and no credentials/network.
"""
import argparse
import hashlib
import importlib.util
import io
import json
import os
import re
from pathlib import Path
import stat
import tarfile

spec=importlib.util.spec_from_file_location('stage',Path(__file__).with_name('stage-candidate.py'))
stage=importlib.util.module_from_spec(spec);spec.loader.exec_module(stage)


def record(path):
    descriptor=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
    with os.fdopen(descriptor,'rb') as stream:
        info=os.fstat(stream.fileno())
        if not stat.S_ISREG(info.st_mode) or not 0<info.st_size<=stage.MAX_ASSET:
            raise ValueError('material outside bounds')
        return {'bytes':info.st_size,'sha256':hashlib.file_digest(stream,'sha256').hexdigest()}


def tar_bytes(archive,name,data):
    member=tarfile.TarInfo(name);member.size=len(data);member.mode=0o444
    archive.addfile(member,io.BytesIO(data))


class CheckedStream:
    def __init__(self,stream):self.stream=stream;self.digest=hashlib.sha256();self.count=0
    def read(self,size):
        data=self.stream.read(size);self.digest.update(data);self.count+=len(data);return data


def native_sources(external,project,tesseract,lock,tesseract_lock,index,output):
    expected={row['path']:row for row in lock['files']}
    if len(expected)!=len(lock['files']) or len(expected)>2048:raise ValueError('invalid external source index')
    seen=set();lock_seen=False;index['files']=[];index['origins']=lock.get('origins',[])
    with tarfile.open(external,'r:') as source,tarfile.open(output,'x',format=tarfile.USTAR_FORMAT) as target:
        for member in source:
            if member.name=='source-inputs.lock.json':
                if lock_seen:raise ValueError('duplicate external source lock')
                lock_seen=True
                if not member.isfile() or member.size>2<<20:raise ValueError('invalid bundled source lock')
                if json.load(source.extractfile(member))!=lock:raise ValueError('external source lock differs')
                continue
            if member.name not in expected or member.name in seen or not member.isfile() or member.issparse():
                raise ValueError('unexpected external source member')
            row=expected[member.name];seen.add(member.name)
            if (any(p in {'','.','..'} for p in member.name.split('/')) or '\\' in member.name
                    or type(row['bytes']) is not int or not 0<row['bytes']<=1<<30
                    or not re.fullmatch('[0-9a-f]{64}',row['sha256'])):
                raise ValueError('invalid external source identity')
            if member.size!=row['bytes']:raise ValueError('external source size mismatch')
            with source.extractfile(member) as stream:
                checked=CheckedStream(stream)
                clean=tarfile.TarInfo(member.name);clean.size=member.size;clean.mode=0o444
                target.addfile(clean,checked)
                if checked.count!=row['bytes'] or checked.digest.hexdigest()!=row['sha256']:
                    raise ValueError('external source hash mismatch')
            index['files'].append({'archive':member.name,**{key:row[key] for key in ['bytes','sha256']}})
        if not lock_seen or seen!=set(expected):raise ValueError('missing external sources')
        for name,path in [('build-controls/scanner-source.tar',project),('upstream/tesseract-source.tar.gz',tesseract)]:
            identity=record(path)
            if path==tesseract and (identity['sha256']!=tesseract_lock['sha256'] or identity['bytes']!=tesseract_lock['bytes']):
                raise ValueError('Tesseract source differs from lock')
            with path.open('rb') as stream:
                member=tarfile.TarInfo(name);member.size=identity['bytes'];member.mode=0o444
                target.addfile(member,stream)
            index['files'].append({'archive':name,**identity})
        tar_bytes(target,'INDEX.json',json.dumps(index,indent=2).encode()+b'\n')


def prepare(inputs,work):
    if work.exists() or work.is_symlink():raise FileExistsError('material work directory exists')
    work.mkdir(mode=0o700);materials=work/'materials';materials.mkdir()
    candidate=inputs/'candidate';native=inputs/'native';reports=inputs/'security/reports'
    app=stage.read_json(candidate/'application.json');build=stage.read_json(candidate/'packaged/build.json')
    native_build=stage.read_json(candidate/'native-build.json');source_sha=app['source_revision']
    if (not native_build['passed'] or native_build['source_revision']!=source_sha
            or record(native/'source.tar')['sha256']!=native_build['source_archive_sha256']):
        raise ValueError('native/application source mismatch')
    files={}
    def copy(role,path):
        row={'path':path.relative_to(inputs).as_posix(),**record(path)}
        stage.check_file(inputs,row,materials/stage.NAMES[role]);files[role]={'path':stage.NAMES[role],**record(materials/stage.NAMES[role])}
    def generated(role):files[role]={'path':stage.NAMES[role],**record(materials/stage.NAMES[role])}
    def write(role,value):
        (materials/stage.NAMES[role]).write_text(json.dumps(value,indent=2)+'\n');generated(role)
    for role,name in {'binary':'packaged/scanner-onefile-linux-amd64','image':'image.tar','build':'packaged/build.json',
            'packed-manifest':'packaged/manifest.json','application':'application.json','image-config':'image.json',
            'onefile-sbom':'onefile-sbom/onefile.syft.json','onefile-inventory':'onefile-sbom/receipt.json',
            'native-sbom':'native-sbom/native-origin.cdx.json','image-sbom':'image-sbom/image.syft.json',
            'image-inventory':'image-sbom/receipt.json','base-source':'base-sources.tar','base-inventory':'base-inventory.json',
            'application-inputs':'application-sources.tar'}.items():copy(role,candidate/name)
    copy('source',native/'source.tar')
    controls={};deployment=[]
    with tarfile.open(native/'source.tar','r:') as archive:
        total=0;seen=set()
        for count,member in enumerate(archive,1):
            total+=member.size
            if count>8192 or total>64<<20 or member.name in seen:raise ValueError('project source outside bounds')
            seen.add(member.name)
            if not member.isfile():continue
            name=member.name.removeprefix('source/')
            required={'LICENSE':'license','THIRD-PARTY-NOTICES.md':'notices','CHANGELOG.md':'changelog',
                      'native/runtime-manifest.json':'native-manifest','native/runtime-provenance.json':'native-provenance'}
            if name in required:
                if member.size>8<<20:raise ValueError('oversized source control')
                (materials/stage.NAMES[required[name]]).write_bytes(archive.extractfile(member).read());generated(required[name])
            if name in {'native/source-inputs.lock.json','native/tesseract-source.lock.json'}:
                if member.size>2<<20:raise ValueError('oversized source lock')
                controls[name]=json.load(archive.extractfile(member))
            if name.startswith('deploy/') or name in {'.env.example','docs/standalone.md','docs/design/native-replacement.md','docs/design/release-channels.md'}:
                if member.size>2<<20:raise ValueError('oversized deployment input')
                deployment.append((name,archive.extractfile(member).read()))
    for role in ['license','notices','changelog','native-manifest','native-provenance']:
        if role not in files:raise ValueError('missing project control: '+role)
    index={'application_source_commit':source_sha,'candidate_sha256':files['binary']['sha256'],
           'native_manifest_sha256':files['native-manifest']['sha256'],'publication_approved':False}
    native_sources(native/'external-sources.tar',native/'source.tar',native/'tesseract-source.tar.gz',
        controls['native/source-inputs.lock.json'],controls['native/tesseract-source.lock.json'],index,materials/stage.NAMES['native-source'])
    generated('native-source');write('native-source-index',index)
    if not deployment or not any(n=='deploy/compose.yaml' for n,_ in deployment):raise ValueError('missing deployment controls')
    with tarfile.open(materials/stage.NAMES['deployment'],'x',format=tarfile.USTAR_FORMAT) as archive:
        for name,data in deployment:tar_bytes(archive,name,data)
    generated('deployment')
    write('deployment-inventory',{'passed':True,'source_archive_sha256':files['source']['sha256'],
        'archive_sha256':files['deployment']['sha256'],'files':[name for name,_ in deployment]})
    security=stage.read_json(reports/'receipt.json')
    if not security['scans_completed']:raise ValueError('advisory collection incomplete')
    write('security',security)
    with tarfile.open(materials/stage.NAMES['security-reports'],'x',format=tarfile.USTAR_FORMAT) as archive:
        for scope,scan in security['scans'].items():
            for name,digest in scan['artifacts_sha256'].items():
                if not re.fullmatch(r'[A-Za-z0-9_.-]+',name) or scope not in {'image','onefile','native','native-cpe'}:
                    raise ValueError('unsafe advisory member')
                path=reports/scope/name;identity=record(path)
                if identity['sha256']!=digest or identity['bytes']>16<<20:raise ValueError('advisory report changed')
                tar_bytes(archive,scope+'/'+name,path.read_bytes())
    generated('security-reports')
    image=stage.read_json(candidate/'image.json')[0]['Id']
    write('source-bindings',{'application_source_commit':source_sha,'candidate_sha256':files['binary']['sha256'],
        'api_sha256':build['app_sha256'],'image_id':image,'version':app['version'],'assets':app['assets'],
        'native_manifest_sha256':files['native-manifest']['sha256'],'native_provenance_sha256':files['native-provenance']['sha256'],
        'archives':{scope:{'bytes':files[role]['bytes'],'sha256':files[role]['sha256']} for scope,role in
                    [('native','native-source'),('base','base-source'),('application','application-inputs')]},'publication_approved':False})
    inventory=work/'inventory.json';inventory.write_text(json.dumps({'format':1,'source_sha':source_sha,'files':files})+'\n')
    return stage.stage(materials,inventory,work/'candidate')


if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--inputs',type=Path,required=True);parser.add_argument('--output',type=Path,required=True)
    args=parser.parse_args();print(json.dumps(prepare(args.inputs,args.output)))
