#!/usr/bin/env python3
"""Measure isolated product API/worker resources using synthetic retained pages.
No scanner, OCR provider, production store or container is contacted. Admission
uses the real HTTP API; only retained original/page fixtures are seeded in SQL.
"""
import argparse
import hashlib
import http.client
import importlib.metadata
import json
import math
import os
from pathlib import Path
import platform
import signal
import socket
import sqlite3
import subprocess
import tempfile
import threading
import time
import uuid

import numpy as np
from PIL import Image, ImageDraw
import pikepdf

parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--receipt',required=True,type=Path)
parser.add_argument('--case',action='append',dest='selected')
parser.add_argument('--gomemlimit',default=None,choices=['768MiB','1024MiB','1152MiB'])
parser.add_argument('--timeout',type=float,default=300)
parser.add_argument('--rss-ceiling-mib',type=int,default=2048)
args=parser.parse_args()
root=Path(__file__).resolve().parents[1]
sources=sorted([*root.glob('cmd/**/*.go'),*root.glob('internal/**/*.go'),*root.glob('internal/**/*.sql'),*root.glob('pkg/**/*.go'),root/'go.mod',root/'go.sum',Path(__file__).resolve()])
def hashes():return {str(p.relative_to(root)):hashlib.sha256(p.read_bytes()).hexdigest() for p in sources}
before=hashes()
cases=[]
for dpi in [300,600]:
 for color in ['gray','color']:
  for encoding in ['jpeg','lossless']:
   cases.append(dict(name=f'a4-{dpi}-{color}-{encoding}',dpi=dpi,color=color,encoding=encoding,pages=1,source='png',pattern='noise',expected='succeeded'))
cases.extend([
 dict(name='a4-300-gray-50-pages',dpi=300,color='gray',encoding='jpeg',pages=50,source='png',pattern='text',expected='succeeded'),
 dict(name='a4-600-color-50-low-jpeg',dpi=600,color='color',encoding='jpeg',pages=50,source='jpeg',quality=10,pattern='noise',expected='succeeded'),
 dict(name='a4-600-color-jpeg-expansion',dpi=600,color='color',encoding='lossless',pages=8,source='jpeg',pattern='noise',expected='failed'),
])
if args.selected:
 unknown=set(args.selected)-{c['name'] for c in cases}
 if unknown:parser.error('unknown cases: '+','.join(sorted(unknown)))
 cases=[c for c in cases if c['name'] in args.selected]
if not 1<=args.rss_ceiling_mib<=8192 or not 1<=args.timeout<=900:parser.error('invalid resource observation bounds')

def spawn(binary,command,env,log,usage_path):
 # Native time forks after exec, so the measured command does not inherit the
 # Python fixture generator's RSS high-water mark through its pre-exec address
 # space. Direct wait4(Python's child) does carry that historical peak on Linux.
 process=subprocess.Popen(['/usr/bin/time','--quiet','-o',str(usage_path),'-f','{"peak_rss_kib":%M,"user_cpu_seconds":%U,"system_cpu_seconds":%S}',str(binary),*command],env=env,stdout=log,stderr=log,start_new_session=True)
 process.target_pid=None;process.target_pidfd=None;process.usage_path=usage_path
 deadline=time.monotonic()+5
 while time.monotonic()<deadline:
  children=Path(f'/proc/{process.pid}/task/{process.pid}/children')
  try:pids=children.read_text().split()
  except FileNotFoundError:break
  if pids:
   target=int(pids[0]);fd=None
   try:
    fd=os.pidfd_open(target)
    # Validate parent identity after opening the pidfd; a recycled PID is
    # never a signalling target. Native time runs exactly one owned command.
    status=Path(f'/proc/{target}/status').read_text()
    parent=next(int(x.split()[1]) for x in status.splitlines() if x.startswith('PPid:'))
    if parent==process.pid:process.target_pid=target;process.target_pidfd=fd;break
    os.close(fd);fd=None
   except (ProcessLookupError,FileNotFoundError):
    if fd is not None:os.close(fd)
  if usage_path.exists() and usage_path.stat().st_size:break
  time.sleep(.002)
 return process

