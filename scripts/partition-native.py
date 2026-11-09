#!/usr/bin/env python3
"""Derive verified SANE/core and Tesseract runtime roots without executing inputs.

Run offline with read-only inputs in the existing bounded build sandbox. This
selects ELF dependencies and preserves origins/notices; it grants no release approval.
"""
import argparse
import copy
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import posixpath
import re
import shutil
import stat
import struct

spec=importlib.util.spec_from_file_location('native_sbom',Path(__file__).with_name('native-sbom.py'))
native=importlib.util.module_from_spec(spec);spec.loader.exec_module(native)
ROOTS={
    'core':['usr/bin/scanimage','usr/libexec/scanner/sane-parameters','usr/lib/sane/libsane-airscan.so.1',
            'usr/bin/pdftoppm','usr/bin/pdftotext','usr/bin/pdfinfo'],
    'tesseract':['usr/bin/tesseract'],
}
LOADER='lib/ld-musl-x86_64.so.1'
LIBRARY_DIRS=('lib','usr/lib')


def require(value,message):
    if not value:raise ValueError(message)


def elf_dependencies(data):
    require(64<=len(data)<=64<<20,'ELF file outside bounds')
    fields=struct.unpack_from('<16sHHIQQQIHHHHHH',data)
    ident,kind,machine,version,_,phoff,_,_,ehsize,phsize,count,_,_,_=fields
    require(ident[:7]==b'\x7fELF\x02\x01\x01' and kind in {2,3} and machine==62 and version==1
            and ehsize==64 and phsize==56 and 1<=count<=256 and phoff>=64
            and phoff+count*phsize<=len(data),'unsupported or malformed ELF header')
    programs=[struct.unpack_from('<IIQQQQQQ',data,phoff+i*phsize) for i in range(count)]
    loads=[];dynamic=None;interpreter=None
    for p in programs:
        tag,_,offset,address,_,size,memory,_=p
        if tag not in {1,2,3}:continue
        require(offset+size<=len(data) and memory>=size,'ELF segment outside file')
        if tag==1:loads.append((offset,address,size))
        elif tag==2:
            require(dynamic is None and 0<size<=128<<10 and size%16==0,'invalid ELF dynamic table')
            dynamic=data[offset:offset+size]
        else:
            raw=data[offset:offset+size]
            require(interpreter is None and raw==('/'+LOADER).encode()+b'\0','unexpected ELF interpreter')
            interpreter=LOADER
    needed=[];values={}
    if dynamic is None:return needed,interpreter
    terminated=False
    for i in range(0,len(dynamic),16):
        tag,value=struct.unpack_from('<qQ',dynamic,i)
        if tag==0:terminated=True;break
        if tag==1:needed.append(value)
        elif tag in {5,10}:
            require(tag not in values,'duplicate ELF string table metadata');values[tag]=value
    require(terminated and len(needed)<=128,'unterminated or oversized ELF dependency list')
    if not needed:return [],interpreter
    require(5 in values and 10 in values and 0<values[10]<=8<<20,'missing ELF string table')
    address,size=values[5],values[10]
    matches=[offset+address-base for offset,base,length in loads if base<=address and address+size<=base+length]
    require(len(matches)==1 and matches[0]+size<=len(data),'invalid ELF string table mapping')
    strings=data[matches[0]:matches[0]+size];names=[]
    for offset in needed:
        require(offset<len(strings),'ELF dependency string outside table')
        end=strings.find(b'\0',offset)
        require(end>=offset and 0<end-offset<=128,'invalid ELF dependency string')
        try:name=strings[offset:end].decode('ascii')
        except UnicodeDecodeError as error:raise ValueError('non-ASCII ELF dependency') from error
        require(re.fullmatch(r'[A-Za-z0-9_+.-]+',name) and name not in {'.','..'} and name not in names,
                'unsafe or duplicate ELF dependency')
        names.append(name)
    return names,interpreter


def resolve(files,name):
    chain=[]
    while True:
        require(name in files and name not in chain,'unregistered or cyclic native dependency')
        chain.append(name)
        if not files[name].get('link'):return chain
        name=posixpath.normpath(posixpath.join(posixpath.dirname(name),files[name]['link']))


def read_elf(root,name,entry):
    with native.assembler.source_parent(root,name) as (parent,leaf):
        fd=os.open(leaf,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK,dir_fd=parent)
        with os.fdopen(fd,'rb') as stream:
            info=os.fstat(stream.fileno())
            require(stat.S_ISREG(info.st_mode) and info.st_size==entry['size'] and info.st_size<=64<<20,
                    'ELF input type or size changed')
            data=stream.read(entry['size']+1)
            require(len(data)==entry['size'] and hashlib.sha256(data).hexdigest()==entry['sha256'],
                    'ELF input bytes changed')
    return elf_dependencies(data)


def closure(root,files,roots):
    selected=set();graph={};pending=list(roots)
    while pending:
        chain=resolve(files,pending.pop());selected.update(chain);name=chain[-1]
        if name in graph:continue
        needed,interpreter=read_elf(root,name,files[name]);children=[]
        for library in needed:
            candidates=[directory+'/'+library for directory in LIBRARY_DIRS if directory+'/'+library in files]
            require(len(candidates)==1,'native dependency is absent or ambiguous')
            children.append(candidates[0])
        if interpreter:children.append(interpreter)
        graph[name]=sorted(children);pending.extend(children)
    # Retain soname/development aliases only when they resolve to an included library.
    for name,entry in files.items():
        if entry.get('link') and name.startswith(tuple(d+'/' for d in LIBRARY_DIRS)):
            chain=resolve(files,name)
            if chain[-1] in selected:selected.update(chain)
    return selected,graph


