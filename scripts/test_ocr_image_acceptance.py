"""Safety contracts for the actual-image tracer; no Docker daemon is contacted."""
import importlib.util
import json
from pathlib import Path
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import tempfile
import threading
import unittest
import subprocess
from unittest.mock import patch


class OCRImageAcceptanceTest(unittest.TestCase):
    def setUp(self):
        spec=importlib.util.spec_from_file_location('image_acceptance',Path(__file__).with_name('check-ocr-images.py'))
        self.module=importlib.util.module_from_spec(spec);spec.loader.exec_module(self.module)

    def test_both_image_payloads_and_build_must_belong_to_the_same_candidate(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);source='a'*40
            def put(name,value):(root/(name+'.json')).write_text(json.dumps(value))
            put('application',{'passed':True,'source_revision':source,'version':{'revision':source}})
            put('result',{'passed':True,'source_revision':source})
            for prefix,char in [('image','b'),('tesseract-image','c')]:
                put(prefix,[{'Id':'sha256:'+char*64}])
                put(prefix+'-payload',{'passed':True,'image_id':'sha256:'+char*64})
            proof=self.module.candidate_identity(root)
            self.assertEqual(proof['images']['api'],proof['images']['worker'])
            self.assertNotEqual(proof['images']['api'],proof['images']['tesseract'])
            put('tesseract-image-payload',{'passed':True,'image_id':'sha256:'+'0'*64})
            with self.assertRaises(ValueError):self.module.candidate_identity(root)
            put('result',{'passed':False,'source_revision':source})
            with self.assertRaises(ValueError):self.module.candidate_identity(root)

    def test_http_uses_the_configured_origin_cookie_and_strict_response_bounds(self):
        seen=[]
        class Handler(BaseHTTPRequestHandler):
            def log_message(self,*args):pass
            def do_GET(self):
                seen.append(dict(self.headers));self.send_response(302 if self.path=='/redirect' else 200)
                self.send_header('Set-Cookie','session=fixture; Secure; HttpOnly');self.end_headers()
                self.wfile.write(b'x'*128 if self.path=='/oversized' else b'{}')
            def do_POST(self):
                seen.append(dict(self.headers));self.rfile.read(int(self.headers.get('Content-Length','0')))
                self.send_response(200);self.end_headers();self.wfile.write(b'{}')
        server=ThreadingHTTPServer(('127.0.0.1',0),Handler)
        thread=threading.Thread(target=server.serve_forever,daemon=True);thread.start()
        try:
            client=self.module.Client('127.0.0.1',server.server_port)
            body,_=client.request('GET','/api/session');self.assertEqual(body,b'{}')
            client.request('POST','/auth/guest',{})
            self.assertEqual(seen[-1]['Host'],'scanner-ocr.invalid')
            self.assertEqual(seen[-1]['Origin'],'https://scanner-ocr.invalid')
            self.assertEqual(seen[-1]['Cookie'],'session=fixture')
            self.assertTrue(seen[-1]['Idempotency-Key'])
            with self.assertRaises(ValueError):client.request('GET','/oversized',max_bytes=32)
            before=len(seen)
            with self.assertRaises(ValueError):client.request('GET','/redirect')
            self.assertEqual(len(seen),before+1,'a redirect must never cause a second request')
        finally:server.shutdown();server.server_close();thread.join(timeout=2)

    def test_failure_keeps_proof_live_and_cleans_only_owned_resources_and_test_key(self):
        for suffix in ['-worker','-full','-copy','-check-coreonly']:
            with self.subTest(suffix=suffix):self.check_start_failure(suffix)

    def check_start_failure(self,suffix):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);candidate=root/'candidate';candidate.mkdir();out=root/'proof';source='a'*40
            for name,value in {'application':{'passed':True,'source_revision':source,'version':{}},
                    'result':{'passed':True,'source_revision':source},
                    'image':[{'Id':'sha256:'+'b'*64}], 'tesseract-image':[{'Id':'sha256:'+'c'*64}],
                    'image-payload':{'passed':True,'image_id':'sha256:'+'b'*64},
                    'tesseract-image-payload':{'passed':True,'image_id':'sha256:'+'c'*64}}.items():
                (candidate/(name+'.json')).write_text(json.dumps(value))
            calls=[];containers={};volumes={};network='f'*64
            def run(args,**kwargs):
                if args[:1]==['git']:return subprocess.CompletedProcess(args,0,'','')
                self.assertEqual(args[0],'docker');calls.append(args);command=args[1:];stdout=''
                if command[:2]==['network','create']:stdout=network
                elif command[:2]==['volume','create']:
                    volumes[command[-1]]=command[command.index('--label')+1].split('=',1)[1]
                    stdout=command[-1]
                elif command[:2]==['volume','inspect']:
                    stdout=json.dumps([{'Labels':{'scanner-kiosk.tracer':volumes[command[-1]]}}])
                elif command[0]=='create':
                    self.assertIn('--pull=never',command);self.assertIn('--read-only',command)
                    self.assertEqual(command[command.index('--user')+1],'10001:10001')
                    self.assertEqual(command[command.index('--cap-drop')+1],'ALL')
                    self.assertIn('no-new-privileges',command)
                    name=command[command.index('--name')+1]
                    if name.endswith('-keeper'):
                        for role in ['state','documents','proof']:
                            self.assertTrue(any('-'+role+',dst=/' in value and value.endswith(',readonly') for value in command),
                                            'all tmpfs stores must survive stopping both core roles')
                    if name.endswith(('-api-coreonly','-worker-coreonly')):
                        self.assertNotIn('--env-file',command)
                        self.assertFalse(any('SCANNER_OCR_' in value for value in command))
                        stopped={c[-1] for c in calls if c[1:2]==['stop']}
                        self.assertTrue(all(cid in stopped for cid,n in containers.items()
                                            if n.endswith(('-api','-worker','-tesseract'))))
                    cid=format(len(containers)+1,'064x');containers[cid]=command[command.index('--name')+1];stdout=cid
                elif command[0]=='inspect':
                    stdout=json.dumps([{'Id':command[-1],'Image':'sha256:'+'b'*64,
                                         'State':{'Running':False,'OOMKilled':False,'ExitCode':0}}])
                elif command[0]=='start':
                    if containers[command[-1]].endswith('-full'):
                        keepers=[cid for cid,name in containers.items() if name.endswith('-keeper')]
                        self.assertEqual(len(keepers),1,'tmpfs proof volume needs a live keeper across helper exits')
                        self.assertTrue(any(c[1:]==['start',keepers[0]] for c in calls))
                    if containers[command[-1]].endswith(suffix):raise subprocess.CalledProcessError(17,args)
                    stdout='scanimage fixture'
                elif command[0]=='logs':
                    self.assertEqual(kwargs['stderr'],subprocess.STDOUT)
                    stdout='synthetic helper stderr retained'
                elif command[0]=='cp':
                    cid,path=command[1].split(':',1)
                    self.assertTrue(containers[cid].endswith('-keeper'),'copy from the live proof mount')
                    if suffix=='-copy':
                        self.assertEqual(path,'/proof/plain.pdf')
                        raise subprocess.CalledProcessError(17,args)
                    Path(command[-1]).write_text(json.dumps({'passed':True,'fixture_sha256':'fixture'}))
                return subprocess.CompletedProcess(args,0,stdout,'')
            with patch.object(self.module.subprocess,'check_output',return_value=source),patch.object(self.module.subprocess,'run',side_effect=run):
                with self.assertRaises(subprocess.CalledProcessError):self.module.trace(candidate,out)
            self.assertFalse((out/'test-key.env').exists())
            receipt=json.loads((out/'receipt.json').read_text());self.assertFalse(receipt['passed'])
            self.assertEqual(receipt['cleanup_errors'],[])
            self.assertEqual({c[-1] for c in calls if c[1:3]==['rm','--force']},set(containers))
            self.assertEqual({c[-1] for c in calls if c[1:3]==['volume','rm']},set(volumes))
            self.assertEqual([c[-1] for c in calls if c[1:3]==['network','rm']],[network])
            if suffix=='-full':
                self.assertIn('helper stderr retained',(out/'full-failure.log').read_text())
                removed=[c[-1] for c in calls if c[1:3]==['rm','--force']]
                self.assertTrue(containers[removed[-1]].endswith('-keeper'))


if __name__=='__main__':unittest.main()
