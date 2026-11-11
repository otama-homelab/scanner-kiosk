from pathlib import Path
import json,subprocess,os,time,signal,hashlib,unicodedata
root=Path('/tmp/scanner-fairscan-parity.43pB9h');repo=Path('/home/yamato/otama-homelab/scanner-kiosk');out=repo/'docs/design/research/correction-fairscan-mask-parity-v060';priorpath=repo/'docs/design/research/correction-fairscan-relative-v060/ocr.json';prior=json.loads(priorpath.read_text());byhash={r['input_sha256']:r for r in prior['rows']};baselines={r['id']:r for r in prior['rows'] if r['arm']=='no_correction_matched_jpeg'};pre=json.loads(Path('/tmp/scanner-v060-synthetic/preregistration.json').read_text());runtime=Path('/home/yamato/.local/share/scanner46-ocr-85v1b2rc');binary=runtime/'full-native/usr/bin/tesseract';loader=runtime/'full-native/lib/ld-musl-x86_64.so.1';library=runtime/'full-native/usr/lib';tessdata=runtime/'tessdata';h=lambda p:hashlib.sha256(p.read_bytes()).hexdigest();assert h(binary)==prior['pins']['binary_sha256'];rows=[];requests=[]
for id,baseline in baselines.items():
 image=root/'source/evaluation/reports/results'/(id+'_output.jpg');inputjpg=image.with_name(id+'_input.jpg');assert h(inputjpg)==baseline['input_sha256'];digest=h(image);truth=next(o['pages'][0] for o in pre['rows'] if o['fixture_id']==id);expected=baseline['reference_text']
 if digest in byhash:
  cached=byhash[digest];text=cached['ocr_text'];row={'id':id,'ocr_reused_by_exact_imagehash':True,'prior_evidence':str(priorpath.relative_to(repo)),'prior_evidence_sha256':h(priorpath),'new_ocr_elapsed_s':0,'peak_rss_bytes':0,'exit_code':cached['exit_code']}
 else:
  cmd=['taskset','-c',','.join(map(str,sorted(os.sched_getaffinity(0))[:2])),str(loader),'--library-path',str(library),str(binary),str(image),'stdout','--tessdata-dir',str(tessdata),'-l','jpn+eng','--psm','3'];start=time.monotonic();peak=0;reason=None;p=subprocess.Popen(cmd,env=dict(os.environ,OMP_THREAD_LIMIT='2'),stdout=subprocess.PIPE,stderr=subprocess.PIPE,start_new_session=True)
  while p.poll() is None:
   rss=0
   for proc in Path('/proc').iterdir():
    if proc.name.isdigit():
     try:
      s=(proc/'stat').read_text().split()
      if int(s[5])==p.pid:rss+=int(s[23])*os.sysconf('SC_PAGE_SIZE')
     except (OSError,ValueError,IndexError):pass
   peak=max(peak,rss)
   if time.monotonic()-start>30:reason='timeout30s'
   elif rss>2*1024**3:reason='rss2GiB'
   if reason:os.killpg(p.pid,signal.SIGKILL);break
   time.sleep(.02)
  stdout,stderr=p.communicate();assert p.returncode==0,(id,stderr);text=stdout.decode();(out/(id+'.txt')).write_bytes(stdout);(out/(id+'.stderr')).write_bytes(stderr);row={'id':id,'ocr_reused_by_exact_imagehash':False,'command':cmd,'new_ocr_elapsed_s':time.monotonic()-start,'peak_rss_bytes':peak,'exit_code':p.returncode,'stop_reason':reason}
 row.update(input_jpeg_sha256=baseline['input_sha256'],candidate_jpeg_sha256=digest,ocr_text=text,baseline_text_errors=baseline['nfc_text_errors'],baseline_cer=baseline['nfc_cer'],reference_text=expected,digit_bearing=bool(baseline['digit_sequence_expected']),digit_sequence_expected=baseline['digit_sequence_expected'],digit_sequence_actual=''.join(x for x in unicodedata.normalize('NFC',text) if x.isdecimal()),baseline_digits_exact=baseline['digits_exact']);row['candidate_digits_exact']=row['digit_sequence_expected']==row['digit_sequence_actual'];rows.append(row);requests.append({'oracle':truth,'text':text})
r=subprocess.run(['/tmp/scanner-v060-synthetic/ocr-analyze'],input=json.dumps(requests,ensure_ascii=False),text=True,capture_output=True,check=True,timeout=30);q=json.loads(r.stdout);assert len(q)==len(rows)==6
for row,v in zip(rows,q):
 delta=v['text_errors']-row['baseline_text_errors'];row.update(candidate_text_errors=v['text_errors'],candidate_cer=v['text_errors']/v['reference_code_points'],text_error_change=delta,disposition='worse' if delta>0 else 'better' if delta<0 else 'equal_error_count')
counts={k:sum(r['disposition']==k for r in rows) for k in ['worse','better','equal_error_count']};assert sum(counts.values())==6
(out/'ocr.json').write_text(json.dumps({'pins':prior['pins'],'rows':rows,'counts':counts,'total_denominator':6,'new_ocr_jobs':sum(not r['ocr_reused_by_exact_imagehash'] for r in rows),'reused_by_hash':sum(r['ocr_reused_by_exact_imagehash'] for r in rows),'region_retention':'unsupported existingbilinearvsprojective mapping; same priorlimitation','exact_android_parity':False},ensure_ascii=False,indent=2)+'\n');print(counts)
