#!/usr/bin/env python3
"""Trace actual core/terminal images through HTTP to synthetic published PDFs.

The host only orchestrates installed Docker. The inside helper has no Docker
socket, host credentials or real stores. PDF geometry/text inspection is separate.
"""
import argparse
import hashlib
import http.client
from http.cookies import SimpleCookie
import json
import os
from pathlib import Path
import re
import secrets
import sqlite3
import subprocess
import time
import uuid


def require(value,message):
    if not value:raise ValueError(message)


def read_json(path):
    require(path.is_file() and not path.is_symlink() and 0<path.stat().st_size<=8<<20,'invalid tracer metadata')
    return json.loads(path.read_bytes())


def candidate_identity(root):
    app,result=read_json(root/'application.json'),read_json(root/'result.json')
    source=app['source_revision'];images={}
    require(app['passed'] is True and result['passed'] is True and result['source_revision']==source
            and re.fullmatch('[0-9a-f]{40}',source),'tracer requires a successful same-source candidate')
    for role,prefix in [('api','image'),('tesseract','tesseract-image')]:
        info=read_json(root/(prefix+'.json'));proof=read_json(root/(prefix+'-payload.json'))
        require(len(info)==1 and re.fullmatch('sha256:[0-9a-f]{64}',info[0]['Id'])
                and proof['passed'] is True and proof['image_id']==info[0]['Id'],'tracer image/payload mismatch')
        images[role]=info[0]['Id']
    images['worker']=images['api']
    return {'source_revision':source,'images':images,'version':app['version']}


class Client:
    def __init__(self,host='api',port=8080):self.host=host;self.port=port;self.cookie=''

    def request(self,method,path,body=None,*,expected=200,revision=None,max_bytes=32<<20):
        require(path.startswith('/') and not path.startswith('//'),'invalid tracer request path')
        headers={'Host':'scanner-ocr.invalid'}
        if self.cookie:headers['Cookie']=self.cookie
        if method!='GET':headers.update(Origin='https://scanner-ocr.invalid',**{'Idempotency-Key':'trace_'+uuid.uuid4().hex})
        if revision is not None:headers['If-Match']='"'+str(revision)+'"'
        if body is not None:headers['Content-Type']='application/json'
        connection=http.client.HTTPConnection(self.host,self.port,timeout=10)
        try:
            connection.request(method,path,body=None if body is None else json.dumps(body),headers=headers)
            response=connection.getresponse();raw=response.read(max_bytes+1)
            require(response.status==expected and len(raw)<=max_bytes,'tracer HTTP status or body bound failed: '+path)
            for key,value in response.getheaders():
                if key.lower()=='set-cookie':
                    cookie=SimpleCookie();cookie.load(value)
                    self.cookie='; '.join(m.key+'='+m.value for m in cookie.values())
            return raw,dict(response.getheaders())
        finally:connection.close()


def wait_for(check,seconds=90):
    deadline=time.monotonic()+seconds
    while True:
        try:
            result=check()
            if result:return result
        except (OSError,ValueError,http.client.HTTPException):pass
        require(time.monotonic()<deadline,'tracer readiness deadline')
        time.sleep(.5)


def seed_document(name,raw):
    documents=Path('/var/lib/scanner/documents');now=int(time.time()*1000)
    with sqlite3.connect('/var/lib/scanner/state/state.sqlite3') as db:
        owners=list(db.execute("SELECT id FROM principals WHERE kind='guest' AND disabled_at_ms IS NULL"))
    require(len(owners)==1,'tracer must have exactly one synthetic owner');owner=owners[0][0]
    document='document_trace_'+name
    with sqlite3.connect(documents/'documents.sqlite3') as db:
        db.execute('PRAGMA foreign_keys=ON');db.execute('INSERT OR IGNORE INTO owners(id) VALUES(?)',(owner,))
        db.execute("INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES(?,?,'document','review',?,?)",(document,owner,now,now+3600000))
        for index,rotation in enumerate([0,90]):
            original='original_trace_'+name+'_'+str(index);page='page_trace_'+name+'_'+str(index)
            relative=original+'.png';path=documents/relative
            with path.open('xb') as stream:stream.write(raw)
            db.execute("INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES(?,?,?,'original','ready',?,'image/png',?,?,1,?)",
                       (original,document,owner,relative,len(raw),hashlib.sha256(raw).digest(),now))
            db.execute("INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,capture_size,capture_color,capture_dpi,rotation,pixel_width,pixel_height) VALUES(?,?,?,?,?,'a4','color',300,?,2480,3508)",
                       (page,document,owner,index,original,rotation))
    return document


