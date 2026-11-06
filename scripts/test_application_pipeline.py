import json
import hashlib
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
import shutil

ROOT=Path(__file__).resolve().parents[1]


def docker_fixture():
    """Docker boundary only: build outputs and command failures, never source execution."""
    import sys
    args=sys.argv[1:];work=Path(os.environ['FIXTURE_WORK']);work.mkdir(exist_ok=True)
    with open(os.environ['CALLS'],'a') as stream:stream.write(json.dumps(args)+'\n')
    digest=lambda data:hashlib.sha256(data).hexdigest()
    version={'version':'0.0.0-dev','revision':'unreleased'}
    def write(name,value):
        path=work/name;path.parent.mkdir(parents=True,exist_ok=True)
        path.write_bytes(value if isinstance(value,bytes) else json.dumps(value).encode())
    if args[:1]==['run'] and '--name' in args:
        name=args[args.index('--name')+1]
        if name.endswith('-build'):
            roles={}
            for role,binary,data in [('core','scanner',b'app'),('tesseract','scanner-tesseract-api',b'service')]:
                write('runtime-images/'+role+'/'+binary,data)
                write('runtime-images/'+role+'/LICENSE',b'license')
                roles[role]={'binary':{'path':binary,'sha256':digest(data)},'native_manifest_sha256':'c'*64}
                write(role+'-native-sbom/receipt.json',{'passed':True})
            inputs={'source_revision':'a'*40,'roles':roles}
            write('runtime-images/runtime-image-inputs.json',inputs)
            write('package/scanner-onefile-linux-amd64',b'onefile')
            write('package/build.json',{'binary_sha256':digest(b'onefile'),'app_sha256':digest(b'app')})
            for filename in ['manifest.json','SHA256SUMS']:write('package/'+filename,b'fixture')
            write('package/LICENSE',b'license')
            for filename in ['native-origin.cdx.json','native-origin-inventory.json','receipt.json']:write('native-sbom/'+filename,b'fixture')
            write('onefile-sbom/receipt.json',{})
            write('application-sources.tar',b'source')
            write('application.json',{'passed':True,'source_revision':'a'*40,'version':version,
                'native_archive_sha256':digest(b'native'),
                'runtime_inputs_sha256':digest((work/'runtime-images/runtime-image-inputs.json').read_bytes())})
            for filename in ['app-buildinfo','launcher-buildinfo','service-buildinfo','service-version']:write(filename+'.json',{})
        elif name.endswith('-inventory'):
            if os.environ['FAILURE']=='inventory':sys.exit(19)
            for prefix in ['image','tesseract-image']:
                write(prefix+'-sbom/receipt.json',{'passed':True})
                write(prefix+'-payload.json',{'passed':True})
            write('base-sources.tar',b'base');write('base-inventory.json',{'passed':True})
    elif args[:1]==['build'] and args[-1].endswith('/tesseract') and os.environ['FAILURE']=='tesseract-build':sys.exit(18)
    elif args[:2]==['image','inspect']:
        service=args[-1].endswith(':tesseract')
        print(json.dumps([{'Id':'sha256:'+('e' if service else 'd')*64,
            'Config':{'Entrypoint':['/usr/local/bin/'+('scanner-tesseract-api' if service else 'scanner')],
                      'Cmd':['serve'],'User':'10001:10001'}}]))
    elif args[:1]==['inspect']:print(json.dumps([{'State':{'Running':False,'OOMKilled':False,'ExitCode':0}}]))
    elif args[:1]==['start']:
        if args[-1].endswith('-tesseract-smoke'):version['native_runtime_sha256']='f'*64 if os.environ['FAILURE']=='wrong-runtime' else 'c'*64
        print(json.dumps(version))
    elif args[:2]==['image','save'] or args[:1]==['export']:
        Path(args[args.index('--output')+1]).write_bytes(b'archive')
    elif args[:1]==['cp']:
        origin,destination=args[1:];remote=origin.split(':',1)[1]
        if remote.startswith('/work/'):
            path=work/remote.removeprefix('/work/')
            if path.is_dir():shutil.copytree(path,destination)
            else:shutil.copyfile(path,destination)
        else:
            Path(destination).write_bytes(b'service' if remote.endswith('scanner-tesseract-api') else b'app')


