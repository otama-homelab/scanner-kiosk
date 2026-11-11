#!/usr/bin/env python3
"""Verify the compiled serve command on isolated loopback backend listeners."""
import argparse
from datetime import datetime,timezone
import hashlib,http.client,json,os,signal,socket,sqlite3,ssl,struct,subprocess,tempfile,threading,time,zlib
from http.server import BaseHTTPRequestHandler,ThreadingHTTPServer
from pathlib import Path

parser=argparse.ArgumentParser();parser.add_argument('--receipt',required=True);args=parser.parse_args()
root=Path(__file__).resolve().parents[1]
files=sorted([*root.glob('cmd/**/*.go'),*root.glob('internal/**/*.go'),*root.glob('internal/**/*.sql'),*root.glob('pkg/**/*.go'),root/'go.mod',root/'go.sum',root/'api/openapi.json',Path(__file__).resolve()])
def blank_pdf():
 newline=bytes([10]);pdf=b'%PDF-1.4'+newline;offsets=[0]
 objects=[b'<< /Type /Catalog /Pages 2 0 R >>',b'<< /Type /Pages /Count 1 /Kids [3 0 R] >>',b'<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << >> /Contents 4 0 R >>',b'<< /Length 0 >>'+newline+b'stream'+newline+b'endstream']
 for index,obj in enumerate(objects,1):
  offsets.append(len(pdf));pdf+=str(index).encode()+b' 0 obj'+newline+obj+newline+b'endobj'+newline
 xref=len(pdf);pdf+=b'xref'+newline+b'0 5'+newline+b'0000000000 65535 f '+newline
 for offset in offsets[1:]:pdf+=f'{offset:010d} 00000 n '.encode()+newline
 return pdf+b'trailer'+newline+b'<< /Size 5 /Root 1 0 R >>'+newline+b'startxref'+newline+str(xref).encode()+newline+b'%%EOF'+newline

