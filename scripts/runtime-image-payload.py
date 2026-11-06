#!/usr/bin/env python3
"""Verify a Docker-exported final rootfs against its role inputs, offline.

The builder exports the already version-checked container. Docker owns layer and
whiteout semantics; this reader never extracts or executes archive contents.
"""
import argparse
import hashlib
import json
from pathlib import Path, PurePosixPath
import posixpath
import re
import stat
import tarfile


def require(value,message):
    if not value:raise ValueError(message)


def safe(name):
    require(isinstance(name,str) and name and not name.startswith('/') and '\\' not in name
            and '\x00' not in name and PurePosixPath(name).as_posix()==name
            and all(p not in {'','.','..'} for p in name.split('/')),'unsafe rootfs path')
    return name


def final_path(name,records,parents):
    name=safe(name);seen=set()
    for _ in range(32):
        require(name not in seen,'cyclic rootfs parent link');seen.add(name);parts=name.split('/')
        for i in range(1,len(parts)):
            parent='/'.join(parts[:i]);entry=records.get(parent)
            if entry and entry['type']=='symlink':
                link=entry['link'];parents[parent]=link
                target=link.lstrip('/') if link.startswith('/') else posixpath.join(posixpath.dirname(parent),link)
                name=safe(posixpath.normpath(posixpath.join(target,*parts[i:])))
                break
        else:return name
    raise ValueError('too many rootfs parent links')


def inventory(path):
    info=path.lstat();require(stat.S_ISREG(info.st_mode) and 0<info.st_size<=512<<20,'rootfs archive outside bounds')
    records={};total=0
    with tarfile.open(path,'r:') as archive:
        for member in archive:
            name=member.name.removeprefix('./').rstrip('/')
            if name in {'','.'} and member.isdir():continue
            safe(name);total+=member.size
            require(name not in records and len(records)<20000 and 0<=member.size<=128<<20
                    and total<=512<<20 and not member.issparse(),'invalid rootfs member')
            row={'uid':member.uid,'gid':member.gid,'mode':member.mode,'bytes':member.size}
            if member.isfile():
                with archive.extractfile(member) as stream:row['sha256']=hashlib.file_digest(stream,'sha256').hexdigest()
                row['type']='file'
            elif member.issym():row.update(type='symlink',link=member.linkname)
            elif member.isdir():row['type']='directory'
            else:row.update(type='other',link=member.linkname)
            records[name]=row
    return records


def inspect(rootfs,manifest,expected,role,image_id,container):
    require(role in {'core','tesseract'} and re.fullmatch('sha256:[0-9a-f]{64}',image_id),'invalid runtime image identity')
    require(isinstance(container,list) and len(container)==1 and container[0]['Image']==image_id
            and re.fullmatch('[0-9a-f]{64}',container[0]['Id']),'rootfs container/image mismatch')
    state=container[0]['State']
    require(state['Running'] is False and state['OOMKilled'] is False and state['ExitCode']==0,
            'runtime version container did not complete safely')
    raw=manifest.read_bytes();require(len(raw)<=8<<20 and hashlib.sha256(raw).hexdigest()==expected['native_manifest_sha256'],
                                    'native runtime manifest differs from build input')
    native=json.loads(raw)['files'];require(0<len(native)<=4096,'invalid native runtime file count')
    records=inventory(rootfs);parents={};verified={}
    def file(name,digest,size=None,mode=0o444):
        target=final_path(name,records,parents);actual=records.get(target,{})
        require(actual.get('type')=='file' and actual.get('sha256')==digest
                and (size is None or actual['bytes']==size) and actual['mode']==mode
                and actual['uid']==0 and actual['gid']==0,'runtime file bytes, mode or owner mismatch')
        verified[name]={'path':target,**actual}
    for name,entry in native.items():
        safe(name)
        if entry.get('link'):
            target=final_path(name,records,parents);actual=records.get(target,{})
            require(actual.get('type')=='symlink' and actual['link']==entry['link']
                    and actual['uid']==0 and actual['gid']==0,'native runtime symlink mismatch')
            verified[name]={'path':target,**actual}
        else:file(name,entry['sha256'],entry['size'],entry['mode'])
    binary=expected['binary'];require(binary['path']==('scanner' if role=='core' else 'scanner-tesseract-api'),'invalid runtime executable path')
    file('usr/local/bin/'+binary['path'],binary['sha256'],binary['bytes'],0o555)
    file('usr/share/scanner-kiosk/runtime-manifest.json',expected['native_manifest_sha256'])
    file('usr/share/scanner-kiosk/runtime-provenance.json',expected['native_provenance_sha256'])
    file('usr/share/doc/scanner-kiosk/LICENSE',expected['license_sha256'])
    for name in records:
        if role=='core':require(name!='usr/bin/tesseract' and not name.startswith(('usr/share/tessdata/','usr/lib/libtesseract')),
                               'core image contains a native OCR payload')
        else:require(name!='usr/bin/scanimage' and not name.startswith(('usr/lib/libsane','usr/lib/sane/','usr/libexec/scanner/')),
                     'Tesseract image contains a scanner payload')
    with rootfs.open('rb') as stream:rootfs_sha=hashlib.file_digest(stream,'sha256').hexdigest()
    return {'passed':True,'image_id':image_id,'container_id':container[0]['Id'],'role':role,
            'rootfs_sha256':rootfs_sha,'native_manifest_sha256':expected['native_manifest_sha256'],
            'binary_sha256':binary['sha256'],'native_paths':len(native),'parent_links':parents,
            'verified_files_sha256':hashlib.sha256(json.dumps(verified,sort_keys=True).encode()).hexdigest(),
            'publication_approved':False}


if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    for name in ['rootfs','manifest','inputs','metadata','container','output']:parser.add_argument('--'+name,type=Path,required=True)
    parser.add_argument('--role',choices=['core','tesseract'],required=True);args=parser.parse_args()
    image=json.loads(args.metadata.read_bytes());require(len(image)==1,'expected one runtime image')
    inputs=json.loads(args.inputs.read_bytes())
    proof=inspect(args.rootfs,args.manifest,inputs['roles'][args.role],args.role,image[0]['Id'],json.loads(args.container.read_bytes()))
    args.output.write_text(json.dumps(proof,indent=2,sort_keys=True)+'\n')
    print(json.dumps(proof,sort_keys=True))
