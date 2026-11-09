#!/usr/bin/env python3
"""Check an embedded product frontend against real API stores and a local TLS proxy."""
import argparse,hashlib,http.client,json,os,socket,sqlite3,ssl,struct,subprocess,tempfile,threading,time,zlib
from datetime import datetime,timezone
from http.server import BaseHTTPRequestHandler,ThreadingHTTPServer
from pathlib import Path
p=argparse.ArgumentParser();p.add_argument('--playwright-module',required=True);p.add_argument('--chromium',required=True);p.add_argument('--receipt',required=True);
for name in ['fixture-python','tesseract-binary','tessdata','font']:p.add_argument('--'+name)
p.add_argument('--scenario',choices=['reocr-start','reocr-compare','reocr-adopt'])
args=p.parse_args()
native_args=[args.fixture_python,args.tesseract_binary,args.tessdata,args.font]
if any(native_args) and not all(native_args):p.error('native OCR verification requires all four fixture/tool options')
root=Path(__file__).resolve().parents[1]
files=sorted([*root.glob('cmd/**/*.go'),*root.glob('internal/**/*.go'),*root.glob('internal/**/*.sql'),*root.glob('pkg/**/*.go'),*root.glob('web/src/**/*.*'),*root.glob('web/public/*'),root/'web/package.json',root/'web/package-lock.json',root/'web/astro.config.mjs',root/'go.mod',root/'go.sum',root/'scripts/build-web.py',root/'scripts/check-web.py',root/'scripts/check-web-editor-ux.cjs',root/'scripts/web-controls.cjs',root/'scripts/check-web.cjs',root/'scripts/check-web-ocr.cjs',root/'scripts/check-web-simple.cjs',root/'scripts/check-web-none.cjs',root/'scripts/check-web-capture.cjs',root/'scripts/check-web-correction.cjs',root/'scripts/check-web-placement.cjs',root/'scripts/check-web-import.cjs',root/'scripts/check-web-rate.cjs',root/'scripts/check-web-keyboard.cjs',root/'scripts/check-web-home.cjs',root/'scripts/check-web-ocr-ui.cjs'])
def hashes():return {str(f.relative_to(root)):hashlib.sha256(f.read_bytes()).hexdigest() for f in files}
before=hashes()
with tempfile.TemporaryDirectory(prefix='scanner-product-web-') as temporary:
 work=Path(temporary);binary=work/'scanner'
 subprocess.run(['python3','scripts/build-web.py','--output',str(binary)],cwd=root,check=True,capture_output=True)
 for name in ['state','documents']:(work/name).mkdir(mode=0o700)
 if all(native_args):
  fixture_code="""
import sys
from PIL import Image,ImageDraw,ImageFont
from pathlib import Path
for index in [1,2]:
 image=Image.new('RGB',(2480,3508),'white');draw=ImageDraw.Draw(image);font=ImageFont.truetype(sys.argv[1],80)
 for y,text in [(200,'日本語の書類'),(400,'Scanner OCR '+str(2025+index)),(600,'テスト文書 300 dpi')]:draw.text((180,y),text,font=font,fill='black')
 image.save(Path(sys.argv[2])/('native-'+str(index)+'.png'),dpi=(300,300))
"""
  subprocess.run([args.fixture_python,'-c',fixture_code,args.font,str(work)],check=True,capture_output=True,timeout=20)

 with socket.socket() as sock:sock.bind(('127.0.0.1',0));backend=sock.getsockname()[1]
 def seed():
  with sqlite3.connect(work/'state/state.sqlite3') as db:owner=db.execute("SELECT id FROM principals WHERE kind='guest' AND disabled_at_ms IS NULL").fetchone()[0]
  def chunk(kind,data):return struct.pack('>I',len(data))+kind+data+struct.pack('>I',zlib.crc32(kind+data))
  image=bytes.fromhex('89504e470d0a1a0a')+chunk(b'IHDR',struct.pack('>IIBBBBB',32,16,8,2,0,0,0))+chunk(b'IDAT',zlib.compress((bytes([0])+bytes([100,120,140])*32)*16))+chunk(b'IEND',b'')
  file=work/'documents/original.png';file.write_bytes(image);file.chmod(0o600);now=int(time.time()*1000)
  with sqlite3.connect(work/'documents/documents.sqlite3') as db:
   db.execute('INSERT INTO owners(id) VALUES(?)',(owner,));db.execute("INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES('document_browser_01',?,'document','review',?,?)",(owner,now,now+3600000))
   db.execute("INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('original_browser_01','document_browser_01',?,'original','ready','original.png','image/png',?,?,1,?)",(owner,len(image),hashlib.sha256(image).digest(),now))
   db.execute("INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,capture_size,capture_color,capture_dpi,pixel_width,pixel_height) VALUES('page_browser_0001','document_browser_01',?,0,'original_browser_01','a4','color',300,32,16)",(owner,))
   second=work/'documents/second.png';second.write_bytes(image);second.chmod(0o600)
   db.execute("INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('original_browser_02','document_browser_01',?,'original','ready','second.png','image/png',?,?,1,?)",(owner,len(image),hashlib.sha256(image).digest(),now))
   db.execute("INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,capture_size,capture_color,capture_dpi,pixel_width,pixel_height) VALUES('page_browser_0002','document_browser_01',?,1,'original_browser_02','a4','color',300,32,16)",(owner,))
 class Proxy(BaseHTTPRequestHandler):
  def log_message(self,*args):pass
  def relay(self):
   if self.path=='/__fixture/native-seed' and all(native_args):
    now=int(time.time()*1000)
    with sqlite3.connect(work/'state/state.sqlite3') as db:owner=db.execute("SELECT id FROM principals WHERE kind='guest' AND disabled_at_ms IS NULL ORDER BY created_at_ms DESC,id DESC LIMIT 1").fetchone()[0]
    with sqlite3.connect(work/'documents/documents.sqlite3') as db:
     db.execute('PRAGMA foreign_keys=ON');db.execute('INSERT INTO owners(id) VALUES(?)',(owner,))
     db.execute("INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES('document_native_001',?,'document','review',?,?)",(owner,now,now+3600000))
     for index in [1,2]:
      raw=(work/('native-'+str(index)+'.png')).read_bytes();relative='native-original-'+str(index)+'.png';file=work/'documents'/relative;file.write_bytes(raw);file.chmod(0o600)
      original='original_native_00'+str(index);page='page_native_0000'+str(index)
      db.execute("INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES(?,'document_native_001',?,'original','ready',?,'image/png',?,?,1,?)",(original,owner,relative,len(raw),hashlib.sha256(raw).digest(),now))
      db.execute("INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,capture_size,capture_color,capture_dpi,rotation,pixel_width,pixel_height) VALUES(?,'document_native_001',?,?,?,'a4','color',300,?,2480,3508)",(page,owner,index-1,original,(index-1)*90))
    self.send_response(200);self.end_headers();return
   if self.path=='/__fixture/ocr-catalog':
    now=int(time.time()*1000);until=now+300000
    model={'id':'local-model','name':'Local model','version':'browser-model-v1','languages':['ja','en'],'quality_presets':['standard'],'outputs':['pdf_text_layer'],'max_pages':50,'max_page_pixels':40000000}
    remote=dict(model,processing_destination='Family OCR server',id='remote-model',name='Remote model',version='browser-model-v2',quality_presets=['fast','accurate'])
    snapshot={'PublishedAtMS':now,'ExpiresAtMS':until,'Providers':[{'api_version':1,'id':'local','name':'Local OCR','version':'v1','external_processing':False,'models':[model]},{'api_version':1,'id':'remote','name':'Remote OCR','version':'v2','external_processing':True,'models':[remote]}],'Defaults':[{'ProviderID':'local','ModelID':'local-model','QualityPreset':'standard'}],'Preferred':{'ProviderID':'local','ModelID':'local-model','Languages':['ja','en'],'QualityPreset':'standard'}}
    with sqlite3.connect(work/'documents/documents.sqlite3') as db:
     generation=db.execute('SELECT COALESCE(MAX(generation),0)+1 FROM processing_coordinator').fetchone()[0];boot='worker_browser_fixture_'+str(generation)
     db.execute("INSERT INTO processing_coordinator(singleton,boot_id,generation,state,created_at_ms,heartbeat_at_ms,valid_until_ms) VALUES(1,?,?,'active',?,?,?) ON CONFLICT(singleton) DO UPDATE SET boot_id=excluded.boot_id,generation=excluded.generation,state='active',created_at_ms=excluded.created_at_ms,heartbeat_at_ms=excluded.heartbeat_at_ms,valid_until_ms=excluded.valid_until_ms",(boot,generation,now,now,until))
     db.execute('INSERT OR REPLACE INTO ocr_catalog_snapshot(singleton,boot_id,generation,published_at_ms,expires_at_ms,payload) VALUES(1,?,?,?,?,?)',(boot,generation,now,until,json.dumps(snapshot)))
    self.send_response(200);self.end_headers();return
   if self.path=='/__fixture/history-delete':
    image=(work/'documents/original.png').read_bytes();file=work/'documents/history-delete.png';file.write_bytes(image);file.chmod(0o600);now=int(time.time()*1000)
    with sqlite3.connect(work/'documents/documents.sqlite3') as db:
     owner=db.execute("SELECT owner_id FROM documents WHERE id='document_browser_01'").fetchone()[0]
     db.execute("INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES('document_history_delete_01',?,'photo','review',?,?)",(owner,now,now+3600000))
     db.execute("INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('original_history_delete_01','document_history_delete_01',?,'original','ready','history-delete.png','image/png',?,?,1,?)",(owner,len(image),hashlib.sha256(image).digest(),now))
     db.execute("INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,capture_size,capture_color,capture_dpi) VALUES('page_history_delete_01','document_history_delete_01',?,0,'original_history_delete_01','photo10x15','color',300)",(owner,))
    self.send_response(200);self.end_headers();return
   if self.path=='/__fixture/photo':
    image=(work/'documents/original.png').read_bytes();file=work/'documents/photo.png';file.write_bytes(image);file.chmod(0o600);now=int(time.time()*1000)
    with sqlite3.connect(work/'documents/documents.sqlite3') as db:
     owner=db.execute("SELECT owner_id FROM documents WHERE id='document_browser_01'").fetchone()[0]
     db.execute("INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES('photo_browser_0001',?,'photo','review',?,?)",(owner,now,now+3600000))
     db.execute("INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('photo_original_0001','photo_browser_0001',?,'original','ready','photo.png','image/png',?,?,1,?)",(owner,len(image),hashlib.sha256(image).digest(),now))
     db.execute("INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,capture_size,capture_color,capture_dpi) VALUES('photo_page_0000001','photo_browser_0001',?,0,'photo_original_0001','photo10x15','color',300)",(owner,))
    self.send_response(200);self.end_headers();return
   if self.path in ['/__fixture/latest-render','/__fixture/latest-ocr']:
    with sqlite3.connect(work/'documents/documents.sqlite3') as db:settings=json.loads(db.execute("SELECT configuration_json FROM jobs WHERE kind='render' ORDER BY created_at_ms DESC,id DESC LIMIT 1").fetchone()[0])['settings']
    body=json.dumps(settings if self.path=='/__fixture/latest-ocr' else {'format':settings['format'],'pdf_encoding':settings.get('pdf_encoding')}).encode();self.send_response(200);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(body)));self.end_headers();self.wfile.write(body);return
   if self.path=='/__fixture/run-worker':
    with sqlite3.connect(work/'documents/documents.sqlite3') as db:
     withdrawn=list(db.execute("SELECT a.document_id,a.relative_path FROM artifacts a JOIN documents d ON d.id=a.document_id WHERE d.state IN ('deleting','expired')"))
    worker_env={'PATH':os.environ['PATH'],'SCANNER_STATE_DIR':str(work/'state'),'SCANNER_DOCUMENT_DIR':str(work/'documents')}
    completed=subprocess.run([str(binary),'worker','--once'],env=worker_env,capture_output=True,timeout=30)
    assert completed.returncode==0 and not completed.stdout and not completed.stderr
    with sqlite3.connect(work/'documents/documents.sqlite3') as db:
     for document,relative in withdrawn:
      if db.execute("SELECT count(*) FROM jobs WHERE document_id=? AND kind='cleanup' AND state='succeeded'",(document,)).fetchone()[0]:
       assert not (work/'documents'/relative).exists(),'successful cleanup retained a private file'
       assert db.execute('SELECT count(*) FROM artifacts WHERE document_id=?',(document,)).fetchone()[0]==0
    self.send_response(200);self.end_headers();return
   if self.path=='/__fixture/job':
    now=int(time.time()*1000)
    with sqlite3.connect(work/'documents/documents.sqlite3') as db:
     owner,revision=db.execute("SELECT owner_id,revision FROM documents WHERE id='document_browser_01'").fetchone()
     db.execute("INSERT INTO jobs(id,document_id,owner_id,kind,state,input_revision,configuration_json,created_at_ms,deadline_at_ms) VALUES('job_browser_00001','document_browser_01',?,'render','queued',?,'{}',?,?)",(owner,revision,now,now+60000))
     db.execute("INSERT INTO reservations(job_id,owner_id,byte_size) VALUES('job_browser_00001',?,100)",(owner,))
    self.send_response(200);self.end_headers();return
   if self.path=='/__fixture/job-finish':
    with sqlite3.connect(work/'documents/documents.sqlite3') as db:
     assert db.execute("SELECT state FROM jobs WHERE id='job_browser_00001'").fetchone()[0]=='cancelling'
     assert db.execute("SELECT byte_size FROM reservations WHERE job_id='job_browser_00001'").fetchone()[0]==100
     db.execute("UPDATE jobs SET state='cancelled' WHERE id='job_browser_00001'")
    self.send_response(200);self.end_headers();return
   if self.path=='/__fixture/seed':seed();self.send_response(200);self.end_headers();return
   data=self.rfile.read(int(self.headers.get('Content-Length','0')));connection=http.client.HTTPConnection('127.0.0.1',backend,timeout=10)
   headers={key:value for key,value in self.headers.items() if key.lower() not in ['connection','transfer-encoding']};connection.request(self.command,self.path,body=data,headers=headers);response=connection.getresponse();body=response.read();self.send_response(response.status)
   for key,value in response.getheaders():
    if key.lower() not in ['connection','transfer-encoding','server','date']:self.send_header(key,value)
   try:
    self.end_headers()
    if self.command!='HEAD':self.wfile.write(body)
   except (BrokenPipeError,ConnectionResetError,ssl.SSLEOFError):
    # Closing/navigating the dialog intentionally aborts its capability fetch.
    # Only writes back to that disconnected browser are ignored.
    pass
   finally:connection.close()
  do_GET=do_POST=do_PATCH=do_PUT=do_DELETE=do_HEAD=relay
 proxy=ThreadingHTTPServer(('127.0.0.1',0),Proxy);origin='https://127.0.0.1:'+str(proxy.server_port)
 cert,key=work/'cert.pem',work/'key.pem';subprocess.run(['openssl','req','-x509','-newkey','rsa:2048','-nodes','-keyout',str(key),'-out',str(cert),'-days','1','-subj','/CN=scanner-fixture','-addext','subjectAltName=IP:127.0.0.1'],check=True,capture_output=True);key.chmod(0o600)
 tls=ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER);tls.load_cert_chain(cert,key);proxy.socket=tls.wrap_socket(proxy.socket,server_side=True)
 thread=threading.Thread(target=proxy.serve_forever,daemon=True);thread.start()
 env={'PATH':os.environ['PATH'],'SCANNER_PUBLIC_URL':origin,'SCANNER_LISTEN_ADDR':'127.0.0.1:'+str(backend),'SCANNER_AUTH_MODE':'guest','SCANNER_STATE_DIR':str(work/'state'),'SCANNER_DOCUMENT_DIR':str(work/'documents')}
 process=subprocess.Popen([str(binary),'serve'],env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
 native_process=None;native_proof=None;simple_process=None;simple_browser=None
 try:
  deadline=time.monotonic()+20
  while True:
   if process.poll() is not None:raise RuntimeError('API terminated before browser check')
   try:
    connection=http.client.HTTPConnection('127.0.0.1',backend,timeout=1);connection.request('GET','/health/ready');ready=connection.getresponse().status;connection.close()
    if ready==200:break
   except OSError:pass
   if time.monotonic()>deadline:raise RuntimeError('readiness timed out')
   time.sleep(.05)
  browser_env=dict(os.environ,PLAYWRIGHT_MODULE=args.playwright_module,CHROMIUM_EXECUTABLE=args.chromium,SCANNER_TEST_ORIGIN=origin,SCANNER_TEST_SCREENSHOT='/tmp/scanner-kiosk-product-review.png')
  if args.scenario:
   focused=subprocess.run(['node','--test','--test-reporter=tap','scripts/check-web-ocr-ui.cjs'],cwd=root,env=dict(browser_env,SCANNER_TEST_SCENARIO=args.scenario,SCANNER_TEST_RECEIPT=args.receipt),capture_output=True,text=True,timeout=90)
   print(focused.stdout,end='');print(focused.stderr,end='')
   if focused.returncode:raise SystemExit(focused.returncode)
   evidence=json.loads(Path(args.receipt).read_text())
   assert evidence['checks'] and evidence['requests'] and evidence['scenario']==args.scenario
   assert before==hashes(),'sources changed during verification'
   evidence.update(source_sha256=before,timestamp_utc=datetime.now(timezone.utc).isoformat())
   Path(args.receipt).write_text(json.dumps(evidence,indent=2)+'\n')
   raise SystemExit(0)
  result=subprocess.run(['node','scripts/check-web.cjs'],cwd=root,env=browser_env,capture_output=True,text=True,timeout=90)
  if result.returncode:raise RuntimeError(result.stderr[-12000:])
  browser=json.loads(result.stdout)
  capture_result=subprocess.run(['node','scripts/check-web-capture.cjs'],cwd=root,env=dict(browser_env,SCANNER_TEST_SCREENSHOT='/tmp/scanner-kiosk-capture-mobile.png'),capture_output=True,text=True,timeout=90)
  if capture_result.returncode:raise RuntimeError(capture_result.stderr[-4000:])
  capture_browser=json.loads(capture_result.stdout)
  correction_result=subprocess.run(['node','scripts/check-web-correction.cjs'],cwd=root,env=browser_env,capture_output=True,text=True,timeout=90)
  if correction_result.returncode:raise RuntimeError(correction_result.stderr[-12000:])
  correction_browser=json.loads(correction_result.stdout)
  placement_result=subprocess.run(['node','scripts/check-web-placement.cjs'],cwd=root,env=browser_env,capture_output=True,text=True,timeout=90)
  if placement_result.returncode:raise RuntimeError(placement_result.stderr[-4000:])
  placement_browser=json.loads(placement_result.stdout)
  import_result=subprocess.run(['node','scripts/check-web-import.cjs'],cwd=root,env=browser_env,capture_output=True,text=True,timeout=90)
  if import_result.returncode:raise RuntimeError(import_result.stderr[-4000:])
  import_browser=json.loads(import_result.stdout)
  rate_result=subprocess.run(['node','scripts/check-web-rate.cjs'],cwd=root,env=browser_env,capture_output=True,text=True,timeout=90)
  if rate_result.returncode:raise RuntimeError(rate_result.stderr[-12000:])
  rate_browser=json.loads(rate_result.stdout)
  keyboard_result=subprocess.run(['node','scripts/check-web-keyboard.cjs'],cwd=root,env=browser_env,capture_output=True,text=True,timeout=90)
  if keyboard_result.returncode:raise RuntimeError(keyboard_result.stderr[-12000:])
  keyboard_browser=json.loads(keyboard_result.stdout)
  home_result=subprocess.run(['node','scripts/check-web-home.cjs'],cwd=root,env=browser_env,capture_output=True,text=True,timeout=90)
  if home_result.returncode:raise RuntimeError(home_result.stderr[-12000:])
  home_browser=json.loads(home_result.stdout)
  ocr_ui_result=subprocess.run(['node','scripts/check-web-ocr-ui.cjs'],cwd=root,env=browser_env,capture_output=True,text=True,timeout=90)
  if ocr_ui_result.returncode:raise RuntimeError(ocr_ui_result.stderr[-12000:])
  ocr_ui_browser=json.loads(ocr_ui_result.stdout)
  editor_result=subprocess.run(['node','scripts/check-web-editor-ux.cjs'],cwd=root,env=browser_env,capture_output=True,text=True,timeout=90)
  if editor_result.returncode:raise RuntimeError(editor_result.stderr[-12000:])
  editor_browser=json.loads(editor_result.stdout)
  if all(native_args):
   worker_env={'PATH':os.environ['PATH'],'SCANNER_STATE_DIR':str(work/'state'),'SCANNER_DOCUMENT_DIR':str(work/'documents'),'SCANNER_OCR_PLUGINS':'tesseract','SCANNER_OCR_DEFAULT_PLUGIN':'tesseract','SCANNER_OCR_DEFAULT_LANGUAGES':'ja,en','SCANNER_OCR_TESSERACT_PATH':str(Path(args.tesseract_binary).resolve()),'SCANNER_OCR_TESSERACT_DATA_DIR':str(Path(args.tessdata).resolve())}
   native_process=subprocess.Popen([str(binary),'worker'],env=worker_env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
   deadline=time.monotonic()+20
   while True:
    assert native_process.poll() is None,'native worker stopped during startup'
    with sqlite3.connect(work/'documents/documents.sqlite3') as db:catalog=db.execute("SELECT w.boot_id,s.payload FROM processing_coordinator w JOIN ocr_catalog_snapshot s ON s.boot_id=w.boot_id AND s.generation=w.generation WHERE w.state='active'").fetchone()
    if catalog and any(provider['id']=='tesseract' for provider in json.loads(catalog[1])['Providers']):break
    assert time.monotonic()<deadline,'native worker did not publish catalog'
    time.sleep(.05)
   native_env=dict(browser_env,SCANNER_TEST_DOWNLOAD=str(work/'browser-ocr.pdf'))
   native_result=subprocess.run(['node','scripts/check-web-ocr.cjs'],cwd=root,env=native_env,capture_output=True,text=True,timeout=90)
   if native_result.returncode:raise RuntimeError(native_result.stderr[-4000:])
   native_browser=json.loads(native_result.stdout)
   with sqlite3.connect(work/'documents/documents.sqlite3') as db:
    job_id,state,runner,configuration=db.execute("SELECT id,state,runner_id,configuration_json FROM jobs WHERE document_id='document_native_001'").fetchone()
    assert state=='succeeded' and runner==catalog[0] and job_id==native_browser['job_id']
    assert db.execute('SELECT count(*) FROM reservations WHERE job_id=?',(job_id,)).fetchone()[0]==0
    settings=json.loads(configuration)['settings'];assert settings['ocr']['provider_id']=='tesseract' and settings['ocr']['languages']==['ja','en'] and settings['pdf_encoding']=='lossless'
    assert settings['ocr']['model_version']==native_browser['model_version']
    relative,digest=db.execute("SELECT a.relative_path,a.sha256 FROM published_outputs p JOIN artifacts a ON a.id=p.artifact_id WHERE p.document_id='document_native_001'").fetchone()
   assert hashlib.sha256((work/'browser-ocr.pdf').read_bytes()).digest()==digest==hashlib.sha256((work/'documents'/relative).read_bytes()).digest()
   inspection="""
import json,sys,subprocess
from pathlib import Path
from PIL import Image
import pikepdf
root=Path(sys.argv[1]);checks=[]
with pikepdf.open(root/'browser-ocr.pdf') as pdf:
 assert len(pdf.pages)==2
 for index,page in enumerate(pdf.pages,1):
  assert int(page.obj.get('/Rotate',0))==(index-1)*90
  assert abs(float(page.MediaBox[2])-210/25.4*72)<.02 and abs(float(page.MediaBox[3])-297/25.4*72)<.02
  images=page.get_images();assert len(images)==1
  decoded=pikepdf.PdfImage(next(iter(images.values()))).as_pil_image()
  with Image.open(root/('native-'+str(index)+'.png')) as original:assert decoded.size==original.size and decoded.convert('RGB').tobytes()==original.convert('RGB').tobytes()
  raw=subprocess.check_output(['pdftotext','-f',str(index),'-l',str(index),'-raw',str(root/'browser-ocr.pdf'),'-'],text=True)
  compact=''.join(raw.split());assert '日本語の書類' in compact and 'ScannerOCR'+str(2025+index) in compact
  assert 'ScannerOCR'+str(2028-index) not in compact
checks=['browser-downloaded two-page PDF','per-page Japanese/English text and distinct page order','original lossless pixels, A4 size and 0/90 rotations']
print(json.dumps(checks))
"""
   independent=subprocess.run([args.fixture_python,'-c',inspection,str(work)],check=True,capture_output=True,text=True,timeout=30)
   for index in [1,2]:assert (work/('native-'+str(index)+'.png')).read_bytes()==(work/'documents'/('native-original-'+str(index)+'.png')).read_bytes()
   assert not list((work/'documents/.native').iterdir())
   native_proof={'passed':True,'browser':native_browser,'artifact_checks':json.loads(independent.stdout),'fixture_sha256':{str(index):hashlib.sha256((work/('native-'+str(index)+'.png')).read_bytes()).hexdigest() for index in [1,2]},'tool_sha256':{'binary':hashlib.sha256(Path(args.tesseract_binary).read_bytes()).hexdigest(),'font':hashlib.sha256(Path(args.font).read_bytes()).hexdigest(),**{name:hashlib.sha256((Path(args.tessdata)/(name+'.traineddata')).read_bytes()).hexdigest() for name in ['jpn','eng']}}}


  # A separate real API/state is selected behind the same local TLS proxy.
  # Local users are provisioned through the actual compiled stdin command.
  for name in ['simple-state','simple-documents']:(work/name).mkdir(mode=0o700)
  simple_env={'PATH':os.environ['PATH'],'SCANNER_STATE_DIR':str(work/'simple-state'),'SCANNER_DOCUMENT_DIR':str(work/'simple-documents'),'SCANNER_PUBLIC_URL':origin,'SCANNER_AUTHENTICATE_TYPE':'simple','SCANNER_AUTH_LOGIN_BURST':'20'}
  for username in ['alice','bob']:
   account=subprocess.run([str(binary),'user','create',username],env=simple_env,input=b'Synthetic browser login 2026!\n',capture_output=True,timeout=15)
   assert account.returncode==0 and account.stdout==b'User created.\n' and not account.stderr,'simple fixture user creation failed'
  now=int(time.time()*1000)
  with sqlite3.connect(work/'simple-state/state.sqlite3') as db:owner=db.execute("SELECT principal_id FROM local_credentials WHERE username='alice'").fetchone()[0]
  # Initialize the document schema using the real worker with no admitted work.
  init=subprocess.run([str(binary),'worker','--once'],env=simple_env,capture_output=True,timeout=15)
  assert init.returncode==0 and not init.stdout and not init.stderr
  with sqlite3.connect(work/'simple-documents/documents.sqlite3') as db:
   db.execute('INSERT INTO owners(id) VALUES(?)',(owner,))
   db.execute("INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES('document_simple_001',?,'document','review',?,?)",(owner,now,now+3600000))
   original=(work/'documents/original.png').read_bytes()
   source=work/'simple-documents/original.png';source.write_bytes(original);source.chmod(0o600)
   db.execute("INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('original_simple_001','document_simple_001',?,'original','ready','original.png','image/png',?,?,1,?)",(owner,len(original),hashlib.sha256(original).digest(),now))
   db.execute("INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,capture_size,capture_color,capture_dpi,pixel_width,pixel_height) VALUES('page_simple_00001','document_simple_001',?,0,'original_simple_001','a4','color',300,32,16)",(owner,))
  with socket.socket() as sock:sock.bind(('127.0.0.1',0));backend=sock.getsockname()[1]
  simple_env['SCANNER_LISTEN_ADDR']='127.0.0.1:'+str(backend)
  simple_process=subprocess.Popen([str(binary),'serve'],env=simple_env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
  deadline=time.monotonic()+20
  while True:
   assert simple_process.poll() is None,'simple API stopped before browser check'
   try:
    connection=http.client.HTTPConnection('127.0.0.1',backend,timeout=1);connection.request('GET','/health/ready');ready=connection.getresponse().status;connection.close()
    if ready==200:break
   except OSError:pass
   assert time.monotonic()<deadline,'simple API readiness timed out'
   time.sleep(.05)
  simple_result=subprocess.run(['node','scripts/check-web-simple.cjs'],cwd=root,env=dict(browser_env,SCANNER_TEST_SCREENSHOT='/tmp/scanner-kiosk-simple-login.png'),capture_output=True,text=True,timeout=90)
  if simple_result.returncode:raise RuntimeError(simple_result.stderr[-4000:])
  simple_browser=json.loads(simple_result.stdout)

  simple_process.terminate();simple_out,simple_error=simple_process.communicate(timeout=15)
  assert simple_process.returncode==0 and not simple_out and not simple_error
  simple_process=None
  for name in ['none-state','none-documents']:(work/name).mkdir(mode=0o700)
  none_env={'PATH':os.environ['PATH'],'SCANNER_STATE_DIR':str(work/'none-state'),'SCANNER_DOCUMENT_DIR':str(work/'none-documents'),'SCANNER_PUBLIC_URL':origin,'SCANNER_AUTHENTICATE_TYPE':'none','SCANNER_OCR_PLUGINS':'','SCANNER_OCR_DEFAULT_PLUGIN':'','SCANNER_LISTEN_ADDR':'127.0.0.1:'+str(backend)}
  simple_process=subprocess.Popen([str(binary),'serve'],env=none_env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
  none_worker=subprocess.Popen([str(binary),'worker'],env=none_env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
  try:
   deadline=time.monotonic()+20
   while True:
    assert simple_process.poll() is None and none_worker.poll() is None,'none runtime stopped before browser check'
    try:
     connection=http.client.HTTPConnection('127.0.0.1',backend,timeout=1);connection.request('GET','/health/ready');ready=connection.getresponse().status;connection.close()
     if ready==200:break
    except OSError:pass
    assert time.monotonic()<deadline,'none API readiness timed out'
    time.sleep(.05)
   none_result=subprocess.run(['node','scripts/check-web-none.cjs'],cwd=root,env=browser_env,capture_output=True,text=True,timeout=90)
   if none_result.returncode:raise RuntimeError(none_result.stderr[-4000:])
   none_browser=json.loads(none_result.stdout)
  finally:
   none_worker.terminate();none_out,none_error=none_worker.communicate(timeout=30)
   assert none_worker.returncode==0 and not none_out and not none_error,'none worker shutdown failed'

 finally:
  try:
   if native_process is not None:
    if native_process.poll() is None:native_process.terminate()
    native_out,native_error=native_process.communicate(timeout=30)
    assert native_process.returncode==0 and not native_out and not native_error,'native worker shutdown failed'
    with sqlite3.connect(work/'documents/documents.sqlite3') as db:
     assert db.execute('SELECT state FROM processing_coordinator').fetchone()[0]=='stopped'
     assert db.execute('SELECT count(*) FROM ocr_catalog_snapshot').fetchone()[0]==0
  finally:
   if simple_process is not None:
    simple_process.terminate();simple_out,simple_error=simple_process.communicate(timeout=15)
    assert simple_process.returncode==0 and not simple_out and not simple_error,'simple API shutdown failed'
   process.terminate();out,error=process.communicate(timeout=15);proxy.shutdown();proxy.server_close();thread.join(timeout=5)
 assert process.returncode==0 and not out and not error
 assert before==hashes(),'sources changed during verification'
 built={str(f.relative_to(root)):hashlib.sha256(f.read_bytes()).hexdigest() for f in sorted((root/'internal/webui/dist').rglob('*')) if f.is_file()}
 receipt={'passed':True,'timestamp_utc':datetime.now(timezone.utc).isoformat(),'source_sha256':before,'built_assets_sha256':built,'browser':browser,'native_ocr':native_proof,'simple_auth':simple_browser,'none_auth':none_browser,'capture_ui':capture_browser,'correction_ui':correction_browser,'placement_ui':placement_browser,'import_ui':import_browser,'rate_limit_ui':rate_browser,'keyboard_ui':keyboard_browser,'home_ui':home_browser,'editor_ux':editor_browser,'ocr_ui':ocr_ui_browser,'scope':'Embedded Go binary, real guest/document APIs and private SQLite, headless Chromium through local fixture TLS proxy. Browser ignores only the fixture certificate error. Synthetic image/document/job rows and injected terminal job transition; real plain-PDF/PNG worker invocations and OCR selection/admission with fixture metadata; capture_ui checks scanner UI contracts, correction_ui checks optional correction compare/apply/undo/reprocess with mocked APIs and no correction engine, placement/import/rate/keyboard/home/editor/ocr_ui use their scoped mocked or real API evidence. No scanner hardware, real OIDC browser login, production proxy/mobile/screen-reader acceptance, complete scan workflow or deployment.'}
 Path(args.receipt).write_text(json.dumps(receipt,indent=2)+'\n');print(json.dumps({'passed':True,'checks':len(browser['checks']),'native_ocr':native_proof is not None,'simple_auth_checks':len(simple_browser['checks']),'none_auth_checks':len(none_browser['checks']),'capture_ui_checks':len(capture_browser['checks']),'correction_ui_checks':len(correction_browser['checks']),'placement_ui_checks':len(placement_browser['checks']),'import_ui_checks':len(import_browser['checks']),'rate_limit_ui_checks':len(rate_browser['checks']),'keyboard_ui_checks':len(keyboard_browser['checks']),'home_ui_checks':len(home_browser['checks']),'ocr_ui_checks':len(ocr_ui_browser['checks'])}))
