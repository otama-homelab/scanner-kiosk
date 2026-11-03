#!/usr/bin/env python3
"""Exercise compiled identity recovery with real local HTTP login and SQLite.

Only synthetic private stores are used. No physical device, IdP or deployment.
The local HTTP fixture manually transports the otherwise Secure session cookie.
"""
import argparse
from contextlib import closing
from datetime import datetime, timezone
import hashlib
import http.client
from http.cookies import SimpleCookie
import json
import os
from pathlib import Path
import socket
import sqlite3
import struct
import subprocess
import tempfile
import time
import zlib

p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--receipt',type=Path,required=True)
args = p.parse_args()
root = Path(__file__).resolve().parents[1]
sources = [root/'go.mod',root/'go.sum',Path(__file__).resolve(),*root.glob('cmd/**/*.go'),*root.glob('internal/**/*.go'),*root.glob('internal/**/*.sql'),*root.glob('pkg/**/*.go')]
sha = lambda path: hashlib.sha256(path.read_bytes()).hexdigest()
before = {str(path.relative_to(root)):sha(path) for path in sorted(sources)}
receipt = {'passed':False,'started_at':datetime.now(timezone.utc).isoformat(),'source_sha256':before,'checks':[],'scope':'isolated compiled identity recovery, preserved-document owner isolation and empty-document-root restart via real simple HTTP/SQLite; no old scanner/production stores, real IdP, physical readiness, actual volume destruction or deployment acceptance'}
children = []
password = 'Synthetic recovery password 2026!'
origin = 'https://scanner.recovery.test'
def check(label):
    receipt['checks'].append(label)
def private(path):
    path.mkdir(mode=0o700)
    return path
def stop(process):
    if process.poll() is None:
        process.terminate()
        try:process.wait(timeout=15)
        except subprocess.TimeoutExpired:
            process.kill();process.wait(timeout=10)
    assert process.returncode==0, 'owned API did not stop cleanly'