def origin_for(origin,files,role,parent,graph):
    out=copy.deepcopy(origin)
    for name,item in list(out['apk_source_components'].items()):
        names=[n for n in item['retained_files'] if n in files]
        if not names:del out['apk_source_components'][name];continue
        item['retained_files']=names
        if 'relocated_from' in item:item['relocated_from']={n:v for n,v in item['relocated_from'].items() if n in files}
    for category in ['upstream_source_components','firstparty_components']:
        rows=[]
        for item in out[category]:
            item['files']={n:v for n,v in item['files'].items() if n in files}
            if item['files']:rows.append(item)
        out[category]=rows
    out['notice_files']=[n for n in out['notice_files'] if n in files]
    if 'notice_sources' in out:out['notice_sources']={n:v for n,v in out['notice_sources'].items() if n in files}
    out['partition']={'role':role,'parent_manifest_sha256':parent,'roots':ROOTS[role],'elf_dependencies':graph}
    return out


def copy_file(root,target,name,entry):
    destination=target/name;destination.parent.mkdir(parents=True,exist_ok=True,mode=0o755)
    with native.assembler.source_parent(root,name) as (parent,leaf):
        fd=os.open(leaf,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK,dir_fd=parent)
        with os.fdopen(fd,'rb') as source,destination.open('xb') as out:
            info=os.fstat(source.fileno());require(stat.S_ISREG(info.st_mode) and info.st_size==entry['size'],
                                                  'native copy input changed')
            digest=hashlib.sha256();size=0
            while chunk:=source.read(min(1<<20,entry['size']-size+1)):
                size+=len(chunk);require(size<=entry['size'],'native copy grew')
                digest.update(chunk);out.write(chunk)
            require(size==entry['size'] and digest.hexdigest()==entry['sha256'],'native copy hash changed')
    destination.chmod(entry['mode'])


def partition(manifest,provenance,root,output):
    if output.exists() or output.is_symlink():raise FileExistsError('partition output exists')
    root=root.resolve(strict=True)
    require(not output.resolve().is_relative_to(root),'partition output must be separate from input')
    origin=json.loads(provenance.read_bytes());files,_=native.assembler.plan(manifest,origin)
    native.verify_native(root,files)
    parent=origin['runtime_manifest_sha256'];selections={};graphs={}
    for role,roots in ROOTS.items():selections[role],graphs[role]=closure(root,files,roots)
    require('usr/share/scanner-kiosk/ca-certificates.crt' in files,'missing native certificate bundle')
    selections['core'].add('usr/share/scanner-kiosk/ca-certificates.crt')
    for name in ['usr/share/poppler/cMap/Adobe-Japan1/90ms-RKSJ-H',
                 'usr/share/poppler/cidToUnicode/Adobe-Japan1']:
        require(name in files,'missing Japanese PDF character mapping')
    selections['core'].update(n for n in files if n.startswith('usr/share/poppler/'))
    for name in ['eng','jpn','osd']:
        require('usr/share/tessdata/'+name+'.traineddata' in files,'missing Tesseract dictionary')
    selections['tesseract'].update(n for n in files if n.startswith('usr/share/tessdata/'))
    # A small notice superset preserves every existing redistribution condition.
    for names in selections.values():names.update(origin['notice_files'])
    proof={'parent_manifest_sha256':parent,'roles':{},'publication_approved':False}
    output.mkdir(mode=0o700)
    try:
        for role,names in selections.items():
            target=output/role;target.mkdir(mode=0o700);tree=target/'native';tree.mkdir(mode=0o755)
            subset={n:files[n] for n in sorted(names)}
            for name,entry in subset.items():
                if not entry.get('link'):copy_file(root,tree,name,entry)
            for name,entry in subset.items():
                if entry.get('link'):
                    (tree/name).parent.mkdir(parents=True,exist_ok=True,mode=0o755)
                    (tree/name).symlink_to(entry['link'])
            raw=(json.dumps({'files':subset},indent=2,sort_keys=True)+'\n').encode()
            (target/'native-manifest.json').write_bytes(raw)
            child_origin=origin_for(origin,subset,role,parent,graphs[role])
            child_origin['runtime_manifest_sha256']=hashlib.sha256(raw).hexdigest()
            (target/'native-provenance.json').write_text(json.dumps(child_origin,indent=2,sort_keys=True)+'\n')
            native.assembler.plan(target/'native-manifest.json',child_origin)
            native.verify_native(tree,subset)
            proof['roles'][role]={'native_manifest_sha256':child_origin['runtime_manifest_sha256'],
                'provenance_sha256':hashlib.sha256((target/'native-provenance.json').read_bytes()).hexdigest(),
                'entries':len(subset),'bytes':sum(e['size'] for e in subset.values())}
        (output/'partition.json').write_text(json.dumps(proof,indent=2,sort_keys=True)+'\n')
    except Exception:
        shutil.rmtree(output);raise
    return proof


if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    for name in ['manifest','provenance','native-root','output']:parser.add_argument('--'+name,type=Path,required=True)
    args=parser.parse_args()
    print(json.dumps(partition(args.manifest,args.provenance,args.native_root,args.output),sort_keys=True))
