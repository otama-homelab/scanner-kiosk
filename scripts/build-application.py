#!/usr/bin/env python3
"""Application stages run only inside the bounded build-application.sh sandbox."""
import argparse
import hashlib
import json
from pathlib import Path, PurePosixPath
import re
import subprocess
import tarfile
import urllib.request
import io

WORK=Path('/work')
LOCKS=['go.mod','go.sum','web/package.json','web/package-lock.json','web/npm-tool.lock.json']


def environment(offline):
    return {'PATH':'/work/bin:/tools:/go/bin:/usr/bin:/bin','HOME':'/work/home',
            'TMPDIR':'/work/tmp','XDG_CACHE_HOME':'/work/cache','XDG_CONFIG_HOME':'/work/config',
            'GOTOOLCHAIN':'local','GOROOT':'/go','GOENV':'off','GOWORK':'off','GOFLAGS':'-mod=readonly',
            'GOMODCACHE':'/work/modules','GOCACHE':'/work/gocache','GOPATH':'/work/gopath',
            'GOPROXY':'off' if offline else 'https://proxy.golang.org',
            'GOSUMDB':'off' if offline else 'sum.golang.org','GOMAXPROCS':'2',
            'CGO_ENABLED':'0','GOOS':'linux','GOARCH':'amd64','PYTHONDONTWRITEBYTECODE':'1',
            'NPM_CONFIG_USERCONFIG':'/work/user.npmrc','NPM_CONFIG_GLOBALCONFIG':'/work/global.npmrc',
            'NPM_CONFIG_CACHE':'/work/npm-cache','NPM_CONFIG_OFFLINE':str(offline).lower(),
            'NPM_CONFIG_UPDATE_NOTIFIER':'false','ASTRO_TELEMETRY_DISABLED':'1'}


def run(args,offline,cwd=None):
    return subprocess.run(args,cwd=cwd or WORK/'source',env=environment(offline),check=True,
                          capture_output=False,timeout=900)


def hashes(source):
    return {name:hashlib.sha256((source/name).read_bytes()).hexdigest() for name in LOCKS}


def extract_native(archive,target):
    with tarfile.open(archive,mode='r:') as stream:
        members=stream.getmembers()
        names=[m.name for m in members]
        if (len(members)>4096 or len(set(names))!=len(names)
                or sum(m.size for m in members)>512<<20
                or any(not ((m.name=='native' and m.isdir()) or
                       (m.name.startswith('native/') and '..' not in PurePosixPath(m.name).parts
                        and PurePosixPath(m.name).as_posix()==m.name
                        and (m.isfile() or m.isdir() or m.issym()))) for m in members)
                or any(m.isfile() and m.mode not in [0o444,0o555] for m in members)):
            raise ValueError('invalid native archive')
        links={m.name for m in members if m.issym()}
        if any(any(str(parent) in links for parent in PurePosixPath(m.name).parents) for m in members):
            raise ValueError('native archive has a symlink parent')
        root=target/'native'
        root.mkdir(mode=0o700)
        selected=[m.replace(name=m.name.removeprefix('native/')) for m in members if m.name!='native']
        stream.extractall(root,members=selected,filter='data')
        # data_filter adds owner write; restore only the validated pinned file modes.
        for member in selected:
            if member.isfile():(root/member.name).chmod(member.mode)


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self,request,fp,code,msg,headers,url):return None


def install_npm(source):
    lock=json.loads((source/'web/npm-tool.lock.json').read_text())
    if not isinstance(lock,dict):raise ValueError('npm client lock must be an object')
    version,digest,size=(lock.get(k) for k in ['version','sha256','bytes'])
    if (not isinstance(version,str) or not re.fullmatch(r'[0-9]+\.[0-9]+\.[0-9]+',version)
            or lock.get('url')!='https://registry.npmjs.org/npm/-/npm-'+version+'.tgz'
            or not isinstance(digest,str) or not re.fullmatch('[0-9a-f]{64}',digest)
            or type(size) is not int or not 0<size<=16<<20):
        raise ValueError('invalid pinned npm client')
    opener=urllib.request.build_opener(urllib.request.ProxyHandler({}),NoRedirect())
    with opener.open(urllib.request.Request(lock['url'],headers={'User-Agent':'scanner-kiosk-build-inputs'}),timeout=60) as response:
        data=response.read(size+1)
    if len(data)!=size or hashlib.sha256(data).hexdigest()!=digest:
        raise ValueError('npm client archive does not match lock')
    with tarfile.open(fileobj=io.BytesIO(data),mode='r:gz') as archive:
        members=archive.getmembers()
        if (len(members)>8192 or sum(m.size for m in members)>64<<20
                or any(not ((m.name=='package' and m.isdir()) or
                   (m.name.startswith('package/') and '..' not in PurePosixPath(m.name).parts
                    and (m.isfile() or m.isdir() or m.issym()))) for m in members)):
            raise ValueError('invalid npm client archive')
        (WORK/'npm-tool').mkdir()
        archive.extractall(WORK/'npm-tool',filter='data')
    (WORK/'bin/npm').symlink_to(WORK/'npm-tool/package/bin/npm-cli.js')
    return lock