try:
    with tempfile.TemporaryDirectory(prefix='scanner-state-recovery-') as directory:
        work = Path(directory)
        binary = work/'scanner'
        subprocess.run(['go','build','-o',str(binary),'./cmd/scanner'],cwd=root,check=True,capture_output=True,timeout=180)
        receipt['binary_sha256'] = sha(binary)
        source, docs, target, new_docs, backups = [private(work/name) for name in ['state','documents','restored','new-documents','backups']]
        def cli(state,*command,password_input=None,ok=True):
            result = subprocess.run([str(binary),*command],env={'PATH':os.environ['PATH'],'SCANNER_STATE_DIR':str(state)},input=password_input,capture_output=True,timeout=60)
            if ok:assert result.returncode==0, result.stderr.decode()
            assert password.encode() not in result.stdout+result.stderr and b'$argon2id$' not in result.stdout+result.stderr
            return result
        cli(source,'user','create','Alice',password_input=(password+'\n').encode())
        cli(source,'user','create','Bob',password_input=(password+'\n').encode())
        def start(state,documents):
            with socket.socket() as sock:
                sock.bind(('127.0.0.1',0));port=sock.getsockname()[1]
            env={'PATH':os.environ['PATH'],'SCANNER_STATE_DIR':str(state),'SCANNER_DOCUMENT_DIR':str(documents),'SCANNER_PUBLIC_URL':origin,'SCANNER_LISTEN_ADDR':'127.0.0.1:'+str(port),'SCANNER_AUTHENTICATE_TYPE':'simple','SCANNER_SCANNERS_JSON':json.dumps([{'id':'scanner_fixture_001','name':'Not accessed','driver':'sane-airscan','endpoint':'http://192.0.2.10/eSCL'}])}
            process=subprocess.Popen([str(binary),'serve'],env=env,stdout=subprocess.DEVNULL,stderr=subprocess.PIPE);children.append(process)
            deadline=time.monotonic()+20
            while True:
                assert process.poll() is None,'owned API terminated before readiness'
                try:
                    conn=http.client.HTTPConnection('127.0.0.1',port,timeout=1);conn.request('GET','/health/ready',headers={'Host':'scanner.recovery.test'});response=conn.getresponse();response.read();conn.close()
                    if response.status==200:break
                except OSError:pass
                assert time.monotonic()<deadline,'API readiness timeout'
                time.sleep(.05)
            return process,port
        def request(port,method,path,cookie=None,body=None,raw=False):
            # Pace expanded recovery scenarios below the default API request budget.
            time.sleep(.11)
            headers={'Host':'scanner.recovery.test','Origin':origin}
            if cookie:headers['Cookie']=cookie
            if body is not None:headers.update({'Content-Type':'application/json','Idempotency-Key':'request_recovery_simple_login_01'})
            conn=http.client.HTTPConnection('127.0.0.1',port,timeout=10)
            conn.request(method,path,body=json.dumps(body) if body is not None else None,headers=headers)
            response=conn.getresponse();data=response.read();cookies=SimpleCookie()
            for name,value in response.getheaders():
                if name.lower()=='set-cookie':cookies.load(value)
            selected=cookies.get('__Host-scanner_session')
            if selected is None and cookies:
                assert len(cookies)==1
                selected=next(iter(cookies.values()))
            next_cookie=None
            if selected:
                assert selected.key.startswith('__Host-') and selected['secure'] and selected['httponly'] and selected['path']=='/' and not selected['domain']
                next_cookie=selected.key+'='+selected.value
            result=(response.status,data if raw else json.loads(data),next_cookie);conn.close();return result
        def login(port,username='ALICE'):
            status,view,anonymous=request(port,'GET','/api/session');assert status==200 and not view['authenticated'] and anonymous
            status,view,cookie=request(port,'POST','/auth/simple/login',anonymous,{'username':username,'password':password})
            assert status==200 and view['authenticated'] and view['principal_kind']=='simple' and not view['can_administer'] and cookie!=anonymous
            return cookie
        api,port=start(source,docs)
        old_cookie=login(port)
        with closing(sqlite3.connect(source/'state.sqlite3')) as db, db:
            original_users=dict(db.execute('SELECT username,principal_id FROM local_credentials'))
            assert set(original_users)=={'alice','bob'}
            principal=original_users['alice']
            db.execute("INSERT INTO principals VALUES('principal_recovery_oidc','oidc',1000,NULL)")
            db.execute("INSERT INTO oidc_identities VALUES('https://issuer.example.test','synthetic-subject','principal_recovery_oidc')")
            db.execute("INSERT INTO auth_attempts VALUES(?,?,?,?,1000,2000,NULL)",(bytes([1])*32,bytes([2])*32,'NONCE_RECOVERY_CANARY_001','VERIFIER_RECOVERY_CANARY_'*3))
        canary=docs/'document-canary.txt';canary.write_bytes(b'DOCUMENT_CONTENT_RECOVERY_CANARY');canary.chmod(0o600);original_hash=sha(canary)
        def chunk(kind,data):return struct.pack('>I',len(data))+kind+data+struct.pack('>I',zlib.crc32(kind+data))
        image=bytes.fromhex('89504e470d0a1a0a')+chunk(b'IHDR',struct.pack('>IIBBBBB',32,16,8,2,0,0,0))+chunk(b'IDAT',zlib.compress((bytes([0])+bytes([100,120,140])*32)*16))+chunk(b'IEND',b'')
        owned=docs/'owned.png';owned.write_bytes(image);owned.chmod(0o600);now=int(time.time()*1000)
        document_id='document_recovery_owned_01';page_id='page_recovery_owned_01'
        detail='/api/documents/'+document_id;preview=detail+'/pages/'+page_id+'/preview'
        with closing(sqlite3.connect(docs/'documents.sqlite3')) as db,db:
            db.execute('PRAGMA foreign_keys=ON')
            db.execute('INSERT INTO owners(id) VALUES(?)',(principal,))
            db.execute("INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES(?,?,'document','review',?,?)",(document_id,principal,now,now+3600000))
            db.execute("INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('original_recovery_owned_01',?,?,'original','ready','owned.png','image/png',?,?,1,?)",(document_id,principal,len(image),hashlib.sha256(image).digest(),now))
            db.execute("INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,capture_size,capture_color,capture_dpi,pixel_width,pixel_height) VALUES(?,?,?,0,'original_recovery_owned_01','a4','color',300,32,16)",(page_id,document_id,principal))
        status,original_view,_=request(port,'GET',detail,old_cookie);assert status==200 and original_view['page_count']==1
        assert request(port,'GET',preview,old_cookie,raw=True)[:2]==(200,image)
        archive=backups/'identities.json';cli(source,'state','export',str(archive))
        data=archive.read_bytes();snapshot=json.loads(data)
        assert archive.stat().st_mode & 0o777==0o600
        for secret in ['NONCE_RECOVERY_CANARY','VERIFIER_RECOVERY_CANARY','DOCUMENT_CONTENT_RECOVERY_CANARY',old_cookie.split('=',1)[1]]:
            assert secret.encode() not in data
        assert set(snapshot)=={'format','version','state_schema','principals','oidc_identities','local_credentials'}
        assert len(snapshot['principals'])==3 and len(snapshot['local_credentials'])==2
        assert request(port,'GET','/api/session',old_cookie)[1]['authenticated'] and sha(canary)==original_hash
        check('compiled export snapshots live API identity state into a private logical archive without revoking the source login or reading/copying document/protocol payloads')
        result=cli(source,'state','export',str(archive),ok=False);assert result.returncode!=0 and archive.read_bytes()==data
        check('compiled export refuses existing destination and retains its bytes')
        stop(api)
        cli(target,'state','restore',str(archive))
        with closing(sqlite3.connect(target/'state.sqlite3')) as db, db:
            for table in ['sessions','auth_attempts','session_revocations','guest_starts','oidc_start_requests','session_login_grants','simple_session_credentials','principal_cleanup_requests']:
                assert db.execute('SELECT count(*) FROM '+table).fetchone()[0]==0
            assert db.execute("SELECT principal_id FROM local_credentials WHERE username='alice'").fetchone()[0]==principal
            assert dict(db.execute('SELECT username,principal_id FROM local_credentials'))==original_users
            assert db.execute('PRAGMA integrity_check').fetchone()[0]=='ok'
        assert sorted(path.name for path in target.iterdir())==['state.sqlite3'] and not list(new_docs.iterdir())
        check('compiled restore publishes a complete checked state DB with stable local principal, no session/protocol/outbox rows, no staging residue or document restoration')
        new_api,new_port=start(target,docs)
        status,view,_=request(new_port,'GET','/api/session',old_cookie);assert status==200 and not view['authenticated']
        fresh=login(new_port);assert fresh!=old_cookie
        assert request(new_port,'GET','/api/session',fresh)[1]['authenticated']
        check('real HTTP rejects the old authenticated cookie after recovery and accepts a fresh password login with normal Secure cookie rotation')
        status,restored_view,_=request(new_port,'GET',detail,fresh);assert status==200 and restored_view==original_view
        status,history,_=request(new_port,'GET','/api/documents',fresh);assert status==200 and [d['id'] for d in history['items']]==[document_id]
        assert request(new_port,'GET',preview,fresh,raw=True)[:2]==(200,image)
        assert request(new_port,'GET',detail,old_cookie)[0]==401
        assert request(new_port,'GET',preview,old_cookie,raw=True)[0]==401
        bob=login(new_port,'BOB')
        status,history,_=request(new_port,'GET','/api/documents',bob);assert status==200 and history['items']==[]
        assert request(new_port,'GET',detail,bob)[0]==404
        assert request(new_port,'GET',preview,bob,raw=True)[0]==404
        assert sha(owned)==hashlib.sha256(image).hexdigest() and sha(canary)==original_hash
        check('restored identity preserves existing document revision/TTL/page metadata and exact original bytes; old cookies and the independently restored Bob account cannot access Alice documents')
        result=cli(target,'state','restore',str(archive),ok=False);assert result.returncode!=0
        assert request(new_port,'GET','/api/session',fresh)[1]['authenticated']
        check('compiled restore refuses an active/nonempty destination without invalidating its new login')
        stop(new_api)
        lost_api,lost_port=start(target,new_docs)
        assert request(lost_port,'GET','/api/session',fresh)[1]['authenticated']
        status,history,_=request(lost_port,'GET','/api/documents',fresh);assert status==200 and history['items']==[]
        assert request(lost_port,'GET',detail,fresh)[0]==404
        assert request(lost_port,'GET',preview,fresh,raw=True)[0]==404
        with closing(sqlite3.connect(new_docs/'documents.sqlite3')) as db:
            for table in ['documents','artifacts','pages','jobs','reservations']:
                assert db.execute('SELECT count(*) FROM '+table).fetchone()[0]==0
        assert sha(owned)==hashlib.sha256(image).hexdigest() and sha(canary)==original_hash
        check('API restart with preserved recovered identity state and an empty document root keeps its fresh session but exposes no old history, document or original and restores no document/job/reservation rows')
        stop(lost_api)
        bad=backups/'bad.json';bad.write_bytes(data.rstrip()+b'{}');bad.chmod(0o600);empty=private(work/'failed-restore')
        result=cli(empty,'state','restore',str(bad),ok=False);assert result.returncode!=0 and not list(empty.iterdir())
        missing=private(work/'missing-source');result=cli(missing,'state','export',str(backups/'not-created.json'),ok=False)
        assert result.returncode!=0 and not list(missing.iterdir()) and not (backups/'not-created.json').exists()
        check('malformed restore leaves no published DB/residue; exporting absent state never initializes it')
        assert before=={str(path.relative_to(root)):sha(path) for path in sorted(sources)}
        receipt['archive_sha256']=sha(archive)
        receipt['passed']=True
except Exception as error:
    receipt['error']=str(error)
    raise
finally:
    cleanup=[]
    for child in children:
        try:stop(child)
        except Exception as error:cleanup.append(str(error))
    receipt['cleanup_errors']=cleanup
    if cleanup:receipt['passed']=False
    receipt['finished_at']=datetime.now(timezone.utc).isoformat()
    args.receipt.write_text(json.dumps(receipt,indent=2)+'\n')
    print(json.dumps({'passed':receipt['passed'],'checks':len(receipt['checks']),'cleanup_errors':len(cleanup)}))
if not receipt['passed']:
    raise SystemExit(1)