def render(client,name,raw,ocr):
    document=seed_document(name,raw)
    data,_=client.request('GET','/api/documents/'+document);revision=json.loads(data)['revision']
    data,_=client.request('POST','/api/documents/'+document+'/render',
                         {'format':'pdf','pdf_encoding':'lossless','ocr':ocr},expected=202,revision=revision)
    job=json.loads(data);require(re.fullmatch('[A-Za-z0-9_-]{16,96}',job['id']),'invalid admitted job')
    def completed():
        data,_=client.request('GET','/api/jobs/'+job['id']);state=json.loads(data)['state']
        if state in ['failed','cancelled','uncertain']:raise RuntimeError('actual image render did not succeed: '+state)
        return state=='succeeded'
    wait_for(completed,180)
    pdf,_=client.request('GET','/api/documents/'+document+'/download')
    require(pdf.startswith(b'%PDF-') and len(pdf)>1024,'missing actual PDF output')
    digest=hashlib.sha256(pdf).digest()
    with sqlite3.connect('/var/lib/scanner/documents/documents.sqlite3') as db:
        relative,stored=db.execute('SELECT a.relative_path,a.sha256 FROM published_outputs p JOIN artifacts a ON a.id=p.artifact_id WHERE p.document_id=?',(document,)).fetchone()
        settings=json.loads(db.execute('SELECT configuration_json FROM jobs WHERE id=?',(job['id'],)).fetchone()[0])['settings']
    path=Path('/var/lib/scanner/documents')/relative
    require(not Path(relative).is_absolute() and '..' not in Path(relative).parts,'invalid publication path')
    require(digest==stored==hashlib.sha256(path.read_bytes()).digest(),'HTTP output differs from atomic publication')
    Path('/proof/'+name+'.pdf').write_bytes(pdf)
    return {'job_id':job['id'],'pdf_sha256':digest.hex(),'pdf_bytes':len(pdf),'settings':settings}


def inside(phase):
    os.umask(0o077)
    status={line.split(':',1)[0]:line.split(':',1)[1].strip() for line in Path('/proc/self/status').read_text().splitlines() if ':' in line}
    require(os.getuid()==10001 and status['NoNewPrivs']=='1'
            and all(int(status[k],16)==0 for k in ['CapEff','CapPrm','CapInh','CapBnd','CapAmb']),'tracer isolation failed')
    raw=Path('/fixture.png').read_bytes();client=Client()
    wait_for(lambda:client.request('GET','/health/ready')[0] is not None)
    if phase=='full':
        client.request('GET','/api/session');client.request('POST','/auth/guest')
        Path('/proof/session').write_text(client.cookie)
        def catalog():
            data,_=client.request('GET','/api/capabilities');value=json.loads(data)
            return value if any(p['id']=='tesseract' for p in value.get('ocr_plugins',[])) else None
        capabilities=wait_for(catalog);default=capabilities['defaults']['ocr']
        provider=next(p for p in capabilities['ocr_plugins'] if p['id']=='tesseract')
        model=next(m for m in provider['models'] if m['id']==default['model_id'])
        require(model.get('processing_destination'),'remote OCR destination is missing')
        results={'plain':render(client,'plain',raw,{'enabled':False})}
        results['ocr']=render(client,'ocr',raw,{'enabled':True,'provider_id':'tesseract',
            'model_id':model['id'],'languages':['ja','en'],'quality_preset':default['quality_preset']})
        require(results['ocr']['settings']['ocr']['model_version']==model['version'],'worker did not pin the advertised OCR model')
        results['model_version']=model['version'];results['destination']=model['processing_destination']
    else:
        client.cookie=Path('/proof/session').read_text()
        results={'outage':render(client,'outage',raw,{'enabled':False})}
    Path('/proof/'+phase+'.json').write_text(json.dumps({'passed':True,'fixture_sha256':hashlib.sha256(raw).hexdigest(),
        'uid':os.getuid(),'caps_zero':True,'no_new_privileges':True,**results},indent=2)+'\n')