class ApplicationPipelineTests(unittest.TestCase):
    def invoke(self,failure,foreign_goroot=False,complete=False):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary);tools=root/'bin';tools.mkdir();node=root/'node';(node/'bin').mkdir(parents=True)
            npm=node/'lib/node_modules/npm/bin';npm.mkdir(parents=True);(npm/'npm-cli.js').write_text('fixture')
            goroot=root/'go';(goroot/'bin').mkdir(parents=True);calls=root/'calls.jsonl';out=root/'output'
            def executable(path,text):path.write_text('#!/bin/sh\n'+text);path.chmod(0o755)
            executable(tools/'go','test "${GOROOT:-}" != /wrong-go || exit 18\nprintf "%s\\n" "$FIXTURE_GO"\n')
            executable(goroot/'bin/go','printf "go version go1.27.3 linux/amd64\\n"\n')
            executable(tools/'node','printf "%s\\n" "$FIXTURE_NODE"\n')
            executable(node/'bin/node','exit 0\n')
            executable(tools/'git','''case "$1" in
rev-parse) printf 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\n';;
bundle) printf bundle > "$3";;
show) printf fixture;;
esac
''')
            docker=tools/'docker';docker.write_text('#!'+sys.executable+'\n'+'''import json,os,sys
args=sys.argv[1:]
with open(os.environ['CALLS'],'a') as f:f.write(json.dumps(args)+'\\n')
if args[:1]==['run'] and '--name' in args and args[args.index('--name')+1].endswith('-'+os.environ['FAILURE']):sys.exit(17)
''');docker.chmod(0o755)
            if complete:
                docker.write_text('#!'+sys.executable+'\nimport sys\nsys.path.insert(0,'+repr(str(ROOT/'scripts'))+')\nfrom test_application_pipeline import docker_fixture\ndocker_fixture()\n')
            native=root/'native.tar';native.write_bytes(b'native')
            env={'PATH':str(tools)+':/usr/bin:/bin','FIXTURE_GO':str(goroot),'FIXTURE_NODE':str(node/'bin/node'),
                 'CALLS':str(calls),'FAILURE':failure,'HOME':str(root),'FIXTURE_WORK':str(root/'work'),
                 'PYTHONDONTWRITEBYTECODE':'1'}
            if foreign_goroot:env['GOROOT']='/wrong-go'
            result=subprocess.run(['/bin/bash',str(ROOT/'scripts/build-application.sh'),str(native),str(out)],env=env,capture_output=True,timeout=20)
            recorded=[json.loads(line) for line in calls.read_text().splitlines()] if calls.exists() else []
            return result,recorded,json.loads((out/'result.json').read_text()) if (out/'result.json').exists() else None

    def test_both_images_are_saved_and_verified_without_exposing_docker_to_inventory(self):
        result,calls,receipt=self.invoke('',complete=True)
        self.assertEqual(result.returncode,0,result.stderr.decode())
        self.assertTrue(receipt['passed'])
        self.assertTrue(receipt['image-payload']['passed'] and receipt['tesseract-image-payload']['passed'])
        self.assertEqual(len([c for c in calls if c[:2]==['image','save']]),2)
        self.assertEqual(len([c for c in calls if c[:1]==['export']]),2)
        inventory=next(c for c in calls if c[:1]==['run'] and '--name' in c and c[c.index('--name')+1].endswith('-inventory'))
        self.assertEqual(inventory[inventory.index('--network')+1],'none')
        self.assertFalse(any(',dst=/runtime-images,' in a for a in inventory))
        for name in ['runtime-inputs.json','core-native-manifest.json','tesseract-native-manifest.json']:
            self.assertTrue(any(',dst=/'+name+',readonly' in a for a in inventory))
        for role in ['image','tesseract-image']:
            self.assertTrue(any(',dst=/'+role+'-rootfs.tar,readonly' in a for a in inventory))
        self.assertFalse(any('docker.sock' in a for a in inventory))

    def test_service_failure_wrong_runtime_or_inventory_failure_cannot_pass_and_cleans_both_images(self):
        for failure in ['tesseract-build','wrong-runtime','inventory']:
            with self.subTest(failure=failure):
                result,calls,receipt=self.invoke(failure,complete=True)
                self.assertNotEqual(result.returncode,0)
                self.assertFalse(receipt['passed'])
                self.assertTrue(any(c[:3]==['image','rm','--force'] and any(a.endswith(':runtime') for a in c)
                                    and any(a.endswith(':tesseract') for a in c) for c in calls))

    def test_acquisition_failure_stops_before_offline_build_and_cleans(self):
        result,calls,receipt=self.invoke('acquire')
        self.assertEqual(result.returncode,17,result.stderr.decode())
        self.assertFalse(any(c[:1]==['run'] and '--name' in c and c[c.index('--name')+1].endswith('-build') for c in calls))
        self.assertTrue(any(c[:2]==['volume','rm'] for c in calls))
        self.assertFalse(receipt['passed'])

    def test_acquisition_does_not_mount_native_binary_input(self):
        result,calls,_=self.invoke('acquire')
        self.assertEqual(result.returncode,17,result.stderr.decode())
        acquirer=next(c for c in calls if c[:1]==['run'] and '--name' in c and c[c.index('--name')+1].endswith('-acquire'))
        self.assertFalse(any(',dst=/native.tar' in a for a in acquirer))

    def test_foreign_goroot_does_not_override_selected_toolchain(self):
        result,_,_=self.invoke('acquire',foreign_goroot=True)
        self.assertEqual(result.returncode,17,result.stderr.decode())

    def test_host_npm_client_is_not_mounted(self):
        result,calls,_=self.invoke('acquire')
        self.assertEqual(result.returncode,17,result.stderr.decode())
        self.assertFalse(any(',dst=/tools/npm' in a for c in calls for a in c))

    def test_existing_output_is_not_overwritten(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary);(root/'native.tar').write_bytes(b'input');out=root/'existing';out.mkdir();(out/'keep').write_text('keep')
            result=subprocess.run(['/bin/bash',str(ROOT/'scripts/build-application.sh'),str(root/'native.tar'),str(out)],capture_output=True,timeout=10)
            self.assertNotEqual(result.returncode,0)
            self.assertEqual((out/'keep').read_text(),'keep')
            self.assertEqual(sorted(p.name for p in out.iterdir()),['keep'])

    def test_compiler_failure_cannot_reach_image_and_build_is_offline(self):
        result,calls,receipt=self.invoke('build')
        self.assertEqual(result.returncode,17,result.stderr.decode())
        builds=[c for c in calls if c[:1]==['run'] and '--name' in c and c[c.index('--name')+1].endswith('-build')]
        self.assertEqual(len(builds),1)
        command=builds[0]
        self.assertEqual(command[command.index('--network')+1],'none')
        self.assertEqual(command[command.index('--user')+1],'10001:10001')
        self.assertIn('--read-only',command)
        self.assertIn('ALL',command)
        self.assertFalse(receipt['passed'])
        self.assertFalse(any(c[:1]==['build'] and any(a.endswith('/image-context') for a in c) for c in calls))


if __name__=='__main__':unittest.main()
