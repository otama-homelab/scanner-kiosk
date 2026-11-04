#!/usr/bin/env python3
"""Build the locked native payload from a clean Git tree; never publish it."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess
import tempfile
import time
import uuid

ROOT = Path(__file__).resolve().parents[1]
EXTRACT_SOURCE = '''import tarfile
from pathlib import PurePosixPath
with tarfile.open('/source.tar') as archive:
    members = archive.getmembers()
    assert len(members) <= 8192 and sum(m.size for m in members) <= 64 << 20
    assert all((m.name == 'source' and m.isdir()) or (m.name.startswith('source/') and '..' not in PurePosixPath(m.name).parts and (m.isfile() or m.isdir() or m.issym())) for m in members)
    archive.extractall('/snapshot', filter='data')
'''
ASSEMBLE = '''import subprocess,sys,tarfile,pathlib
source = pathlib.Path('/snapshot/source')
subprocess.run(['python3',str(source/'scripts/prepare-native-apks.py'),
 '--lock',str(source/'native/runtime-apks.lock.json'),
 '--manifest',str(source/'native/runtime-manifest.json'),
 '--provenance',str(source/'native/runtime-provenance.json'),
 '--archives','/inputs/runtime-apks','--keys','/build-keys',
 '--apk','/usr/local/bin/apk','--output','/work/apk'],check=True,stdout=sys.stderr)
pathlib.Path('/work/notices').mkdir()
subprocess.run(['python3',str(source/'scripts/assemble-native.py'),
 '--manifest',str(source/'native/runtime-manifest.json'),
 '--provenance',str(source/'native/runtime-provenance.json'),
 '--apk-root','/work/apk','--built-archive','/builds/helper.tar',
 '--built-archive','/builds/tesseract.tar','--notices-root','/work/notices',
 '--output','/work/native'],check=True,stdout=sys.stderr)
with tarfile.open(fileobj=sys.stdout.buffer,mode='w|') as archive:
    archive.add('/work/native',arcname='native')
'''


def build(root, output, retain_sources=False):
    root, output = root.resolve(), output.absolute()
    if output.exists() or output.is_symlink():
        raise FileExistsError('native output directory exists')
    if any(',' in str(p) or '\n' in str(p) for p in [root,output]):
        raise ValueError('Docker bind paths cannot contain comma or newline')
    output.mkdir(mode=0o700)
    prefix = 'scanner-native-'+uuid.uuid4().hex
    volumes, containers, images, commands = [], [], [], []
    result = {'passed':False, 'publication_approved':False, 'commands':commands}
    error = None
    deadline = time.monotonic()+2700

    def call(label, args, timeout=300, binary=None, capture=False):
        timeout = min(timeout, max(1, deadline-time.monotonic()))
        if time.monotonic() >= deadline:
            raise TimeoutError('native pipeline exceeded 2700 seconds')
        entry = {'stage':label, 'command':args, 'timeout_seconds':timeout}
        commands.append(entry)
        with (output/(label+'.stderr')).open('wb') as diagnostics:
            with (binary or output/(label+'.stdout')).open('wb') as log:
                try:
                    completed = subprocess.run(args, cwd=root, check=True, timeout=timeout,
                                               stdout=subprocess.PIPE if capture else log, stderr=diagnostics)
                except subprocess.CalledProcessError as exc:
                    entry['exit_code'] = exc.returncode
                    raise
                except subprocess.TimeoutExpired:
                    entry['timed_out'] = True
                    raise
        entry['exit_code'] = completed.returncode
        if binary:
            binary.chmod(0o444)
        return completed.stdout

    def volume(label, size):
        name = prefix+'-'+label
        volumes.append(name)
        call(label,['docker','volume','create','--driver','local','--opt','type=tmpfs',
                    '--opt','device=tmpfs','--opt',f'o=size={size},uid=10001,gid=10001,mode=0700',name])
        return name

    def run(label, image, command, mounts, network='none', memory='512m', scratch='256m', file_limit=536870912):
        name = prefix+'-'+label
        containers.append(name)
        args = ['docker','run','--rm','--name',name,'--network',network,'--read-only',
                '--user','10001:10001','--cap-drop','ALL','--security-opt','no-new-privileges',
                '--memory',memory,'--memory-swap',memory,'--cpus','2','--pids-limit','128',
                '--ulimit','nofile=256:256','--ulimit',f'fsize={file_limit}:{file_limit}',
                '--tmpfs',f'/work:rw,exec,nosuid,nodev,size={scratch},mode=1777','--workdir','/work']
        for mount in mounts:
            args += ['--mount',mount]
        return args+['--entrypoint','/usr/bin/env',image,'-i',
                     'PATH=/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin','HOME=/work/home',
                     'TMPDIR=/work','LC_ALL=C','GOMAXPROCS=2','OMP_THREAD_LIMIT=1',
                     'PYTHONDONTWRITEBYTECODE=1',*command]

    try:
        call('clean-source',['git','diff','--exit-code','HEAD','--'])
        call('tracked-controls',['git','ls-files','--error-unmatch','scripts/build-native.py','scripts/fetch-tesseract-source.py'])
        revision = call('revision',['git','rev-parse','HEAD'],capture=True).decode().strip()
        if not re.fullmatch('[0-9a-f]{40}',revision):
            raise ValueError('invalid source revision')
        result['source_revision'] = revision
        source_tar = output/'source.tar'
        call('source-archive',['git','archive','--format=tar','--prefix=source/',revision],binary=source_tar)
        if not 0 < source_tar.stat().st_size <= 64 << 20:
            raise ValueError('source archive outside bounds')
        with source_tar.open('rb') as stream:
            result['source_archive_sha256'] = hashlib.file_digest(stream,'sha256').hexdigest()
        # Pull immutable official bases during acquisition, before offline SDK installation.
        bases = []
        for filename in ['Dockerfile.apk-fetch','Dockerfile.sane-sdk','Dockerfile.tesseract-sdk']:
            for line in (root/'native'/filename).read_text().splitlines():
                if line.startswith('FROM '):
                    image = line.split()[1]
                    if not re.fullmatch('[A-Za-z0-9_./:-]+@sha256:[0-9a-f]{64}',image):
                        raise ValueError('bootstrap and SDK bases must be digest pinned')
                    if image not in bases:
                        bases.append(image)
        for index, image in enumerate(bases):
            call('pull-'+str(index),['docker','pull','--platform','linux/amd64',image],timeout=300)
        bootstrap = prefix+':fetch'
        images.append(bootstrap)
        call('bootstrap',['docker','build','--platform','linux/amd64','--network','none','--file',
                          str(root/'native/Dockerfile.apk-fetch'),'--tag',bootstrap,str(root/'native')])
        snapshot, inputs = volume('source-volume','64m'), volume('input-volume','1g')
        source_mount = f'type=volume,src={snapshot},dst=/snapshot,readonly'
        input_mount = f'type=volume,src={inputs},dst=/inputs,readonly'
        sources = volume('external-source-volume','2g') if retain_sources else None
        source_keeper_mount = ['--mount',f'type=volume,src={sources},dst=/sources,readonly'] if sources else []
        keeper = prefix+'-keeper';containers.append(keeper)
        call('keeper',['docker','run','--detach','--name',keeper,'--network','none','--read-only',
                       '--user','10001:10001','--cap-drop','ALL','--security-opt','no-new-privileges',
                       '--memory','32m','--memory-swap','32m','--cpus','0.25','--pids-limit','8',
                       '--mount',source_mount,'--mount',input_mount,*source_keeper_mount,'--entrypoint','/bin/sleep',bootstrap,'3000'])
        call('snapshot',run('snapshot',bootstrap,['python3','-c',EXTRACT_SOURCE],
             [f'type=volume,src={snapshot},dst=/snapshot',f'type=bind,src={source_tar},dst=/source.tar,readonly']))
        for lock in ['runtime-apks','sane-sdk','tesseract-sdk']:
            call('fetch-'+lock,run('fetch-'+lock,bootstrap,['python3','/snapshot/source/scripts/fetch-native-apks.py',
                 '--lock',f'/snapshot/source/native/{lock}.lock.json','--keys','/build-keys',
                 '--apk','/usr/local/bin/apk','--output',f'/inputs/{lock}'],
                 [source_mount,f'type=volume,src={inputs},dst=/inputs'],network='bridge'))
        call('fetch-tesseract',run('fetch-tesseract',bootstrap,['python3','/snapshot/source/scripts/fetch-tesseract-source.py',
             '--lock','/snapshot/source/native/tesseract-source.lock.json','--output','/inputs/source.tar.gz'],
             [source_mount,f'type=volume,src={inputs},dst=/inputs'],network='bridge'))
        call('source-controls',run('source-controls',bootstrap,['python3','-c',
             "import shutil; shutil.copyfile('/snapshot/source/native/tesseract-source.SHA256SUMS','/inputs/tesseract-source.SHA256SUMS')"],
             [source_mount,f'type=volume,src={inputs},dst=/inputs']))
        if retain_sources:
            source_inputs = f'type=volume,src={sources},dst=/sources'
            call('fetch-external-sources',run('fetch-external-sources',bootstrap,
                 ['python3','/snapshot/source/scripts/fetch-source-inputs.py',
                  '--lock','/snapshot/source/native/source-inputs.lock.json','--output','/sources/inputs'],
                 [source_mount,source_inputs],network='bridge',memory='2g',file_limit=1 << 30),timeout=900)
            call('pack-external-sources',run('pack-external-sources',bootstrap,
                 ['/bin/sh','-c','python3 /snapshot/source/scripts/pack-source-inputs.py '
                  '--lock /snapshot/source/native/source-inputs.lock.json '
                  '--inputs /sources/inputs --output /work/external-sources.tar '
                  '&& cat /work/external-sources.tar'],
                 [source_mount,source_inputs+',readonly'],memory='3g',scratch='2g',
                 file_limit=2 << 30),timeout=300,binary=output/'external-sources.tar')
            call('export-tesseract-source',run('export-tesseract-source',bootstrap,
                 ['/bin/cat','/inputs/source.tar.gz'],[input_mount]),
                 binary=output/'tesseract-source.tar.gz')
            for filename, key, limit in [('external-sources.tar','external_sources_sha256',2 << 30),
                                         ('tesseract-source.tar.gz','tesseract_source_sha256',128 << 20)]:
                path = output/filename
                if not 1024 <= path.stat().st_size <= limit:
                    raise ValueError('missing or oversized retained source export')
                with path.open('rb') as stream:
                    result[key] = hashlib.file_digest(stream,'sha256').hexdigest()
        with tempfile.TemporaryDirectory(prefix='native-sdk-',dir=output) as temporary:
            context_root = Path(temporary)
            sdks = {}
            for sdk in ['sane','tesseract']:
                context = context_root/sdk;context.mkdir()
                for filename in [f'Dockerfile.{sdk}-sdk',f'{sdk}-sdk.SHA256SUMS','alpine-sdk-keys.SHA256SUMS']:
                    call('copy-'+sdk+'-'+filename,['docker','cp',keeper+':/snapshot/source/native/'+filename,str(context/filename)])
                call('copy-'+sdk+'-apks',['docker','cp',keeper+f':/inputs/{sdk}-sdk',str(context/'apks')])
                tag = prefix+':'+sdk
                images.append(tag)
                call('sdk-'+sdk,['docker','build','--platform','linux/amd64','--network','none',
                     '--file',str(context/f'Dockerfile.{sdk}-sdk'),'--tag',tag,str(context)],timeout=600)
                sdks[sdk] = tag
            trap = '''set -eu; trap 'code=$?; trap - EXIT; if [ "$code" -ne 0 ]; then for log in output/*.log; do test ! -f "$log" || tail -c 65536 "$log" >&2 || :; done; fi; exit "$code"' EXIT; '''
            call('tesseract',run('tesseract',sdks['tesseract'],['/bin/sh','-c',trap+
                 'sh /snapshot/source/native/build-tesseract.sh >&2; cat /work/native.tar'],
                 [source_mount,input_mount],
                 memory='4g',scratch='3g'),timeout=1200,binary=output/'tesseract.tar')
            call('helper',run('helper',sdks['sane'],['/bin/sh','-c',trap+
                 'sh /snapshot/source/native/build-sane-parameters.sh /snapshot/source /snapshot/source/native/helper-source.SHA256SUMS >&2; cat /work/native.tar'],
                 [source_mount],memory='1g',scratch='512m'),timeout=300,binary=output/'helper.tar')
        for filename in ['helper.tar','tesseract.tar']:
            if not 1024 <= (output/filename).stat().st_size <= 128 << 20:
                raise ValueError('missing or oversized native build export')
        call('assemble',run('assemble',bootstrap,['python3','-c',ASSEMBLE],
             [source_mount,input_mount,
              f'type=bind,src={output}/helper.tar,dst=/builds/helper.tar,readonly',
              f'type=bind,src={output}/tesseract.tar,dst=/builds/tesseract.tar,readonly'],
             memory='1g',scratch='512m'),timeout=180,binary=output/'native.tar')
        if not 1024 <= (output/'native.tar').stat().st_size <= 512 << 20:
            raise ValueError('missing or oversized assembled native export')
        with (output/'native.tar').open('rb') as stream:
            result['native_archive_sha256'] = hashlib.file_digest(stream,'sha256').hexdigest()
        result['passed'] = True
    except Exception as exc:
        error = exc
        result['error'] = str(exc)
    finally:
        cleanup = ([(['docker','rm','--force',*containers],False)] if containers else [])
        cleanup += ([(['docker','image','rm','--force',*images],True)] if images else [])
        cleanup += [(['docker','volume','rm',v],True) for v in volumes]
        for args, checked in cleanup:
            try:
                subprocess.run(args,check=checked,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=60)
            except Exception as exc:
                error = error or exc
                result.setdefault('cleanup_errors',[]).append(str(exc))
        if error:
            result['passed'] = False
            (output/'native.tar').unlink(missing_ok=True)
        (output/'result.json').write_text(json.dumps(result,indent=2)+'\n')
    if error:
        raise error
    return result


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output-dir',type=Path,required=True)
    parser.add_argument('--retain-sources',action='store_true',help='Acquire and export locked external source data for distribution')
    args = parser.parse_args()
    print(json.dumps(build(ROOT,args.output_dir,retain_sources=args.retain_sources)))
