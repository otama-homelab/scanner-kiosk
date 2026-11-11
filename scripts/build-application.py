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
import urllib.parse
import io
import os
import stat
import tempfile

WORK=Path('/work')
LOCKS=['go.mod','go.sum','web/package.json','web/package-lock.json','web/npm-tool.lock.json','native/syft-tool.lock.json','native/base-source.lock.json']


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



class GithubReleaseRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self,request,fp,code,msg,headers,url):
        parsed=urllib.parse.urlsplit(url)
        if (parsed.scheme!='https' or parsed.hostname not in {'github.com','release-assets.githubusercontent.com'}
                or parsed.username or parsed.password or parsed.port not in {None,443}):
            raise ValueError('unexpected release tool redirect')
        return super().redirect_request(request,fp,code,msg,headers,url)


def install_tool(source,name):
    if name not in {'syft','grype'}:raise ValueError('unknown inventory tool')
    lock=json.loads((source/('native/'+name+'-tool.lock.json')).read_text())
    version,digest,size=(lock.get(k) for k in ['version','sha256','bytes'])
    if (not isinstance(version,str) or not re.fullmatch(r'[0-9]+\.[0-9]+\.[0-9]+',version)
            or lock.get('url')!='https://github.com/anchore/'+name+'/releases/download/v'+version+'/'+name+'_'+version+'_linux_amd64.tar.gz'
            or not isinstance(digest,str) or not re.fullmatch('[0-9a-f]{64}',digest)
            or type(size) is not int or not 0<size<=64<<20):
        raise ValueError('invalid pinned inventory tool')
    opener=urllib.request.build_opener(urllib.request.ProxyHandler({}),GithubReleaseRedirect())
    with opener.open(lock['url'],timeout=60) as response:data=response.read(size+1)
    if len(data)!=size or hashlib.sha256(data).hexdigest()!=digest:
        raise ValueError('inventory tool archive does not match lock')
    with tarfile.open(fileobj=io.BytesIO(data),mode='r:gz') as archive:
        members=archive.getmembers()
        selected=[m for m in members if m.name==name]
        if (len(members)>32 or sum(m.size for m in members)>256<<20 or len(selected)!=1
                or not selected[0].isfile() or selected[0].issparse() or not 0<selected[0].size<=128<<20):
            raise ValueError('invalid inventory tool executable member')
        with archive.extractfile(selected[0]) as stream:
            executable=stream.read((128<<20)+1)
        if len(executable)!=selected[0].size:raise ValueError('incomplete inventory tool executable')
    path=WORK/'bin'/name
    with path.open('xb') as target:target.write(executable)
    path.chmod(0o555)
    return lock


def verify_runtime_bases(source,base):
    for name in ['Dockerfile','Dockerfile.tesseract']:
        images=[line.split()[1] for line in (source/name).read_text().splitlines()
                if line.strip() and line.split()[0].upper()=='FROM']
        if len(images)!=1 or not images[0].endswith('@'+base['base_digest']):
            raise ValueError('base source lock differs from runtime base')


def acquire(revision,ref):
    for name in ['bin','home','tmp','cache','config','source']:(WORK/name).mkdir()
    for name in ['user.npmrc','global.npmrc']:(WORK/name).write_text('')
    checks=[(['node','--version'],'v24.22.2'),
            (['go','version'],'go version go1.27.4 linux/amd64')]
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
    install_tool(source,'syft')
    base=json.loads((source/'native/base-source.lock.json').read_text())
    verify_runtime_bases(source,base)
    run(['python3','scripts/base-source.py','acquire','--lock','native/base-source.lock.json',
         '--output','/work/base-source-inputs'],False)
    if subprocess.check_output(['npm','--version'],env=environment(False),text=True).strip()!=tool['version']:
        raise ValueError('npm client version does not match lock')
    before=hashes(source)
    run(['go','mod','download'],False)
    run(['go','mod','verify'],False)
    run(['npm','ci','--ignore-scripts','--no-audit','--no-fund'],False,source/'web')
    if hashes(source)!=before:raise ValueError('dependency locks changed during acquisition')
    (WORK/'acquisition.json').write_text(json.dumps({'source_revision':revision,'locks':before,'npm_tool':tool})+'\n')