def hashes():return {str(p.relative_to(root)):hashlib.sha256(p.read_bytes()).hexdigest() for p in files}
before=hashes();checks=[]
with tempfile.TemporaryDirectory(prefix='scanner-api-cli-') as directory:
 work=Path(directory);binary=work/'scanner'
 subprocess.run(['go','build','-o',str(binary),'./cmd/scanner'],cwd=root,env=dict(os.environ,GOTOOLCHAIN='go1.27.4'),check=True,capture_output=True)
 def port():
  with socket.socket() as s:s.bind(('127.0.0.1',0));return s.getsockname()[1]
 def environment(name):
  state,docs=work/(name+'-state'),work/(name+'-documents');state.mkdir(mode=0o700);docs.mkdir(mode=0o700)
  return {'PATH':os.environ['PATH'],'SCANNER_STATE_DIR':str(state),'SCANNER_DOCUMENT_DIR':str(docs),'SCANNER_PUBLIC_URL':'https://scanner.example.test','SCANNER_LISTEN_ADDR':'127.0.0.1:'+str(port()),'SCANNER_AUTH_MODE':'guest'}
 def request(env,method,path,cookie=None,origin=None,key=None,body=None,revision=None,range_header=None):
  c=http.client.HTTPConnection('127.0.0.1',int(env['SCANNER_LISTEN_ADDR'].split(':')[-1]),timeout=3)
  headers={'Host':'scanner.example.test'}
  if cookie:headers['Cookie']=cookie
  if origin:headers['Origin']=origin
  if key:headers['Idempotency-Key']=key
  if revision:headers['If-Match']=revision
  if range_header:headers['Range']=range_header
  data=None
  if body is not None:headers['Content-Type']='application/json';data=json.dumps(body).encode()
  c.request(method,path,body=data,headers=headers);r=c.getresponse();data=r.read();media=r.getheader('Content-Type','');value=data if media.startswith('image/') or media=='application/pdf' else json.loads(data) if data else None;result=(r.status,dict(r.getheaders()),value);c.close();return result
 def start(env):
  process=subprocess.Popen([str(binary),'serve'],env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
  deadline=time.monotonic()+20
  while time.monotonic()<deadline:
   if process.poll() is not None:
    _,error=process.communicate();raise RuntimeError('serve exited before readiness: '+error.decode()[:200])
   try:
    if request(env,'GET','/health/ready')[0]==200:return process
   except (OSError,TimeoutError):pass
   time.sleep(.05)
  process.terminate();process.communicate(timeout=15);raise RuntimeError('serve did not become ready')
 def stop(process):
  if process.poll() is None:process.send_signal(signal.SIGTERM)
  out,error=process.communicate(timeout=15);assert process.returncode==0 and out==b'' and error==b''
 def cookie(headers):
  value=headers['Set-Cookie'];assert '__Host-scanner=' in value and 'Secure' in value and 'HttpOnly' in value and 'SameSite=Lax' in value and 'Domain=' not in value
  return value.split(';')[0]
 env=environment('guest');process=start(env)
 try:
  status,headers,view=request(env,'GET','/api/session');assert status==200 and not view['authenticated'] and view['authenticate_type']=='guest';anonymous=cookie(headers)
  status,_,_=request(env,'POST','/auth/guest',anonymous,'https://other.example.test','request_guest_0001');assert status==403
  status,headers,view=request(env,'POST','/auth/guest',anonymous,env['SCANNER_PUBLIC_URL'],'request_guest_0001');assert status==200 and view['authenticated'];guest=cookie(headers);assert guest!=anonymous
  checks.append('compiled guest serve: health, bootstrap, Origin rejection, private Cookie and independent principal')
 finally:stop(process)
 process=start(env)
 try:
  assert request(env,'GET','/api/session',guest)[2]['authenticated']
  now=int(time.time()*1000)
  def chunk(kind,data):return struct.pack('>I',len(data))+kind+data+struct.pack('>I',zlib.crc32(kind+data))
  original=bytes.fromhex('89504e470d0a1a0a')+chunk(b'IHDR',struct.pack('>IIBBBBB',32,16,8,2,0,0,0))+chunk(b'IDAT',zlib.compress((bytes([0])+bytes([80,100,120])*32)*16))+chunk(b'IEND',b'')
  original_path=Path(env['SCANNER_DOCUMENT_DIR'])/'private-original.png';original_path.write_bytes(original);original_path.chmod(0o600)
  pdf=blank_pdf();pdf_path=Path(env['SCANNER_DOCUMENT_DIR'])/'private-output.pdf';pdf_path.write_bytes(pdf);pdf_path.chmod(0o600)
  with sqlite3.connect(Path(env['SCANNER_STATE_DIR'])/'state.sqlite3') as db:owner=db.execute("SELECT id FROM principals WHERE kind='guest'").fetchone()[0]
  with sqlite3.connect(Path(env['SCANNER_DOCUMENT_DIR'])/'documents.sqlite3') as db:
   db.execute('INSERT INTO owners(id) VALUES(?)',(owner,))
   for identifier in ['document_cli_00001','document_cli_00002']:
    db.execute("INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES(?,?,'document','review',?,?)",(identifier,owner,now,now+3600000))
   db.execute("INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('original_cli_0001','document_cli_00001',?,'original','ready','private-original.png','image/png',?,?,1,?)",(owner,len(original),hashlib.sha256(original).digest(),now))
   db.execute("INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,capture_size,capture_color,capture_dpi) VALUES('page_cli_00000001','document_cli_00001',?,0,'original_cli_0001','a4','gray',300)",(owner,))
   db.execute("INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('output_cli_000001','document_cli_00001',?,'output','ready','private-output.pdf','application/pdf',?,?,1,?)",(owner,len(pdf),hashlib.sha256(pdf).digest(),now))
   db.execute("INSERT INTO published_outputs(document_id,owner_id,revision,artifact_id) VALUES('document_cli_00001',?,1,'output_cli_000001')",(owner,))
   db.execute("INSERT INTO jobs(id,document_id,owner_id,kind,state,input_revision,configuration_json,created_at_ms,deadline_at_ms) VALUES('job_cli_00000001','document_cli_00002',?,'render','queued',1,'{}',?,?)",(owner,now,now+60000))
  status,_,history=request(env,'GET','/api/documents?limit=1',guest);assert status==200 and len(history['items'])==1 and history['next_cursor']
  status,_,second=request(env,'GET','/api/documents?limit=1&cursor='+history['next_cursor'],guest);assert status==200 and second['next_cursor'] is None and second['items'][0]['id']=='document_cli_00002'
  status,headers,document=request(env,'GET','/api/documents/document_cli_00001',guest);assert status==200 and headers['Etag']=='"1"' and document['pages'][0]['rotation']==0 and document['output']['format']=='pdf' and 'owner_id' not in document
  assert request(env,'GET','/api/documents')[0]==401
  checks.append('compiled authorized history/detail: persistent DB projections, cursor pagination, revision ETag and anonymous denial')
  page='/api/documents/document_cli_00001/pages/page_cli_00000001'
  status,headers,preview=request(env,'GET',page+'/preview',guest);assert status==200 and preview==original and headers['Content-Type']=='image/png' and headers['Cache-Control']=='no-store', (status,headers.get('Content-Type'),type(preview).__name__,len(preview),preview if isinstance(preview,dict) else 'binary')
  assert request(env,'GET',page+'/preview')[0]==401
  _,headers,_=request(env,'GET','/api/session');other_anon=cookie(headers)
  _,headers,_=request(env,'POST','/auth/guest',other_anon,env['SCANNER_PUBLIC_URL'],'request_other_001');other=cookie(headers)
  assert request(env,'GET',page+'/preview',other)[0]==404
  checks.append('compiled source-image preview: verified real PNG bytes, anonymous and cross-owner denial')
  download='/api/documents/document_cli_00001/download'
  status,headers,downloaded=request(env,'GET',download,guest);assert status==200 and downloaded==pdf and headers['Content-Disposition']=='attachment; filename="scan.pdf"'
  status,headers,part=request(env,'GET',download,guest,range_header='bytes=0-3');assert status==206 and part==pdf[:4] and headers['Content-Range']==f'bytes 0-3/{len(pdf)}'
  assert request(env,'GET',download,guest,range_header='bytes=999999-')[0]==416
  assert request(env,'GET',download,other,range_header='bytes=0-3')[0]==404
  checks.append('compiled PDF download: verified full bytes, authorized single range, 416 and cross-owner denial')
  job_url='/api/jobs/job_cli_00000001'
  status,_,job=request(env,'GET',job_url,guest);assert status==200 and job['state']=='queued' and set(job)=={'id','document_id','kind','state','input_revision','error_code'}
  assert request(env,'GET',job_url,other)[0]==404
  status,headers,job=request(env,'POST',job_url+'/cancel',guest,env['SCANNER_PUBLIC_URL'],'request_cancel_01');assert status==202 and job['state']=='cancelling' and headers['Location']==job_url
  assert request(env,'POST',job_url+'/cancel',guest,env['SCANNER_PUBLIC_URL'],'request_cancel_01')[0]==202
  checks.append('compiled owned job status and durable cancel intent/replay; cancellation does not assert worker stop')
  rotated=request(env,'PATCH',page,guest,env['SCANNER_PUBLIC_URL'],'request_rotate_001',{'rotation':90},'"1"');assert rotated[0]==200 and rotated[1]['Etag']=='"2"' and rotated[2]['pages'][0]['rotation']==90
  assert request(env,'GET',download,guest,range_header='bytes=0-3')[0]==404
  assert request(env,'PATCH',page,guest,env['SCANNER_PUBLIC_URL'],'request_rotate_001',{'rotation':90},'"1"')[2]==rotated[2]
  assert request(env,'PATCH',page,guest,env['SCANNER_PUBLIC_URL'],'request_rotate_002',{'rotation':180},'"1"')[0]==412
  assert request(env,'PUT','/api/documents/document_cli_00001/pages/order',guest,env['SCANNER_PUBLIC_URL'],'request_reorder_01',{'page_ids':['page_cli_00000001']},'"2"')[1]['Etag']=='"3"'
  deleted=request(env,'DELETE',page,guest,env['SCANNER_PUBLIC_URL'],'request_delete_01',revision='"3"');assert deleted[0]==200 and deleted[2]['page_count']==0 and deleted[2]['revision']==4 and deleted[2]['expires_at']==document['expires_at']
  assert request(env,'GET',page+'/preview',guest)[0]==404
  checks.append('compiled page edit HTTP: rotation, exact replay, stale revision rejection, reorder and deletion with immutable TTL')
  status,headers,_=request(env,'POST','/auth/guest/end',guest,env['SCANNER_PUBLIC_URL'],'request_end_000001');assert status==202 and 'Max-Age=0' in headers['Set-Cookie']
  assert not request(env,'GET','/api/session',guest)[2]['authenticated']
  with sqlite3.connect(Path(env['SCANNER_STATE_DIR'])/'state.sqlite3') as db:
   assert db.execute('SELECT count(*) FROM principals WHERE disabled_at_ms IS NOT NULL').fetchone()[0]==1
   assert db.execute('SELECT count(*) FROM principal_cleanup_requests').fetchone()[0]==1
  checks.append('normal process restart retains sessions; guest end disables access and persists cleanup intent')
 finally:stop(process)
 cert,key=work/'idp.crt',work/'idp.key'
 subprocess.run(['openssl','req','-x509','-newkey','rsa:2048','-nodes','-keyout',str(key),'-out',str(cert),'-days','1','-subj','/CN=scanner-fixture','-addext','subjectAltName=IP:127.0.0.1'],check=True,capture_output=True);key.chmod(0o600)
 class IdP(BaseHTTPRequestHandler):
  def log_message(self,*args):pass
  def do_GET(self):
   if self.path!='/.well-known/openid-configuration':self.send_error(404);return
   body=json.dumps({'issuer':issuer,'authorization_endpoint':issuer+'/authorize','token_endpoint':issuer+'/token','jwks_uri':issuer+'/jwks','id_token_signing_alg_values_supported':['RS256']}).encode()
   self.send_response(200);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(body)));self.end_headers();self.wfile.write(body)
 server=ThreadingHTTPServer(('127.0.0.1',0),IdP);context=ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER);context.load_cert_chain(cert,key);server.socket=context.wrap_socket(server.socket,server_side=True);issuer='https://127.0.0.1:'+str(server.server_port)
 thread=threading.Thread(target=server.serve_forever,daemon=True);thread.start()
 env=environment('oidc');env.update(SCANNER_AUTH_MODE='oidc',SCANNER_OIDC_ISSUER=issuer,SCANNER_OIDC_CLIENT_ID='fixture-cli',SCANNER_OIDC_CLIENT_AUTH='none',SCANNER_OIDC_SCOPES='openid,profile',SSL_CERT_FILE=str(cert),SSL_CERT_DIR=str(work/'empty-certs'));(work/'empty-certs').mkdir()
 try:
  process=start(env)
  try:
   status,headers,view=request(env,'GET','/api/session');assert status==200 and not view['authenticated'] and view['authenticate_type']=='oidc';browser=cookie(headers)
   assert request(env,'POST','/auth/guest',browser,env['SCANNER_PUBLIC_URL'],'request_guest_0001')[0]==403
   status,_,target=request(env,'POST','/auth/oidc/start',browser,env['SCANNER_PUBLIC_URL'],'request_oidc_00001');assert status==200 and target['authorization_url'].startswith(issuer+'/authorize?')
   assert request(env,'POST','/auth/oidc/start',browser,env['SCANNER_PUBLIC_URL'],'request_oidc_00001')[2]==target
   with sqlite3.connect(Path(env['SCANNER_STATE_DIR'])/'state.sqlite3') as db:db.executescript('DELETE FROM sessions; DELETE FROM session_revocations;')
   status,headers,view=request(env,'GET','/api/session',browser);assert status==200 and not view['authenticated'] and 'Set-Cookie' not in headers
   status,headers,_=request(env,'POST','/auth/logout',browser,env['SCANNER_PUBLIC_URL'],'request_reset_0001');assert status==204 and 'Max-Age=0' in headers['Set-Cookie']
   checks.append('compiled OIDC env composition: trusted TLS discovery, fixed redirect/client/scopes, idempotent JSON start and no guest fallback')
   checks.append('explicit same-origin logout clears unknown Cookie after state recovery without granting identity')
  finally:stop(process)
 finally:server.shutdown();server.server_close();thread.join(timeout=5)
 bad=environment('bad-oidc');bad.update(SCANNER_AUTH_MODE='oidc',SCANNER_OIDC_ISSUER='https://127.0.0.1:1',SCANNER_OIDC_CLIENT_ID='fixture-cli',SCANNER_OIDC_CLIENT_AUTH='none')
 result=subprocess.run([str(binary),'serve'],env=bad,capture_output=True,timeout=20);assert result.returncode!=0 and b'configured OIDC provider unavailable' in result.stderr
 checks.append('unavailable OIDC exits before serving; no implicit guest runtime')
 scan_env=environment('scan-admission')
 scan_env['SCANNER_SCANNERS_JSON']=json.dumps([{'id':'scanner_cli_000001','name':'Registered scanner','driver':'sane-airscan','endpoint':'http://192.0.2.10/eSCL'}])
 process=start(scan_env)
 try:
  _,headers,_=request(scan_env,'GET','/api/session');scan_anon=cookie(headers)
  command={'scanner_id':'scanner_cli_000001','purpose':'document','capture':{'size':'a4','color':'color','dpi':300}}
  assert request(scan_env,'GET','/api/scanners')[0]==401
  assert request(scan_env,'POST','/api/documents',scan_anon,scan_env['SCANNER_PUBLIC_URL'],'request_scan_cli_01',command)[0]==401
  _,headers,_=request(scan_env,'POST','/auth/guest',scan_anon,scan_env['SCANNER_PUBLIC_URL'],'request_scan_login');scan_user=cookie(headers)
  assert request(scan_env,'GET','/api/capabilities',scan_user)[0]==503
  status,_,scanners=request(scan_env,'GET','/api/scanners',scan_user)
  assert status==200 and scanners['scanners'][0]['state']=='unknown' and scanners['scanners'][0]['capture_options']==[] and 'ocr_plugins' not in scanners
  checks.append('compiled scanner list is authenticated and independent of missing OCR/readiness; no selectable or ready device is fabricated')
  assert request(scan_env,'POST','/api/documents',scan_user,'https://foreign.example.test','request_scan_cli_01',command)[0]==403
  status,_,error=request(scan_env,'POST','/api/documents',scan_user,scan_env['SCANNER_PUBLIC_URL'],'request_scan_cli_01',command)
  assert status==503 and error['code']=='scanner_unavailable'
  assert request(scan_env,'POST','/api/documents/document_unready_001/scans',scan_user,scan_env['SCANNER_PUBLIC_URL'],'request_next_cli_01',{'scanner_id':command['scanner_id'],'capture':command['capture']},'"1"')[0]==503
  assert request(scan_env,'POST','/api/documents',scan_user,scan_env['SCANNER_PUBLIC_URL'],'request_scan_cli_01',dict(command,worker_boot_id='browser_worker_001'))[0]==400
  with sqlite3.connect(Path(scan_env['SCANNER_DOCUMENT_DIR'])/'documents.sqlite3') as db:
   assert all(db.execute('SELECT count(*) FROM '+table).fetchone()[0]==0 for table in ['documents','jobs','reservations','request_receipts','device_leases'])
  checks.append('compiled scan HTTP rejects anonymous, foreign Origin and browser worker policy; missing readiness returns 503 without creating documents/jobs/leases')
 finally:stop(process)
 assert before==hashes()
receipt={'timestamp_utc':datetime.now(timezone.utc).isoformat(),'passed':True,'checks':checks,'source_sha256':before,'scope':'Compiled serve command with isolated private stores and loopback HTTP backend. Guest lifecycle/restart, document metadata/page edits, real PNG preview and PDF/Range download from synthetic DB records and OIDC TLS discovery/start are real. Secure Cookie is manually relayed to model a trusted TLS proxy. Scan admission is also exercised without readiness and must leave all document/job/lease metadata empty. No product frontend, successful compiled document creation, native scanning/thumbnails, compiled OIDC token callback, live IdP/proxy/browser acceptance or deployment.'}
Path(args.receipt).write_text(json.dumps(receipt,indent=2)+'\n');print(json.dumps({'passed':True,'checks':len(checks)}))
