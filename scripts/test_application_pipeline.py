import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT=Path(__file__).resolve().parents[1]


class ApplicationPipelineTests(unittest.TestCase):
    def invoke(self,failure,foreign_goroot=False):
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
            native=root/'native.tar';native.write_bytes(b'native')
            env={'PATH':str(tools)+':/usr/bin:/bin','FIXTURE_GO':str(goroot),'FIXTURE_NODE':str(node/'bin/node'),
                 'CALLS':str(calls),'FAILURE':failure,'HOME':str(root)}
            if foreign_goroot:env['GOROOT']='/wrong-go'
            result=subprocess.run(['/bin/bash',str(ROOT/'scripts/build-application.sh'),str(native),str(out)],env=env,capture_output=True,timeout=20)
            recorded=[json.loads(line) for line in calls.read_text().splitlines()] if calls.exists() else []
            return result,recorded,json.loads((out/'result.json').read_text()) if (out/'result.json').exists() else None

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