def retain_sources(revision):
    """Retain only linked Go modules plus the exact project source, offline."""
    source=WORK/'source'
    output=WORK/'application-sources.tar'
    if output.exists() or output.is_symlink():raise FileExistsError('application sources already exist')
    sums={(parts[0],parts[1]):parts[2] for line in (source/'go.sum').read_text().splitlines()
          if len(parts:=line.split())==3}
    dependencies={}
    for name in ['app-buildinfo','launcher-buildinfo','service-buildinfo']:
        for item in json.loads((WORK/(name+'.json')).read_text()).get('Deps',[]):
            if item.get('Replace'):raise ValueError('replaced module requires explicit source mapping')
            key=(item['Path'],item['Version'])
            if item['Sum']!=sums.get(key):raise ValueError('linked module sum differs from source lock')
            dependencies[key]=item['Sum']
    if len(dependencies)>256:raise ValueError('too many linked modules')
    index={'source_revision':revision,'modules':[],'files':[], 'publication_approved':False,
           'scope':'project source and linked Go module archives; not native/base sources or license approval'}
    with tempfile.TemporaryDirectory(prefix='application-sources-',dir=WORK) as directory:
        temporary=Path(directory)
        (temporary/'project.tar').write_bytes(subprocess.check_output(
            ['git','archive','--format=tar',revision],cwd=source,env=environment(True),timeout=60))
        total=0
        with tarfile.open(temporary/'bundle.tar','w',format=tarfile.USTAR_FORMAT) as archive:
            def add(path,name,limit):
                nonlocal total
                descriptor=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
                with os.fdopen(descriptor,'rb') as stream:
                    info=os.fstat(stream.fileno());total+=info.st_size
                    if not stat.S_ISREG(info.st_mode) or not 0<info.st_size<=limit or total>512<<20:
                        raise ValueError('application source file outside bounds')
                    digest=hashlib.file_digest(stream,'sha256').hexdigest();stream.seek(0)
                    member=tarfile.TarInfo(name);member.size=info.st_size;member.mode=0o444
                    archive.addfile(member,stream)
                    after=os.fstat(stream.fileno())
                    if (info.st_size,info.st_mtime_ns)!=(after.st_size,after.st_mtime_ns):
                        raise ValueError('application source changed')
                    index['files'].append({'path':name,'bytes':info.st_size,'sha256':digest})
            add(temporary/'project.tar','project.tar',64<<20)
            for (name,version),digest in sorted(dependencies.items()):
                item=json.loads(subprocess.check_output(['go','mod','download','-json',name+'@'+version],
                    cwd=source,env=environment(True),timeout=60))
                if (item.get('Path')!=name or item.get('Version')!=version or item.get('Sum')!=digest
                        or item.get('GoModSum')!=sums.get((name,version+'/go.mod'))):
                    raise ValueError('cached module differs from linked module/source lock')
                prefix='modules/'+hashlib.sha256((name+'@'+version).encode()).hexdigest()
                for key,suffix in [('Zip','.zip'),('GoMod','.mod')]:
                    path=Path(item[key])
                    if not path.resolve().is_relative_to((WORK/'modules').resolve()):
                        raise ValueError('module input outside private cache')
                    add(path,prefix+suffix,256<<20)
                index['modules'].append({'path':name,'version':version,'sum':digest,
                                         'go_mod_sum':item['GoModSum'],'archive_prefix':prefix})
            raw=json.dumps(index,sort_keys=True,indent=2).encode()+b'\n'
            member=tarfile.TarInfo('INDEX.json');member.size=len(raw);member.mode=0o444
            archive.addfile(member,io.BytesIO(raw))
        (temporary/'bundle.tar').chmod(0o444)
        os.link(temporary/'bundle.tar',output)


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
    run(['python3','scripts/partition-native.py','--manifest','native/runtime-manifest.json',
         '--provenance','native/runtime-provenance.json','--native-root','/work/native',
         '--output','/work/runtime-roles'],True)
    roles=json.loads((WORK/'runtime-roles/partition.json').read_text())
    runtime_sha=roles['roles']['tesseract']['native_manifest_sha256']
    flags='-s -w -X scanner-kiosk/internal/buildinfo.Version='+version['version']+' -X scanner-kiosk/internal/buildinfo.Revision='+version['revision']
    flags+=' -X scanner-kiosk/internal/buildinfo.NativeRuntimeSHA256='+runtime_sha
    run(['go','build','-trimpath','-buildvcs=false','-ldflags='+flags,'-o','/work/scanner-tesseract-api','./cmd/scanner-tesseract-api'],True)
    service_version=json.loads(subprocess.check_output(['/work/scanner-tesseract-api','version'],env=environment(True),timeout=30))
    if service_version!={**version,'native_runtime_sha256':runtime_sha}:raise ValueError('terminal OCR executable identity differs')
    (WORK/'service-version.json').write_text(json.dumps(service_version,indent=2)+'\n')
    run(['python3','scripts/prepare-runtime-images.py','--source','/work/source','--partitions','/work/runtime-roles',
         '--app','/work/app-scanner','--service','/work/scanner-tesseract-api','--revision',revision,
         '--output','/work/runtime-images'],True)
    for role in ['core','tesseract']:
        root='/work/runtime-images/'+role
        run(['python3','scripts/native-sbom.py','--native-manifest',root+'/native-manifest.json',
             '--provenance',root+'/native-provenance.json','--native-root',root+'/native',
             '--output-dir','/work/'+role+'-native-sbom'],True)
    with Path('/native.tar').open('rb') as stream:native_sha=hashlib.file_digest(stream,'sha256').hexdigest()
    for name,binary in [('app-buildinfo','/work/app-scanner'),('launcher-buildinfo','/work/package/scanner-onefile-linux-amd64'),
                        ('service-buildinfo','/work/scanner-tesseract-api')]:
        info=subprocess.check_output(['go','version','-m','-json',binary],env=environment(True),timeout=30)
        (WORK/(name+'.json')).write_bytes(info)
    result={'passed':True,'source_revision':revision,'version':version,'locks':acquired['locks'],
            'native_archive_sha256':native_sha,'publication_approved':False,
            'npm_tool':acquired['npm_tool'],
            'runtime_inputs_sha256':hashlib.sha256((WORK/'runtime-images/runtime-image-inputs.json').read_bytes()).hexdigest(),
            'assets':{p.relative_to(source/'web/dist').as_posix():hashlib.sha256(p.read_bytes()).hexdigest()
                      for p in (source/'web/dist').rglob('*') if p.is_file()}}
    (WORK/'application.json').write_text(json.dumps(result,indent=2)+'\n')
    run(['go','mod','verify'],True)
    retain_sources(revision)
    run(['python3','scripts/onefile-sbom.py','--binary','/work/package/scanner-onefile-linux-amd64',
         '--build-receipt','/work/package/build.json','--packed-manifest','/work/package/manifest.json',
         '--native-manifest','native/runtime-manifest.json','--provenance','native/runtime-provenance.json',
         '--syft','/work/bin/syft','--output-dir','/work/onefile-sbom'],True)


