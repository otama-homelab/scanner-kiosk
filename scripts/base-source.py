#!/usr/bin/env python3
"""Retain the reviewed distroless base source subset; never extract or execute it."""
import argparse
import hashlib
import importlib.util
import io
import json
import os
import re
from pathlib import Path, PurePosixPath
import tarfile
import tempfile
import urllib.request
import urllib.error
import urllib.parse

spec=importlib.util.spec_from_file_location('debian',Path(__file__).with_name('collect-debian-sources.py'))
debian=importlib.util.module_from_spec(spec);spec.loader.exec_module(debian)


def read_lock(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size>2<<20:
        raise ValueError('invalid base source lock')
    lock=json.loads(path.read_bytes())
    if lock['format']!=1 or len(lock['image_files'])>64 or len(lock['sources'])>16:
        raise ValueError('base source lock outside bounds')
    for entry in lock['image_files']:
        name=entry['file']
        if (not name.startswith('image/') or '\\' in name or any(p in {'','.','..'} for p in name.split('/'))
                or type(entry['bytes']) is not int or not 0<entry['bytes']<=16<<20
                or not re.fullmatch('[0-9a-f]{64}',entry['sha256'])):
            raise ValueError('invalid base image file record')
    for group in lock['sources']:
        for entry in group['files']:
            if (not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9.+~_-]*',entry['filename'])
                    or type(entry['size']) is not int or not 0<entry['size']<=16<<20
                    or not re.fullmatch('[0-9a-f]{64}',entry['sha256'])):
                raise ValueError('invalid base source record')
            debian.snapshot_url(entry['url'])
    return lock



class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self,request,fp,code,msg,headers,url):return None


def cdn_url(url):
    debian.snapshot_url(url)
    match=re.fullmatch(r'/archive/(debian|debian-security)/[0-9]{8}T[0-9]{6}Z/(pool/[A-Za-z0-9/_.+~%-]+)',urllib.parse.urlsplit(url).path)
    if not match or any(part in {'','.','..'} for part in urllib.parse.unquote(match[2]).split('/')):
        raise ValueError('invalid fixed Debian pool path')
    return 'https://deb.debian.org/'+match[1]+'/'+match[2]


def acquire(lock_path,output):
    lock=read_lock(lock_path)
    if output.exists() or output.is_symlink():raise FileExistsError('base source output exists')
    output.mkdir(mode=0o700)
    opener=urllib.request.build_opener(urllib.request.ProxyHandler({}),debian.SnapshotRedirect())
    cdn=urllib.request.build_opener(urllib.request.ProxyHandler({}),NoRedirect())
    def fetch(url,timeout):
        try:
            return cdn.open(cdn_url(url),timeout=timeout)
        except urllib.error.HTTPError as error:
            if error.code not in {404,408,429,500,502,503,504}:raise
        except (urllib.error.URLError,TimeoutError,ConnectionError):
            pass
        return opener.open(url,timeout=timeout)
    for group in lock['sources']:
        for entry in group['files']:
            debian.snapshot_url(entry['url'])
            if (PurePosixPath(entry['filename']).name!=entry['filename'] or '\\' in entry['filename']
                    or not 0<entry['size']<=16<<20):raise ValueError('invalid base source input')
            debian.collect_file(entry,output,fetch)


def pack(lock_path,image_files,sources,image_metadata,output):
    lock=read_lock(lock_path)
    if output.exists() or output.is_symlink():raise FileExistsError('base archive output exists')
    info=json.loads(image_metadata.read_bytes())[0]
    index={'image_id':info['Id'],'base_digest':lock['base_digest'],'packages':lock['packages'],
           'files':[],'publication_approved':False,'source_executed':False}
    inputs=[(image_files,r['file'],r['file'],r['bytes'],r['sha256']) for r in lock['image_files']]
    inputs += [(sources,r['filename'],'source/'+r['filename'],r['size'],r['sha256'])
               for group in lock['sources'] for r in group['files']]
    with tempfile.TemporaryDirectory(prefix='base-source-',dir=output.parent) as directory:
        temporary=Path(directory)/'base.tar'
        with tarfile.open(temporary,'w',format=tarfile.USTAR_FORMAT) as archive:
            seen=set();total=0
            for root,name,target,size,digest in inputs:
                if target in seen or '\\' in name or any(p in {'','.','..'} for p in name.split('/')):
                    raise ValueError('invalid/duplicate base source path')
                seen.add(target);path=root
                for part in PurePosixPath(name).parts:
                    path=path/part
                    if path.is_symlink():raise ValueError('linked base source input')
                if not path.is_file() or not 0<size<=16<<20 or path.stat().st_size!=size:
                    raise ValueError('base source type/size mismatch')
                total+=size
                if total>64<<20:raise ValueError('base source bundle outside bounds')
                data=path.read_bytes()
                if len(data)!=size or hashlib.sha256(data).hexdigest()!=digest:
                    raise ValueError('base source hash mismatch')
                member=tarfile.TarInfo(target);member.size=size;member.mode=0o444
                archive.addfile(member,io.BytesIO(data))
                index['files'].append({'file':target,'bytes':size,'sha256':digest})
            raw=json.dumps(index,indent=2).encode()+b'\n'
            member=tarfile.TarInfo('INDEX.json');member.size=len(raw);member.mode=0o444
            archive.addfile(member,io.BytesIO(raw))
        temporary.chmod(0o444)
        os.link(temporary,output)
    with output.open('rb') as stream:digest=hashlib.file_digest(stream,'sha256').hexdigest()
    return {'passed':True,'image_id':info['Id'],'base_digest':lock['base_digest'],
            'archive_sha256':digest,'publication_approved':False}


if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('phase',choices=['acquire','pack','paths'])
    parser.add_argument('--lock',type=Path,required=True)
    parser.add_argument('--output',type=Path)
    parser.add_argument('--image-files',type=Path);parser.add_argument('--sources',type=Path)
    parser.add_argument('--image-metadata',type=Path)
    args=parser.parse_args()
    if args.phase=='acquire':acquire(args.lock,args.output)
    elif args.phase=='paths':print('\n'.join(r['file'] for r in read_lock(args.lock)['image_files']))
    else:print(json.dumps(pack(args.lock,args.image_files,args.sources,args.image_metadata,args.output)))