def command_usage(process):
 data=json.loads(process.usage_path.read_text())
 data['peak_rss_bytes']=data.pop('peak_rss_kib')*1024
 if process.target_pidfd is not None:os.close(process.target_pidfd);process.target_pidfd=None
 return data

def reap(process):
 pid,status,_=os.wait4(process.pid,0);process.returncode=os.waitstatus_to_exitcode(status)
 return command_usage(process)

def stop(process):
 if process.returncode is not None:return None
 if process.target_pidfd is not None:
  try:signal.pidfd_send_signal(process.target_pidfd,signal.SIGTERM)
  except ProcessLookupError:pass
 else:os.killpg(process.pid,signal.SIGTERM)
 end=time.monotonic()+5
 while time.monotonic()<end:
  pid,status,_=os.wait4(process.pid,os.WNOHANG)
  if pid:process.returncode=os.waitstatus_to_exitcode(status);return command_usage(process)
  time.sleep(.02)
 if process.target_pidfd is not None:
  try:signal.pidfd_send_signal(process.target_pidfd,signal.SIGKILL)
  except ProcessLookupError:pass
 else:os.killpg(process.pid,signal.SIGKILL)
 return reap(process)

def rss(pid):
 try:
  for line in Path(f'/proc/{pid}/status').read_text().splitlines():
   if line.startswith('VmRSS:'):return int(line.split()[1])*1024
 except FileNotFoundError:pass
 return 0

def volume_size(path):
 logical=allocated=0
 for base,dirs,files in os.walk(path):
  for name in files:
   try:stat=(Path(base)/name).stat(follow_symlinks=False)
   except FileNotFoundError:continue
   logical+=stat.st_size;allocated+=stat.st_blocks*512
 return logical,allocated

def request(port,path,method='GET',body=None,cookie=None,extra=None):
 headers={'Host':'scanner.example.test'}
 if method not in ['GET','HEAD']:headers.update({'Origin':'https://scanner.example.test','Idempotency-Key':f'rq1_{int(time.time()*1000):013d}_{uuid.uuid4().hex}'})
 if cookie:headers['Cookie']=cookie
 if body is not None:body=json.dumps(body).encode();headers['Content-Type']='application/json'
 if extra:headers.update(extra)
 connection=http.client.HTTPConnection('127.0.0.1',port,timeout=3)
 try:
  connection.request(method,path,body,headers);response=connection.getresponse();data=response.read()
  return response.status,dict(response.getheaders()),json.loads(data) if data else None
 finally:connection.close()