def inventory():
    for role,prefix in [('core','image'),('tesseract','tesseract-image')]:
        run(['python3','scripts/runtime-image-payload.py','--role',role,
             '--rootfs','/'+prefix+'-rootfs.tar','--manifest','/'+role+'-native-manifest.json',
             '--inputs','/runtime-inputs.json','--metadata','/'+prefix+'.json',
             '--container','/'+prefix+'-container.json','--output','/work/'+prefix+'-payload.json'],True)
        run(['python3','scripts/saved-image-sbom.py','--archive','/'+prefix+'.tar','--metadata','/'+prefix+'.json',
             '--syft','/work/bin/syft','--output','/work/'+prefix+'-sbom'],True)
    with (WORK/'base-inventory.json').open('wb') as receipt:
        subprocess.run(['python3','scripts/base-source.py','pack','--lock','native/base-source.lock.json',
            '--sources','/work/base-source-inputs','--image-files','/base-image','--image-metadata','/image.json',
            '--output','/work/base-sources.tar'],cwd=WORK/'source',env=environment(True),check=True,timeout=60,stdout=receipt)


if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('phase',choices=['acquire','build','inventory'])
    parser.add_argument('--revision',required=True)
    parser.add_argument('--ref');parser.add_argument('--sha')
    args=parser.parse_args()
    if not re.fullmatch('[0-9a-f]{40}',args.revision) or bool(args.ref)!=bool(args.sha):
        parser.error('revision and optional ref/SHA pair required')
    if args.phase=='acquire':acquire(args.revision,args.ref)
    elif args.phase=='build':build(args.revision,args.ref,args.sha)
    else:inventory()