def trace(candidate,output):
    identity=candidate_identity(candidate)
    require(not output.exists() and not output.is_symlink(),'tracer output already exists')
    output.mkdir(mode=0o700,parents=True)
    root=Path(__file__).resolve().parents[1];prefix='scanner-ocr-'+uuid.uuid4().hex
    revision=subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True,timeout=10).strip()
    require(revision==identity['source_revision'],'tracer source differs from built images')
    subprocess.run(['git','diff','--exit-code','HEAD','--'],cwd=root,check=True,capture_output=True,timeout=10)
    tool=[line.split()[1] for line in (root/'native/Dockerfile.apk-fetch').read_text().splitlines() if line.startswith('FROM python@sha256:')]
    require(len(tool)==1,'missing pinned Python helper image');tool=tool[0]
    containers=[];volumes=[];network=None
    proof={**identity,'passed':False,'publication_approved':False,
           'scope':'Actual separate core API/worker and terminal service, HTTP admission, atomic PDF publication and service outage; independent PDF inspection is separate',
           'script_sha256':hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
           'fixture_sha256':hashlib.sha256((root/'scripts/fixtures/ocr-ja-en-300dpi.png').read_bytes()).hexdigest()}
    key=output/'test-key.env';key.write_text('SCANNER_OCR_TESSERACT_API_KEY='+secrets.token_hex(32)+'\n');key.chmod(0o600)
    def docker(*args,timeout=30):
        return subprocess.run(['docker',*map(str,args)],check=True,capture_output=True,text=True,timeout=timeout).stdout.strip()
    common=['--pull=never','--read-only','--user','10001:10001','--cap-drop','ALL','--security-opt','no-new-privileges',
            '--cpus','2','--pids-limit','128','--ulimit','nofile=512:512','--ulimit','fsize=268435456:268435456',
            '--log-opt','max-size=1m','--log-opt','max-file=1']
    def create(name,image,*args,memory='1536m',command=()):
        cid=docker('create','--name',prefix+'-'+name,'--network',prefix,
                   *common,'--memory',memory,'--memory-swap',memory,*args,image,*command)
        require(re.fullmatch('[0-9a-f]{64}',cid),'invalid owned container ID');containers.append(cid);return cid
    def checked_stop(cid):
        docker('stop','--time','30',cid,timeout=45)
        info=json.loads(docker('inspect',cid))[0]
        require(info['Id']==cid and not info['State']['Running'] and not info['State']['OOMKilled']
                and info['State']['ExitCode']==0,'actual container did not stop cleanly')
        return {'id':cid,'image_id':info['Image'],'exit_code':0,'oom_killed':False}
    try:
        network=docker('network','create','--internal',prefix)
        for name,size in [('state','64m'),('documents','1g'),('proof','128m')]:
            volume=prefix+'-'+name
            docker('volume','create','--label','scanner-kiosk.tracer='+prefix,'--driver','local',
                   '--opt','type=tmpfs','--opt','device=tmpfs','--opt','o=size='+size+',uid=10001,gid=10001,mode=0700',volume)
            info=json.loads(docker('volume','inspect',volume))[0]
            require(info['Labels'].get('scanner-kiosk.tracer')==prefix,'volume ownership mismatch');volumes.append(volume)
        mounts=['--mount','type=volume,src='+prefix+'-state,dst=/var/lib/scanner/state',
                '--mount','type=volume,src='+prefix+'-documents,dst=/var/lib/scanner/documents']
        scratch=['--tmpfs','/tmp:rw,noexec,nosuid,nodev,size=512m,uid=10001,gid=10001,mode=0700']
        env=['-e','SCANNER_SCANNERS_JSON=[]','-e','SCANNER_OCR_PLUGINS=tesseract']
        sane=create('sane-version',identity['images']['api'],'--entrypoint','/usr/bin/scanimage',
                    memory='128m',command=['--version'])
        proof['sane_version']=docker('start','--attach',sane)
        require('scanimage' in proof['sane_version'] and len(proof['sane_version'])<4096,'core native scanner runtime failed')
        proof['sane_stop']=checked_stop(sane)
        tess=create('tesseract',identity['images']['tesseract'],'--network-alias','tesseract',
                    '--env-file',key,'--tmpfs','/tmp:rw,noexec,nosuid,nodev,size=1g,uid=10001,gid=10001,mode=0700')
        api=create('api',identity['images']['api'],*mounts,*scratch,*env,'--network-alias','api',
                   '-e','SCANNER_PUBLIC_URL=https://scanner-ocr.invalid','-e','SCANNER_AUTH_MODE=guest',memory='512m')
        worker_args=[*mounts,*scratch,*env,'--env-file',key,'-e','SCANNER_OCR_TESSERACT_ENDPOINT=http://tesseract:8090',
                     '-e','SCANNER_OCR_TESSERACT_ALLOW_INSECURE_HTTP=true']
        worker=create('worker',identity['images']['worker'],*worker_args,command=['worker'])
        for cid in [tess,api,worker]:docker('start',cid)
        def helper(phase):
            cid=create(phase,tool,
                *mounts,'--mount','type=volume,src='+prefix+'-proof,dst=/proof',
                '--mount','type=bind,src='+str(Path(__file__).resolve())+',dst=/check.py,readonly',
                '--mount','type=bind,src='+str(root/'scripts/fixtures/ocr-ja-en-300dpi.png')+',dst=/fixture.png,readonly',
                '--mount','type=bind,src=/usr,dst=/usr,readonly','--mount','type=bind,src=/usr/bin,dst=/bin,readonly',
                '--mount','type=bind,src=/usr/lib,dst=/lib,readonly','--mount','type=bind,src=/usr/lib64,dst=/lib64,readonly',
                '--tmpfs','/work:rw,noexec,nosuid,nodev,size=64m,uid=10001,gid=10001,mode=0700',
                '--entrypoint','/usr/bin/env',memory='512m',command=['-i','PATH=/usr/bin:/bin','HOME=/work','TMPDIR=/work',
                'PYTHONDONTWRITEBYTECODE=1','python3','/check.py','--inside',phase])
            try:docker('start','--attach',cid,timeout=300)
            except subprocess.SubprocessError:
                (output/(phase+'-failure.log')).write_text(docker('logs',cid));raise
            info=json.loads(docker('inspect',cid))[0]
            require(info['State']['ExitCode']==0 and not info['State']['OOMKilled'],'actual tracer helper failed')
            return cid
        first=helper('full')
        for name in ['plain.pdf','ocr.pdf','full.json']:docker('cp',first+':/proof/'+name,output/name)
        proof['tesseract_stop']=checked_stop(tess)
        last=helper('outage')
        for name in ['outage.pdf','outage.json']:docker('cp',last+':/proof/'+name,output/name)
        proof['core_stops']=[checked_stop(api),checked_stop(worker)]
        proof['full']=read_json(output/'full.json');proof['outage']=read_json(output/'outage.json')
        require(proof['full']['fixture_sha256']==proof['outage']['fixture_sha256']==proof['fixture_sha256'],
                'actual image tracer used a different fixture')
        proof['passed']=proof['full']['passed'] is True and proof['outage']['passed'] is True
        proof['independent_pdf_inspection_passed']=False
    finally:
        cleanup=[]
        for cid in reversed(containers):
            try:docker('rm','--force',cid)
            except subprocess.SubprocessError:cleanup.append('container '+cid)
        for volume in reversed(volumes):
            try:docker('volume','rm',volume)
            except subprocess.SubprocessError:cleanup.append('volume '+volume)
        if network:
            try:docker('network','rm',network)
            except subprocess.SubprocessError:cleanup.append('network '+network)
        key.unlink(missing_ok=True)
        proof['cleanup_errors']=cleanup;proof['passed']=proof['passed'] and not cleanup
        (output/'receipt.json').write_text(json.dumps(proof,indent=2)+'\n')
    require(proof['passed'],'actual image tracer or cleanup failed')


if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--candidate',type=Path);parser.add_argument('--output',type=Path)
    parser.add_argument('--inside',choices=['full','outage']);args=parser.parse_args()
    if args.inside:inside(args.inside)
    elif args.candidate and args.output:trace(args.candidate.resolve(strict=True),args.output.absolute())
    else:parser.error('provide --candidate and a new --output directory')