def fixture_image(path,case,index):
 width,height=(2480,3508) if case['dpi']==300 else (4960,7016)
 mode='L' if case['color']=='gray' else 'RGB'
 if case['pattern']=='noise':
  shape=(height,width) if mode=='L' else (height,width,3)
  image=Image.fromarray(np.random.default_rng(43000+index).integers(0,256,size=shape,dtype=np.uint8))
 else:
  image=Image.new(mode,(width,height),255 if mode=='L' else (255,255,255));draw=ImageDraw.Draw(image)
  for y in range(160,height-160,52):
   for x in range(160,width-160,40):
    if (x//40+y//52+index)%5:draw.rectangle((x,y,x+23,y+12),fill=0 if mode=='L' else (0,0,0))
 if case['source']=='jpeg':image.save(path,format='JPEG',quality=case.get('quality',85))
 else:image.save(path,format='PNG',compress_level=3)
 path.chmod(0o600)
 return width,height

def measure_case(binary,work,case):
 home=work/case['name'];home.mkdir(mode=0o700)
 state=home/'state';documents=home/'documents'
 for path in [state,documents]:path.mkdir(mode=0o700)
 with socket.socket() as reserve:reserve.bind(('127.0.0.1',0));port=reserve.getsockname()[1]
 # Deliberately isolated role environments: no inherited credentials/provider or
 # scanner acquisition settings. GOMAXPROCS is CPU scheduling, not a CPU quota.
 env={'PATH':os.environ['PATH'],'GOMAXPROCS':'2','SCANNER_STATE_DIR':str(state),'SCANNER_DOCUMENT_DIR':str(documents),'SCANNER_SCANNERS_JSON':'[]','SCANNER_OCR_PLUGINS':'','SCANNER_OCR_DEFAULT_PLUGIN':''}
 worker_env=dict(env)
 if args.gomemlimit:worker_env['GOMEMLIMIT']=args.gomemlimit
 api_env=dict(env,GOMAXPROCS='1',SCANNER_LISTEN_ADDR=f'127.0.0.1:{port}',SCANNER_PUBLIC_URL='https://scanner.example.test',SCANNER_AUTH_MODE='guest')
 with (home/'api.log').open('w+b') as api_log,(home/'worker.log').open('w+b') as worker_log:
  api=spawn(binary,['serve'],api_env,api_log,home/'api-usage.json')
  worker=None;poll_stop=threading.Event();poll_thread=None
  result=dict(case)
  try:
   end=time.monotonic()+10
   while True:
    pid,status,api_early=os.wait4(api.pid,os.WNOHANG)
    if pid:
     api.returncode=os.waitstatus_to_exitcode(status);api_log.seek(0);raise RuntimeError('API exited during initialization: '+api_log.read(1024).decode(errors='replace'))
    try:
     if request(port,'/health/ready')[0]==200:break
    except (ConnectionError,OSError):pass
    if time.monotonic()>end:raise RuntimeError('API startup observation timeout')
    time.sleep(.02)
   status,headers,_=request(port,'/api/session');assert status==200
   anonymous=headers['Set-Cookie'].split(';',1)[0]
   status,headers,_=request(port,'/auth/guest','POST',cookie=anonymous);assert status==200
   cookie=headers['Set-Cookie'].split(';',1)[0]
   with sqlite3.connect(state/'state.sqlite3') as db:owner=db.execute("SELECT id FROM principals WHERE kind='guest' AND disabled_at_ms IS NULL").fetchone()[0]
   document='document_resource_01';stamp=int(time.time()*1000)
   inputs=[]
   # Each page is an actual private file (not a hardlink or sparse fixture).
   for index in range(case['pages']):
    path=documents/(f'original_{index:03d}.'+case['source']);width,height=fixture_image(path,case,index)
    with path.open('rb') as original:digest=hashlib.file_digest(original,'sha256').digest()
    inputs.append((path,path.stat().st_size,digest))
   result.update(width=width,height=height,pixels_per_page=width*height,input_bytes=sum(v[1] for v in inputs),input_sha256=[v[2].hex() for v in inputs])
   with sqlite3.connect(documents/'documents.sqlite3') as db:
    db.execute('PRAGMA foreign_keys=ON');db.execute('INSERT INTO owners(id) VALUES(?)',(owner,))
    db.execute("INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES(?,?,'document','review',?,?)",(document,owner,stamp,stamp+3600000))
    for index,(path,size,digest) in enumerate(inputs):
     artifact=f'artifact_resource_{index:03d}';page=f'page_resource_{index:03d}'
     db.execute("INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES(?,?,?,'original','ready',?,?,?,?,1,?)",(artifact,document,owner,path.name,'image/png' if case['source']=='png' else 'image/jpeg',size,digest,stamp))
     db.execute("INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,capture_size,capture_color,capture_dpi) VALUES(?,?,?,?,?,'a4',?,?)",(page,document,owner,index,artifact,case['color'],case['dpi']))
   status,_,admission=request(port,f'/api/documents/{document}/render','POST',{'format':'pdf','pdf_encoding':case['encoding'],'ocr':{'enabled':False}},cookie,{'If-Match':'"1"'})
   result['admission_status']=status
   assert status==202,('render admission rejected',status,admission)
   job=admission['id']
   with sqlite3.connect(documents/'documents.sqlite3') as db:result['reserved_bytes']=db.execute('SELECT byte_size FROM reservations WHERE job_id=?',(job,)).fetchone()[0]
   baseline=volume_size(documents);result['volume_before_bytes'],result['volume_before_allocated_bytes']=baseline
   api_samples=[];api_errors=[];api_observations=[];observation_zero=time.monotonic()
   def poll_api():
    while not poll_stop.is_set():
     started=time.monotonic()
     try:
      code,_,view=request(port,'/api/jobs/'+job,cookie=cookie)
      if code!=200:api_errors.append(code)
      latency=(time.monotonic()-started)*1000;api_samples.append(latency)
      api_observations.append(dict(elapsed_seconds=started-observation_zero,latency_ms=latency,status=code,job_state=view.get('state') if isinstance(view,dict) else None))
     except (ConnectionError,OSError,ValueError) as error:api_errors.append(type(error).__name__)
     poll_stop.wait(.1)
   poll_thread=threading.Thread(target=poll_api);poll_thread.start()
   started=time.monotonic();worker=spawn(binary,['worker','--once'],worker_env,worker_log,home/'worker-usage.json')
   peak_logical,peak_allocated=baseline;api_peak_rss=0;worker_sample_peak=0;termination=None
   while True:
    pid,status,usage=os.wait4(worker.pid,os.WNOHANG)
    if pid:worker.returncode=os.waitstatus_to_exitcode(status);usage=command_usage(worker);break
    worker_sample_peak=max(worker_sample_peak,rss(worker.target_pid) if worker.target_pid else 0);api_peak_rss=max(api_peak_rss,rss(api.target_pid) if api.target_pid else 0)
    logical,allocated=volume_size(documents);peak_logical=max(peak_logical,logical);peak_allocated=max(peak_allocated,allocated)
    if worker_sample_peak>args.rss_ceiling_mib*1024*1024:termination='observer_rss_ceiling'
    if time.monotonic()-started>args.timeout:termination='observer_timeout'
    if termination:usage=stop(worker);break
    time.sleep(.02)
   result.update(worker_exit_code=worker.returncode,worker_elapsed_seconds=time.monotonic()-started,observer_termination=termination,worker_sample_peak_rss_bytes=worker_sample_peak,**usage)
   poll_stop.set();poll_thread.join();poll_thread=None
   final=volume_size(documents);peak_logical=max(peak_logical,final[0]);peak_allocated=max(peak_allocated,final[1])
   ordered=sorted(api_samples)
   result.update(api_sample_count=len(ordered),api_status_p95_ms=ordered[math.ceil(.95*len(ordered))-1] if ordered else None,api_status_max_ms=max(ordered) if ordered else None,api_poll_errors=api_errors,api_status_observations=api_observations,api_sample_peak_rss_bytes=api_peak_rss,volume_sample_peak_bytes=peak_logical,volume_sample_peak_allocated_bytes=peak_allocated,volume_after_bytes=final[0],volume_sample_growth_bytes=peak_logical-baseline[0])
   with sqlite3.connect(documents/'documents.sqlite3') as db:
    result['job_state']=db.execute('SELECT state FROM jobs WHERE id=?',(job,)).fetchone()[0]
    result['remaining_reservations']=db.execute('SELECT count(*) FROM reservations WHERE job_id=?',(job,)).fetchone()[0]
    outputs=db.execute("SELECT relative_path,byte_size,sha256,kind FROM artifacts WHERE document_id=? AND kind!='original'",(document,)).fetchall()
   result['output_bytes']=sum(v[1] for v in outputs if v[3]=='output');result['thumbnail_count']=sum(v[3]=='thumbnail' for v in outputs)
   for path,size,digest,kind in outputs:
    with (documents/path).open('rb') as source:assert hashlib.file_digest(source,'sha256').digest()==digest
    assert (documents/path).stat().st_size==size
    if kind=='output':
     with pikepdf.open(documents/path) as pdf:assert len(pdf.pages)==case['pages']
    if kind=='thumbnail':
     with Image.open(documents/path) as image:assert image.format=='JPEG' and max(image.size)<=512
   for path,size,digest in inputs:
    with path.open('rb') as source:assert hashlib.file_digest(source,'sha256').digest()==digest
   result['candidate_observations']=dict(worker_peak_within_1536_mib=max(result['peak_rss_bytes'],worker_sample_peak)<=1536*1024*1024,sampled_status_p95_within_500_ms=bool(ordered) and result['api_status_p95_ms']<=500)
   result['passed']=termination is None and worker.returncode==0 and result['job_state']==case['expected'] and result['remaining_reservations']==0 and not api_errors
   if case['expected']=='succeeded':result['passed']=result['passed'] and result['thumbnail_count']==case['pages'] and result['output_bytes']>0
   else:result['passed']=result['passed'] and not outputs
   if not result['passed']:
    worker_log.seek(0);result['synthetic_worker_diagnostic']=worker_log.read(2048).decode(errors='replace')
  finally:
   poll_stop.set()
   if poll_thread:poll_thread.join(timeout=4)
   if worker and worker.returncode is None:stop(worker)
   usage=stop(api)
   if usage:
    result['api_lifetime_usage']=usage
    if 'candidate_observations' in result:result['candidate_observations']['api_peak_within_512_mib']=max(usage['peak_rss_bytes'],result['api_sample_peak_rss_bytes'])<=512*1024*1024
   if api.returncode not in [None,0]:
    api_log.seek(0);result['synthetic_api_diagnostic']=api_log.read(2048).decode(errors='replace')
  return result

receipt=dict(pass_meaning='Measurement execution and expected functional results only; not acceptance of performance/resource budgets',timestamp_utc=time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),passed=False,source_sha256=before,cases=[],environment=dict(kernel=platform.release(),machine=platform.machine(),cpu_count=os.cpu_count(),memory_limit=Path('/sys/fs/cgroup/memory.max').read_text().strip(),cpu_quota=Path('/sys/fs/cgroup/cpu.max').read_text().strip(),gomaxprocs=dict(api=1,worker=2),worker_gomemlimit=args.gomemlimit,api_gomemlimit=None,observer_rss_ceiling_bytes=args.rss_ceiling_mib*1024*1024,observer_timeout_seconds=args.timeout,python=platform.python_version(),packages={p:importlib.metadata.version(p) for p in ['pillow','numpy','pikepdf']}),scope='Local release-mode compiled API/worker; synthetic retained A4-equivalent images; real HTTP admission/session/status, SQLite, PDF/thumbnail output. No race instrumentation, container/RSS hard limit, CPU quota, scanner/capture, OCR, mobile, real IdP or production acceptance. Disk/RSS observations are sampled; worker/API peak RSS/CPU uses GNU time measuring its own native-forked command (avoids inherited Python pre-exec RSS). Controller fixture/image/verification cost is excluded.')
try:
 with tempfile.TemporaryDirectory(prefix='scanner-resources-') as temporary:
  work=Path(temporary);binary=work/'scanner'
  subprocess.run(['go','build','-o',str(binary),'./cmd/scanner'],cwd=root,check=True,capture_output=True)
  receipt['native_time_version']=subprocess.run(['/usr/bin/time','--version'],check=True,capture_output=True,text=True).stdout.splitlines()[0]
  receipt['binary_sha256']=hashlib.sha256(binary.read_bytes()).hexdigest()
  receipt['go_version']=subprocess.run(['go','version'],cwd=root,check=True,capture_output=True,text=True).stdout.strip()
  for case in cases:
   result=measure_case(binary,work,case);receipt['cases'].append(result)
   print(json.dumps({k:result.get(k) for k in ['name','passed','job_state','peak_rss_bytes','worker_elapsed_seconds','api_status_p95_ms','observer_termination']}),flush=True)
   args.receipt.write_text(json.dumps(receipt,indent=2)+'\n')
 receipt['source_unchanged_during_checks']=before==hashes()
 receipt['passed']=bool(receipt['cases']) and all(c['passed'] for c in receipt['cases']) and receipt['source_unchanged_during_checks']
except Exception as error:
 receipt['verification_error']=str(error)
finally:
 args.receipt.write_text(json.dumps(receipt,indent=2)+'\n')
print(json.dumps({'passed':receipt['passed'],'cases':len(receipt['cases']),'receipt':str(args.receipt)}))
raise SystemExit(0 if receipt['passed'] else 1)
