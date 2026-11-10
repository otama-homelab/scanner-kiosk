from pathlib import Path
import json,subprocess,os,hashlib,unicodedata
import numpy as np
from PIL import Image
p=Path('/tmp/scanner-v060-synthetic');pre=json.loads((p/'preregistration.json').read_text());cat=json.loads((p/'catalog.json').read_text());h=lambda p:hashlib.sha256(p.read_bytes()).hexdigest();runtime=Path('/home/yamato/.local/share/scanner46-ocr-85v1b2rc');binary=runtime/'full-native/usr/bin/tesseract';loader=runtime/'full-native/lib/ld-musl-x86_64.so.1';library=runtime/'full-native/usr/lib';tessdata=runtime/'tessdata';assert h(binary)=='5cc985582023a51d1ecd15bba88bf5b531607f60a4a68c4bf2b43344fcea3b66';rows=[];ocrinputs=[]
for arm in ['none','docaligner','old-onnx','mesh']:
 for f in cat['fixtures']:
  source=p/'corpus'/f['relative_path'];out=p/(arm+'-'+f['id']);b=json.loads((out/'benchmark.json').read_text());cf=out/'repetitions/001-cold/artifact-catalog.json';c=json.loads(cf.read_text());a=next(x for x in c['artifacts'] if x['role']=='correction');image=cf.parent/'artifacts'/a['id'];assert h(source)==f['sha256'] and h(image)==a['sha256'];truth=next(o['pages'][0] for o in pre['rows'] if o['fixture_id']==f['id']);expected=''.join(s['text'] for s in sorted(truth['spans'],key=lambda s:s['order']));dest=p/'ocr-work'/(arm+'-'+f['id']+'.txt')
  r=subprocess.run([str(loader),'--library-path',str(library),str(binary),str(image),'stdout','--tessdata-dir',str(tessdata),'-l','jpn+eng','--psm','3'],env=dict(os.environ,OMP_THREAD_LIMIT='2'),capture_output=True,timeout=30);assert r.returncode==0;dest.write_bytes(r.stdout);text=r.stdout.decode('utf-8');orig=np.asarray(Image.open(source).convert('RGB'),dtype=np.int16);actual=np.asarray(Image.open(image).convert('RGB'),dtype=np.int16)
  row={'arm':arm,'fixture_id':f['id'],'source_sha256':f['sha256'],'output_sha256':a['sha256'],'output_byte_identical':h(source)==h(image),'fallback_reason':a['parameters'].get('fallback_reason',''),'output_size':list(Image.open(image).size),'reference_size':[truth['width'],truth['height']],'process_resources':{'samples':len(b['samples']),'peak_rss_bytes':max(s['peak_rss_bytes'] for s in b['samples']),'max_wall_millis':max(s['wall_millis'] for s in b['samples']),'all_complete':all(s['status']=='complete' for s in b['samples'])},'reference_text':expected,'ocr_text':text,'ocr_text_sha256':h(dest),'digit_sequence_expected':''.join(x for x in expected if x.isdecimal()),'digit_sequence_actual':''.join(x for x in unicodedata.normalize('NFC',text) if x.isdecimal()),'pixel_alignment':'fixed renderer coordinates, no realignment/custom warp'}
  row['digits_exact']=row['digit_sequence_expected']==row['digit_sequence_actual']
  if actual.shape==orig.shape:
   row['channel_mae']=float(np.abs(actual-orig).mean()/255);row['clean_mae_pass']=row['channel_mae']<=1/255
   if f['id']=='fixture_ocr_table_001':
    # Existing renderer draws2px black segments exactly at these source coordinates.
    mask=np.zeros(orig.shape[:2],dtype=bool)
    for x in [70,420,800]:mask[70:341,x:x+2]=True
    for y in [70,200,340]:mask[y:y+2,70:801]=True
    mask &= (orig.max(axis=2)<128);assert mask.sum()>0
    row['table_line_source_dark_pixels']=int(mask.sum());row['table_line_retained_dark_pixels']=int((actual.max(axis=2)[mask]<128).sum());row['table_line_fixed_position_survival']=row['table_line_retained_dark_pixels']/row['table_line_source_dark_pixels'];row['table_line_pass']=row['table_line_fixed_position_survival']==1
  else:row['clean_mae_pass']=False;row['table_line_pass']=False if f['id']=='fixture_ocr_table_001' else None
  rows.append(row);ocrinputs.append({'oracle':truth,'text':text})
r=subprocess.run([str(p/'ocr-analyze')],input=json.dumps(ocrinputs,ensure_ascii=False),text=True,capture_output=True,check=True);q=json.loads(r.stdout);assert len(q)==len(rows)==24
for row,v in zip(rows,q):row['nfc_text_errors']=v['text_errors'];row['reference_code_points']=v['reference_code_points'];row['nfc_cer']=v['text_errors']/v['reference_code_points'];row['ocr_scope']='Existing AnalyzeOCRPage strict NFC text distance; no positioned spans provided, so matched-region/cell/status verdicts are not used.'
# Self-check the reused analyzer with an exact string and one substituted character.
o=pre['rows'][0]['pages'][0];s=''.join(t['text'] for t in o['spans']);test=[{'oracle':o,'text':s},{'oracle':o,'text':'X'+s[1:]}];r=subprocess.run([str(p/'ocr-analyze')],input=json.dumps(test,ensure_ascii=False),capture_output=True,text=True,check=True);z=json.loads(r.stdout);assert z[0]['text_errors']==0 and z[1]['text_errors']==1
pins={'binary_sha256':h(binary),'loader_sha256':h(loader),'prior_onefile_probe':'bundled runtime unavailable or changed; preserved, wrapper not repaired; reused existing extracted native binary via its loader','eng_sha256':h(tessdata/'eng.traineddata'),'jpn_sha256':h(tessdata/'jpn.traineddata'),'engine':'Tesseract5.5.4 jpn+eng psm3 threads2','oracle_analyzer':'existing evaluation.AnalyzeOCRPage, NFC, all Unicode code points including whitespace; no text cleanup','selfchecks':'exact and single substitution pass'}
(p/'quality.json').write_text(json.dumps({'scope':'synthetic clean controls only, not real photo or shadow qualification','rows':rows,'ocr':pins},ensure_ascii=False,indent=2)+'\n');assert len(rows)==24
print(json.dumps([{'arm':a,'cases':sum(r['arm']==a for r in rows),'mae_pass':sum(r['clean_mae_pass'] for r in rows if r['arm']==a),'digit_exact_cases':sum(r['digits_exact'] for r in rows if r['arm']==a),'digit_bearing_cases':sum(bool(r['digit_sequence_expected']) for r in rows if r['arm']==a)} for a in ['none','docaligner','old-onnx','mesh']],indent=2))
