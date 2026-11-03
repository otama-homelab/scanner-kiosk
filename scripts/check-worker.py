#!/usr/bin/env python3
"""Build/run the product worker CLI with isolated synthetic SQLite/image data."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import sqlite3
import subprocess
import tempfile
import time
from PIL import Image
import pikepdf

parser = argparse.ArgumentParser()
parser.add_argument('--receipt', required=True)
args = parser.parse_args()
root = Path(__file__).resolve().parents[1]
files = sorted([*root.glob('cmd/**/*.go'), *root.glob('internal/**/*.go'), *root.glob('internal/**/*.sql'), *root.glob('pkg/**/*.go'), root/'go.mod', root/'go.sum', Path(__file__).resolve()])
def hashes():
    return {str(p.relative_to(root)): hashlib.sha256(p.read_bytes()).hexdigest() for p in files}
before = hashes()
checks = []
with tempfile.TemporaryDirectory(prefix='scanner-worker-') as directory:
    work = Path(directory)
    binary = work/'scanner'
    subprocess.run(['go', 'build', '-o', str(binary), './cmd/scanner'], cwd=root, check=True, capture_output=True)
    identity=json.loads(subprocess.run([str(binary),'version'],env={'PATH':os.environ['PATH']},check=True,capture_output=True,timeout=10).stdout)
    assert identity['version']=='0.0.0-dev' and identity['revision']=='unreleased' and identity['go_version']=='go1.27.3' and identity['os']=='linux'
    checks.append('version reports explicit unreleased identity without configuration, credentials or stores')
    state_root, document_root = work/'state', work/'documents'
    for path in [state_root, document_root]:
        path.mkdir(mode=0o700)
    env = {'PATH': os.environ['PATH'], 'SCANNER_STATE_DIR': str(state_root), 'SCANNER_DOCUMENT_DIR': str(document_root), 'SCANNER_SCANNERS_JSON': json.dumps([{'id':'scanner_fixture_001','name':'Fixture','driver':'sane-airscan','endpoint':'http://192.0.2.10/eSCL'}])}
    def run():
        subprocess.run([str(binary), 'worker', '--once'], env=env, check=True, capture_output=True, timeout=30)
    run()
    checks.append('compiled CLI initializes private stores without HTTP/OIDC configuration')
    # Legacy directory-only residue, after old leaf removal acknowledgements.
    empty_scopes=[document_root/('work_%048x'%index) for index in range(137)]
    empty_scopes += [document_root/('capture_%048x'%index) for index in range(4)]
    for path in empty_scopes:
        path.mkdir(mode=0o700)
    quarantine=document_root/'.cleanup';quarantine.mkdir(mode=0o700)
    for index in range(129):
        (quarantine/('job_empty_legacy_%04d'%index)).mkdir(mode=0o700)
    retained_scope=document_root/('work_%048x'%1000);retained_scope.mkdir(mode=0o700)
    retained_file=retained_scope/'retained.bin';retained_file.write_bytes(b'retained private fixture');retained_file.chmod(0o600)
    outside=work/'outside-sweep';outside.mkdir(mode=0o700)
    protected_link=document_root/('work_%048x'%1001);protected_link.symlink_to(outside,target_is_directory=True)
    run()
    assert all(not path.exists() for path in empty_scopes) and not quarantine.exists()
    assert retained_file.read_bytes()==b'retained private fixture' and protected_link.is_symlink() and outside.is_dir()
    run()
    assert retained_file.read_bytes()==b'retained private fixture' and protected_link.is_symlink()
    checks.append('compiled startup streams 137 empty legacy scopes, 4 captures and 129 quarantine directories across batches, retains nonempty private bytes/symlink target and repeats without a job')
    image_path = document_root/'original.png'
    Image.new('L', (80,100), 210).save(image_path)
    image_path.chmod(0o600)
    raw = image_path.read_bytes()
    now = int(time.time()*1000)
    owner, document, job = 'principal_cli_00001', 'document_cli_000001', 'job_cli_render_0001'
    original, page = 'original_cli_00001', 'page_cli_00000001'
    with sqlite3.connect(state_root/'state.sqlite3') as state:
        state.execute("INSERT INTO principals(id,kind,created_at_ms) VALUES(?,'guest',?)", (owner,now))
    # Synthetic admitted-job fixture; the Go runtime tests separately use real admission.
    with sqlite3.connect(document_root/'documents.sqlite3') as docs:
        docs.execute('PRAGMA foreign_keys=ON')
        docs.execute('INSERT INTO owners(id) VALUES(?)',(owner,))
        docs.execute("INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES(?,?,'document','processing',?,?)",(document,owner,now,now+3600000))
        docs.execute("INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES(?,?,?,'original','ready','original.png','image/png',?,?,1,?)",(original,document,owner,len(raw),hashlib.sha256(raw).digest(),now))
        docs.execute("INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,capture_size,capture_color,capture_dpi,rotation) VALUES(?,?,?,0,?,'a4','gray',300,90)",(page,document,owner,original))
        snapshot = {'version':1,'settings':{'format':'pdf','pdf_encoding':'lossless','ocr':{'enabled':False}},'pages':[{'id':page,'original_artifact_id':original,'rotation':90,'size':'a4','color':'gray','dpi':300}]}
        docs.execute("INSERT INTO jobs(id,document_id,owner_id,kind,state,input_revision,configuration_json,created_at_ms,deadline_at_ms) VALUES(?,?,?,'render','queued',1,?,?,?)",(job,document,owner,json.dumps(snapshot),now,now+60000))
        docs.execute('INSERT INTO reservations(job_id,owner_id,byte_size) VALUES(?,?,?)',(job,owner,1<<20))
    run()
    with sqlite3.connect(document_root/'documents.sqlite3') as docs:
        state = docs.execute('SELECT state,runner_id,started_at_ms FROM jobs WHERE id=?',(job,)).fetchone()
        assert state[0]=='succeeded' and state[1].startswith('worker_') and state[2]>=now
        assert docs.execute('SELECT count(*) FROM reservations').fetchone()[0]==0
        relative, digest = docs.execute('SELECT a.relative_path,a.sha256 FROM published_outputs p JOIN artifacts a ON a.id=p.artifact_id WHERE p.document_id=?',(document,)).fetchone()
    pdf_path = document_root/relative
    assert hashlib.sha256(pdf_path.read_bytes()).digest()==digest
    with pikepdf.open(pdf_path) as pdf:
        assert len(pdf.pages)==1 and int(pdf.pages[0].obj.get('/Rotate',0))==90
        decoded = pikepdf.PdfImage(next(iter(pdf.pages[0].get_images().values()))).as_pil_image()
        assert decoded.convert('L').tobytes()==Image.open(image_path).tobytes()
    assert image_path.read_bytes()==raw
    checks.append('queued synthetic job claimed by fresh boot, generated PDF published, reservation released and pixels/rotation independently checked')
    with sqlite3.connect(document_root/'documents.sqlite3') as docs:
        thumb_relative,thumb_digest,thumb_bytes,tw,th=docs.execute("SELECT a.relative_path,a.sha256,a.byte_size,t.pixel_width,t.pixel_height FROM page_thumbnails t JOIN artifacts a ON a.id=t.artifact_id WHERE t.original_artifact_id=?",(original,)).fetchone()
        assert docs.execute('SELECT count(*) FROM job_artifacts WHERE job_id=?',(job,)).fetchone()[0]==2
    thumb_path=document_root/thumb_relative
    assert hashlib.sha256(thumb_path.read_bytes()).digest()==thumb_digest and thumb_path.stat().st_size==thumb_bytes<=1<<20
    with Image.open(thumb_path) as thumb:
        assert thumb.format=='JPEG' and thumb.size==(tw,th)==(80,100) and abs(thumb.convert('L').getpixel((40,50))-210)<=2
    checks.append('compiled worker atomically publishes source-bound JPEG thumbnail with independent hash, geometry, gray pixels and original orientation')

    run()
    with sqlite3.connect(document_root/'documents.sqlite3') as docs:
        assert docs.execute('SELECT state,runner_id,started_at_ms FROM jobs WHERE id=?',(job,)).fetchone()==state
    assert hashlib.sha256(pdf_path.read_bytes()).digest()==digest
    checks.append('second CLI invocation does not replay completed work')
    for format in ['jpeg', 'png']:
        photo_document = 'document_photo_' + format
        photo_job = 'job_photo_export_' + format
        photo_original = 'original_photo_' + format
        photo_page = 'page_photo_export_' + format
        photo_path = document_root / ('photo_' + format + '.png')
        photo_path.write_bytes(raw)
        photo_path.chmod(0o600)
        snapshot = {'version':1,'settings':{'format':format,'ocr':{'enabled':False}},'pages':[{'id':photo_page,'original_artifact_id':photo_original,'rotation':90,'size':'photo10x15','color':'gray','dpi':300}]}
        with sqlite3.connect(document_root/'documents.sqlite3') as docs:
            docs.execute('PRAGMA foreign_keys=ON')
            docs.execute("INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES(?,?,'photo','processing',?,?)",(photo_document,owner,now,now+3600000))
            docs.execute("INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES(?,?,?,'original','ready',?,'image/png',?,?,1,?)",(photo_original,photo_document,owner,photo_path.name,len(raw),hashlib.sha256(raw).digest(),now))
            docs.execute("INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,capture_size,capture_color,capture_dpi,rotation) VALUES(?,?,?,0,?,'photo10x15','gray',300,90)",(photo_page,photo_document,owner,photo_original))
            docs.execute("INSERT INTO jobs(id,document_id,owner_id,kind,state,input_revision,configuration_json,created_at_ms,deadline_at_ms) VALUES(?,?,?,'render','queued',1,?,?,?)",(photo_job,photo_document,owner,json.dumps(snapshot),now,now+60000))
            docs.execute('INSERT INTO reservations(job_id,owner_id,byte_size) VALUES(?,?,?)',(photo_job,owner,1<<20))
        run()
        with sqlite3.connect(document_root/'documents.sqlite3') as docs:
            relative, media = docs.execute('SELECT a.relative_path,a.media_type FROM published_outputs p JOIN artifacts a ON a.id=p.artifact_id WHERE p.document_id=?',(photo_document,)).fetchone()
            assert docs.execute('SELECT count(*) FROM reservations WHERE job_id=?',(photo_job,)).fetchone()[0]==0
        assert media == 'image/' + format
        with Image.open(document_root/relative) as exported:
            assert exported.format.lower()==format and exported.size==(100,80)
            if format=='png':
                assert exported.convert('L').tobytes()==Image.open(photo_path).transpose(Image.Transpose.ROTATE_270).tobytes()
        assert photo_path.read_bytes()==raw
        checks.append('compiled CLI photo ' + format + ': dimensions, rotation, publication, reservation and original retention')
    # Real file cleanup through the compiled worker, with synthetic delete intent.
    cleanup_job='job_cli_cleanup_001';now=int(time.time()*1000)
    with sqlite3.connect(document_root/'documents.sqlite3') as docs:
        deleted_paths=[document_root/row[0] for row in docs.execute('SELECT relative_path FROM artifacts WHERE document_id=?',(document,))]
        docs.execute("UPDATE documents SET state='deleting' WHERE id=?",(document,))
        docs.execute("INSERT INTO jobs(id,document_id,owner_id,kind,state,input_revision,configuration_json,created_at_ms,deadline_at_ms) SELECT ?,id,owner_id,'cleanup','queued',revision,'{\"version\":1}',?,? FROM documents WHERE id=?",(cleanup_job,now,now+60000,document))
    run()
    with sqlite3.connect(document_root/'documents.sqlite3') as docs:
        assert docs.execute('SELECT state FROM jobs WHERE id=?',(cleanup_job,)).fetchone()[0]=='succeeded'
        assert all(docs.execute('SELECT count(*) FROM '+table+' WHERE document_id=?',(document,)).fetchone()[0]==0 for table in ['artifacts','pages','published_outputs','page_thumbnails'])
        assert docs.execute('SELECT count(*) FROM cleanup_files WHERE job_id=?',(cleanup_job,)).fetchone()[0]==0
    assert deleted_paths and all(not path.exists() for path in deleted_paths)
    assert all(not path.parent.exists() for path in deleted_paths if path.parent!=document_root), 'empty source scope retained'
    assert not (document_root/'.cleanup').exists(), 'empty quarantine retained'
    run()
    checks.append('compiled worker delete intent -> immutable manifest -> quarantined file removal -> durable progress -> quota/status completion, repeat boot does not repeat cleanup')
    expired_document='document_expired_cli';expired_original='original_expired_cli';expired_page='page_expired_cli_001';expired_path=document_root/'expired.png';expired_path.write_bytes(raw);expired_path.chmod(0o600);now=int(time.time()*1000)
    with sqlite3.connect(state_root/'state.sqlite3') as state:
        state.execute('UPDATE principals SET disabled_at_ms=? WHERE id=?',(now,owner))
    with sqlite3.connect(document_root/'documents.sqlite3') as docs:
        docs.execute("INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES(?,?,'document','review',?,?)",(expired_document,owner,now-10000,now-1))
        docs.execute("INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES(?,?,?,'original','ready','expired.png','image/png',?,?,1,?)",(expired_original,expired_document,owner,len(raw),hashlib.sha256(raw).digest(),now-10000))
        docs.execute("INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,capture_size,capture_color,capture_dpi) VALUES(?,?,?,0,?,'a4','gray',300)",(expired_page,expired_document,owner,expired_original))
    run()
    with sqlite3.connect(document_root/'documents.sqlite3') as docs:
        assert docs.execute("SELECT state FROM jobs WHERE document_id=? AND kind='cleanup'",(expired_document,)).fetchone()[0]=='succeeded'
        assert docs.execute('SELECT count(*) FROM artifacts WHERE document_id=?',(expired_document,)).fetchone()[0]==0
    assert not expired_path.exists()
    checks.append('compiled worker startup TTL sweep cleans expired private files for a disabled owner without a browser session')
    # Simulate persisted pre-crash states; acquisition is never enabled.
    for restart_state in ['source','quarantine','absent','acknowledged']:
        stamp=int(time.time()*1000)
        restart_doc='document_restart_'+restart_state
        restart_job='job_restart_cleanup_'+restart_state
        restart_art='original_restart_'+restart_state
        previous='worker_restart_previous_01'
        source=document_root/('restart_'+restart_state+'.png')
        source.write_bytes(raw);source.chmod(0o600)
        with sqlite3.connect(document_root/'documents.sqlite3') as docs:
            docs.execute('PRAGMA foreign_keys=ON')
            docs.execute("INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES(?,?,'document','expired',?,?)",(restart_doc,owner,stamp-20000,stamp-8000))
            docs.execute("INSERT INTO jobs(id,document_id,owner_id,kind,state,input_revision,configuration_json,created_at_ms,deadline_at_ms,runner_id,started_at_ms,heartbeat_at_ms) VALUES(?,?,?,'cleanup','running',1,'{\"version\":1}',?,?,?,?,?)",(restart_job,restart_doc,owner,stamp-10000,stamp-5000,previous,stamp-9000,stamp-9000))
            docs.execute("INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES(?,?,?,'original','deleting',?,'image/png',?,?,1,?)",(restart_art,restart_doc,owner,source.name,len(raw),hashlib.sha256(raw).digest(),stamp-19000))
            docs.execute("INSERT INTO cleanup_files(job_id,owner_id,artifact_id,relative_path,byte_size,sha256) VALUES(?,?,?,?,?,?)",(restart_job,owner,restart_art,source.name,len(raw),hashlib.sha256(raw).digest()))
            docs.execute("INSERT INTO cleanup_attempts(job_id,owner_id,runner_id,generation,started_at_ms,deadline_at_ms) VALUES(?,?,?,1,?,?)",(restart_job,owner,previous,stamp-9000,stamp-6000))
            if restart_state=='acknowledged':
                docs.execute("UPDATE cleanup_files SET state='deleted',deleted_at_ms=? WHERE job_id=?",(stamp-8500,restart_job))
        target=document_root/'.cleanup'/restart_job/restart_art
        if restart_state=='quarantine':
            target.parent.parent.mkdir(mode=0o700,exist_ok=True)
            target.parent.mkdir(mode=0o700)
            source.rename(target)
        elif restart_state in ['absent','acknowledged']:
            source.unlink()
        run()
        with sqlite3.connect(document_root/'documents.sqlite3') as docs:
            status=docs.execute('SELECT state,runner_id,started_at_ms,deadline_at_ms FROM jobs WHERE id=?',(restart_job,)).fetchone()
            assert status[0]=='succeeded' and status[1]!=previous and status[2:]==(stamp-9000,stamp-5000)
            assert docs.execute('SELECT count(*) FROM artifacts WHERE document_id=?',(restart_doc,)).fetchone()[0]==0
            assert docs.execute('SELECT count(*) FROM cleanup_files WHERE job_id=?',(restart_job,)).fetchone()[0]==0
            assert docs.execute('SELECT count(*) FROM cleanup_attempts WHERE job_id=?',(restart_job,)).fetchone()[0]==0
        assert not source.exists() and not target.exists()
        assert not (document_root/'.cleanup').exists(), 'empty restart quarantine retained'
        run()
        checks.append('compiled fresh-boot cleanup resumes expired job/attempt from '+restart_state+' with disabled owner, immutable job times, quota completion and repeat boot')
    # Status retention: full startup batch followed by a second pass. Receipt
    # hashes survive while their old result FK becomes NULL, never a fresh key.
    stamp=int(time.time()*1000);day=86400000
    with sqlite3.connect(document_root/'documents.sqlite3') as docs:
        docs.execute('PRAGMA foreign_keys=ON')
        for index in range(102):
            retired_doc='document_retirement_%03d'%index
            retired_job='job_retirement_%03d'%index
            docs.execute("INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES(?,?,'document','expired',?,?)",(retired_doc,owner,stamp-10*day,stamp-9*day))
            docs.execute("INSERT INTO jobs(id,document_id,owner_id,kind,state,input_revision,configuration_json,created_at_ms,deadline_at_ms,runner_id,started_at_ms,heartbeat_at_ms) VALUES(?,?,?,'cleanup','succeeded',1,'{\"version\":1}',?,?,'worker_retirement_01',?,?)",(retired_job,retired_doc,owner,stamp-8*day,stamp-6*day,stamp-8*day,stamp-7*day))
            key=hashlib.sha256(('retirement_key_%03d'%index).encode()).digest()
            docs.execute("INSERT INTO request_receipts(owner_id,key_hash,operation,request_hash,result_job_id,expires_at_ms) VALUES(?,?,'delete_document',?,?,?)",(owner,key,key,retired_job,stamp+60000 if index==101 else stamp-1))
    run()
    with sqlite3.connect(document_root/'documents.sqlite3') as docs:
        assert docs.execute("SELECT count(*) FROM documents WHERE id LIKE 'document_retirement_%'").fetchone()[0]==1
        assert docs.execute("SELECT count(*) FROM jobs WHERE id LIKE 'job_retirement_%'").fetchone()[0]==1
        assert docs.execute("SELECT count(*) FROM request_receipts WHERE owner_id=? AND result_job_id IS NULL",(owner,)).fetchone()[0]==101
        assert docs.execute("SELECT count(*) FROM request_receipts WHERE result_job_id='job_retirement_101'").fetchone()[0]==1
    run()
    checks.append('compiled startup retires 101 completed content-free status rows across batches for a disabled owner, preserves all receipt hashes/null result tombstones, protects a future receipt and repeats safely')
    # Only proven-aged document request hashes are eligible for retirement.
    stamp=int(time.time()*1000)
    with sqlite3.connect(document_root/'documents.sqlite3') as docs:
        legacy_before=docs.execute('SELECT count(*) FROM request_receipts WHERE key_expires_at_ms IS NULL').fetchone()[0]
        jobs_before=docs.execute('SELECT count(*) FROM jobs').fetchone()[0]
        for index in range(102):
            key=('rq1_%013d_%032x'%(stamp-86401000,index)).encode();digest=hashlib.sha256(key).digest()
            docs.execute("INSERT INTO request_receipts(owner_id,key_hash,operation,request_hash,expires_at_ms,key_expires_at_ms) VALUES(?,?,'scan-new',?,?,?)",(owner,digest,digest,stamp-1,stamp-1000))
        for label,receipt_until,key_until in [('key-live',stamp-1,stamp+60000),('receipt-live',stamp+60000,stamp-1000)]:
            digest=hashlib.sha256(label.encode()).digest()
            docs.execute("INSERT INTO request_receipts(owner_id,key_hash,operation,request_hash,expires_at_ms,key_expires_at_ms) VALUES(?,?,'scan-new',?,?,?)",(owner,digest,digest,receipt_until,key_until))
    run()
    with sqlite3.connect(document_root/'documents.sqlite3') as docs:
        assert docs.execute('SELECT count(*) FROM request_receipts WHERE key_expires_at_ms IS NOT NULL').fetchone()[0]==2
        assert docs.execute('SELECT count(*) FROM request_receipts WHERE key_expires_at_ms IS NULL').fetchone()[0]==legacy_before
        assert docs.execute('SELECT retired_at_ms FROM request_retirement_fence').fetchone()[0]>=stamp
        assert docs.execute('SELECT count(*) FROM jobs').fetchone()[0]==jobs_before
    run()
    checks.append('compiled startup drains 102 aged request hashes across batches for a disabled owner, preserves legacy/live-key/live-receipt rows and jobs, advances the durable clock fence and repeats safely')
    daemon_env = dict(env, SCANNER_OCR_PLUGINS='', SCANNER_OCR_DEFAULT_PLUGIN='', SCANNER_OCR_CATALOG_LEASE_SECONDS='15')
    for authority_loss in [False, True]:
        process = subprocess.Popen([str(binary), 'worker'], env=daemon_env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        try:
            deadline = time.monotonic() + 10
            while True:
                assert process.poll() is None, 'daemon exited before catalog publication'
                with sqlite3.connect(document_root/'documents.sqlite3') as docs:
                    publication = docs.execute("SELECT c.state,s.payload FROM processing_coordinator c JOIN ocr_catalog_snapshot s ON c.boot_id=s.boot_id AND c.generation=s.generation WHERE c.state='active'").fetchone()
                if publication:
                    assert json.loads(publication[1]).get('providers') in [None, []]
                    break
                assert time.monotonic() < deadline, 'daemon catalog publication timeout'
                time.sleep(0.02)
            if authority_loss:
                # Database authority-loss fixture; no device takeover is implied.
                with sqlite3.connect(document_root/'documents.sqlite3') as docs:
                    docs.execute("UPDATE processing_coordinator SET state='stopped'")
            else:
                process.terminate()
            process.communicate(timeout=10)
            assert process.returncode == (1 if authority_loss else 0), 'incorrect daemon exit status'
            with sqlite3.connect(document_root/'documents.sqlite3') as docs:
                assert docs.execute('SELECT state FROM processing_coordinator').fetchone()[0]=='stopped'
                if not authority_loss:
                    assert docs.execute('SELECT count(*) FROM ocr_catalog_snapshot').fetchone()[0]==0
        finally:
            if process.poll() is None:
                process.kill()
                process.communicate(timeout=5)
        checks.append('daemon authority loss returns failure' if authority_loss else 'daemon publishes disabled-OCR catalog and SIGTERM withdraws it with successful exit')
    assert subprocess.run([str(binary),'unknown-command'],env=env,capture_output=True,timeout=10).returncode==2
    checks.append('unknown command rejected explicitly')
assert before==hashes()
receipt = {'passed':True,'checks':checks,'source_sha256':before,'scope':'Compiled product worker CLI, real private SQLite and synthetic PNG/PDF only; no HTTP, scanner, OCR, production deployment or physical scanner/native restart recovery acceptance'}
Path(args.receipt).write_text(json.dumps(receipt,indent=2)+'\n')
print(json.dumps({'passed':True,'checks':len(checks)}))
