from pathlib import Path
import json,subprocess,os,hashlib,unicodedata,time,signal
root=Path('/tmp/scanner-fairscan-relative.csJ1VV');repo=Path('/home/yamato/otama-homelab/scanner-kiosk');out=repo/'docs/design/research/correction-fairscan-relative-v060';pre=json.loads(Path('/tmp/scanner-v060-synthetic/preregistration.json').read_text());cat=json.loads(Path('/tmp/scanner-v060-synthetic/catalog.json').read_text());runtime=Path('/home/yamato/.local/share/scanner46-ocr-85v1b2rc');binary=runtime/'full-native/usr/bin/tesseract';loader=runtime/'full-native/lib/ld-musl-x86_64.so.1';library=runtime/'full-native/usr/lib';tessdata=runtime/'tessdata';analyzer=Path('/tmp/scanner-v060-synthetic/ocr-analyze');h=lambda p:hashlib.sha256(p.read_bytes()).hexdigest();assert h(binary)=='5cc985582023a51d1ecd15bba88bf5b531607f60a4a68c4bf2b43344fcea3b66';rows=[];requests=[]
for f in cat['fixtures']:
 truth=next(o['pages'][0] for o in pre['rows'] if o['fixture_id']==f['id']);expected=''.join(s['text'] for s in sorted(truth['spans'],key=lambda s:s['order']))
 for arm,suffix in [('no_correction_matched_jpeg','input'),('official_dataset_output','output')]:
  image=root/'source/evaluation/reports/results'/(f['id']+'_'+suffix+'.jpg');assert image.is_file();cmd=['taskset','-c',','.join(map(str,sorted(os.sched_getaffinity(0))[:2])),str(loader),'--library-path',str(library),str(binary),str(image),'stdout','--tessdata-dir',str(tessdata),'-l','jpn+eng','--psm','3'];env=dict(os.environ,OMP_THREAD_LIMIT='2');start=time.monotonic();peak=0;reason=None
  p=subprocess.Popen(cmd,env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE,start_new_session=True)
  # Native OCR emits short text; collect only after the bounded process ends.
  while p.poll() is None:
   rss=0
   for proc in Path('/proc').iterdir():
    if proc.name.isdigit():
     try:
      stat=(proc/'stat').read_text().split()
      if int(stat[5])==p.pid:rss+=int(stat[23])*os.sysconf('SC_PAGE_SIZE')
     except (OSError,ValueError,IndexError):pass
   peak=max(peak,rss)
   if time.monotonic()-start>30:reason='timeout30s'
   elif rss>2*1024**3:reason='rss2GiB'
   if reason:os.killpg(p.pid,signal.SIGKILL);break
   time.sleep(.02)
  stdout,stderr=p.communicate();rc=p.returncode;dest=out/(f['id']+'-'+suffix+'.txt');dest.write_bytes(stdout);(out/(f['id']+'-'+suffix+'.stderr')).write_bytes(stderr)
  row={'id':f['id'],'arm':arm,'input_sha256':h(image),'source_sha256':f['sha256'],'oracle_source':'/tmp/scanner-v060-synthetic/preregistration.json','reference_text':expected,'ocr_text':stdout.decode('utf-8'),'ocr_text_sha256':h(dest),'exit_code':rc,'stop_reason':reason,'elapsed_s':time.monotonic()-start,'peak_rss_bytes':peak,'command':cmd,'digit_sequence_expected':''.join(x for x in expected if x.isdecimal()),'digit_sequence_actual':''.join(x for x in unicodedata.normalize('NFC',stdout.decode('utf-8')) if x.isdecimal())};row['digits_exact']=row['digit_sequence_expected']==row['digit_sequence_actual'];rows.append(row);requests.append({'oracle':truth,'text':row['ocr_text']})
  assert rc==0,(f['id'],arm,stderr)
r=subprocess.run([str(analyzer)],input=json.dumps(requests,ensure_ascii=False),text=True,capture_output=True,check=True,timeout=30);qualities=json.loads(r.stdout);assert len(rows)==len(qualities)==12
for row,q in zip(rows,qualities):row.update(nfc_text_errors=q['text_errors'],reference_code_points=q['reference_code_points'],nfc_cer=q['text_errors']/q['reference_code_points'])
o=pre['rows'][0]['pages'][0];s=''.join(t['text'] for t in o['spans']);check=subprocess.run([str(analyzer)],input=json.dumps([{'oracle':o,'text':s},{'oracle':o,'text':'X'+s[1:]}],ensure_ascii=False),text=True,capture_output=True,check=True,timeout=30);z=json.loads(check.stdout);assert z[0]['text_errors']==0 and z[1]['text_errors']==1
pins={'engine':'Tesseract5.5.4 jpn+eng psm3 threads2','binary_sha256':h(binary),'loader_sha256':h(loader),'eng_sha256':h(tessdata/'eng.traineddata'),'jpn_sha256':h(tessdata/'jpn.traineddata'),'analyzer_sha256':h(analyzer),'oracle_sha256':h(Path('/tmp/scanner-v060-synthetic/preregistration.json')),'scorer':'unchanged evaluation.AnalyzeOCRPage strictNFC includingwhitespace; no positionedspans so region/status verdicts unused'}
(out/'ocr.json').write_text(json.dumps({'rows':rows,'pins':pins,'selfchecks':'existing scorer exact0/substitution1'},ensure_ascii=False,indent=2)+'\n');print('Twelve paired OCR jobs/scorer success')