def acquire(revision,ref):
    for name in ['bin','home','tmp','cache','config','source']:(WORK/name).mkdir()
    for name in ['user.npmrc','global.npmrc']:(WORK/name).write_text('')
    checks=[(['node','--version'],'v24.22.2'),
            (['go','version'],'go version go1.27.3 linux/amd64')]
    for args,expected in checks:
        actual=subprocess.check_output(args,env=environment(False),text=True).strip()
        if actual!=expected:raise ValueError('unexpected build tool version: '+actual)
    source=WORK/'source'
    run(['git','init','--quiet'],False)
    refs=['HEAD:refs/heads/candidate','refs/remotes/origin/main:refs/remotes/origin/main']
    if ref and ref.startswith('refs/tags/'):refs.append(ref+':'+ref)
    run(['git','fetch','--quiet','--no-tags','/source.bundle',*refs],False)
    run(['git','checkout','--quiet','--detach',revision],False)
    if (source/'.npmrc').exists() or (source/'web/.npmrc').exists():
        raise ValueError('project npm configuration is not a build input')
    tool=install_npm(source)
    if subprocess.check_output(['npm','--version'],env=environment(False),text=True).strip()!=tool['version']:
        raise ValueError('npm client version does not match lock')
    before=hashes(source)
    run(['go','mod','download'],False)
    run(['go','mod','verify'],False)
    run(['npm','ci','--ignore-scripts','--no-audit','--no-fund'],False,source/'web')
    if hashes(source)!=before:raise ValueError('dependency locks changed during acquisition')
    (WORK/'acquisition.json').write_text(json.dumps({'source_revision':revision,'locks':before,'npm_tool':tool})+'\n')


def build(revision,ref,sha):
    source=WORK/'source'
    acquired=json.loads((WORK/'acquisition.json').read_text())
    if acquired['source_revision']!=revision or acquired['locks']!=hashes(source):
        raise ValueError('acquired source and dependency inputs do not match')
    command=['python3','scripts/build-web.py','--output','/work/app-scanner']
    if ref:command+=['--ref',ref,'--sha',sha]
    run(command,True)
    if hashes(source)!=acquired['locks']:raise ValueError('dependency locks changed during build')
    run(['git','diff','--exit-code','HEAD','--'],True)
    extract_native(Path('/native.tar'),WORK)
    run(['python3','scripts/build-standalone.py','--app','/work/app-scanner',
         '--native','/work/native','--native-manifest','native/runtime-manifest.json',
         '--output-dir','/work/package'],True)
    run(['python3','scripts/native-sbom.py','--native-manifest','native/runtime-manifest.json',
         '--provenance','native/runtime-provenance.json','--native-root','/work/native',
         '--output-dir','/work/native-sbom'],True)
    version=json.loads(subprocess.check_output(['/work/package/scanner-onefile-linux-amd64','version'],
                       env=environment(True),timeout=60))
    expected=json.loads(subprocess.check_output(['python3','scripts/check-release-ref.py',
                       '--ref',ref,'--sha',sha],cwd=source,env=environment(True),timeout=30)) if ref else {'version':'0.0.0-dev','source_sha':'unreleased'}
    if version['version']!=expected['version'] or version['revision']!=expected['source_sha']:
        raise ValueError('packaged executable has wrong source identity')
    with Path('/native.tar').open('rb') as stream:native_sha=hashlib.file_digest(stream,'sha256').hexdigest()
    for name,binary in [('app-buildinfo','/work/app-scanner'),('launcher-buildinfo','/work/package/scanner-onefile-linux-amd64')]:
        info=subprocess.check_output(['go','version','-m','-json',binary],env=environment(True),timeout=30)
        (WORK/(name+'.json')).write_bytes(info)
    result={'passed':True,'source_revision':revision,'version':version,'locks':acquired['locks'],
            'native_archive_sha256':native_sha,'publication_approved':False,
            'npm_tool':acquired['npm_tool'],
            'assets':{p.relative_to(source/'web/dist').as_posix():hashlib.sha256(p.read_bytes()).hexdigest()
                      for p in (source/'web/dist').rglob('*') if p.is_file()}}
    (WORK/'application.json').write_text(json.dumps(result,indent=2)+'\n')


if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('phase',choices=['acquire','build'])
    parser.add_argument('--revision',required=True)
    parser.add_argument('--ref');parser.add_argument('--sha')
    args=parser.parse_args()
    if not re.fullmatch('[0-9a-f]{40}',args.revision) or bool(args.ref)!=bool(args.sha):
        parser.error('revision and optional ref/SHA pair required')
    if args.phase=='acquire':acquire(args.revision,args.ref)
    else:build(args.revision,args.ref,args.sha)
