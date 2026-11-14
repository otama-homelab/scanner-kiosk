from pathlib import Path
import ast, hashlib, json, math, shutil
import numpy as np
repo=Path.cwd(); ops=repo/'.planning/ops/photo-auto-correction-20261114'; out=repo/'docs/design/research/correction-scanic-20261114'; fixed=Path('/tmp/scanner-v060-synthetic')
sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
metric=repo/'docs/design/research/correction-detection-v060/measure.py'; text=metric.read_text(); tree=ast.parse(text); bindings=[]
def statement(target, contains):
 for node in ast.walk(tree):
  if isinstance(node,ast.Assign) and any(isinstance(n,ast.Name) and n.id==target for n in node.targets):
   source=ast.get_source_segment(text,node)
   if contains in source:
    bindings.append({'path':str(metric.relative_to(repo)),'sha256':sha(metric),'line':node.lineno,'statement':source})
    return compile(ast.Module(body=[node],type_ignores=[]),str(metric),'exec')
 raise AssertionError(target)
gtcode=statement('gt','w-1'); errcode=statement('err','q-gt')
def expression(key):
 for node in ast.walk(tree):
  if isinstance(node,ast.Dict):
   for k,v in zip(node.keys,node.values):
    if isinstance(k,ast.Constant) and k.value==key:
     bindings.append({'path':str(metric.relative_to(repo)),'sha256':sha(metric),'line':v.lineno,'expression':ast.get_source_segment(text,v)})
     return compile(ast.Expression(v),str(metric),'eval')
 raise AssertionError(key)
maxcode=expression('max_corner_error_diagonal'); passcode=expression('corner_pass')
def measure(q,w,h):
 ctx={'np':np,'math':math,'q':np.array(q),'w':w,'ht':h};exec(gtcode,ctx);exec(errcode,ctx)
 return ctx['gt'],ctx['err'],float(eval(maxcode,ctx)),bool(eval(passcode,ctx))
zero=[[0,0],[899,0],[899,699],[0,699]];assert measure(zero,900,700)[2:]==(0.0,True);assert measure(np.array(zero)+100,900,700)[3] is False
truth={t['fixture_id']:t for t in json.loads((fixed/'truth-definition.json').read_text())['rows']};cat=json.loads((fixed/'catalog.json').read_text());rows=[]
for f in cat['fixtures']:
 source=fixed/'corpus'/f['relative_path'];assert sha(source)==f['sha256'];t=truth[f['id']];assert t['source_sha256']==f['sha256'];g=t['known_full_page_quad'];w,h=g[2][0]+1,g[2][1]+1
 run=ops/'synthetic-runs'/f['id']; benchmark=json.loads((run/'benchmark.json').read_text()); assert benchmark['status']=='complete' and len(benchmark['samples'])==3
 dest=out/'clean-controls'/f['id'];dest.mkdir(parents=True,exist_ok=True);shutil.copyfile(run/'benchmark.json',dest/'benchmark.json');samples=[]
 for s in benchmark['samples']:
  cp=run/s['artifact_catalog'];c=json.loads(cp.read_text());assert c['fixture_sha256']==f['sha256'];a=c['artifacts'][0];image=cp.parent/'artifacts'/a['id'];assert sha(image)==a['sha256'];p=a['parameters'];q=json.loads(p['crop_quad']);gt,err,mx,passed=measure(q,w,h);assert gt.tolist()==g
  baselines=[]
  for arm in ('none','docaligner'):
   bp=fixed/(arm+'-'+f['id'])/'repetitions'/cp.parent.name/'artifact-catalog.json';bc=json.loads(bp.read_text());assert bc['fixture_sha256']==f['sha256'];ba=bc['artifacts'][0];bi=bp.parent/'artifacts'/ba['id'];assert sha(bi)==ba['sha256'];baselines.append({'arm':arm,'catalog_sha256':sha(bp),'output_sha256':ba['sha256'],'same_pixels_proven_by_exact_bytes':a['sha256']==ba['sha256']})
  samples.append({'sample_id':s['sample_id'],'mode':s['mode'],'status':s['status'],'source_sha256':c['fixture_sha256'],'output_sha256':a['sha256'],'quad':q,'corner_errors_diagonal':err.tolist(),'max_corner_error_diagonal':mx,'corner_pass':passed,'fallback_reason':p['fallback_reason'],'detected':not bool(p['fallback_reason']),'source_byte_identical':a['sha256']==f['sha256'],'wall_millis':s['wall_millis'],'peak_process_rss_bytes':s['peak_rss_bytes'],'baseline_bindings':baselines})
  target=dest/(cp.parent.name+'-artifact-catalog.json');shutil.copyfile(cp,target)
 rows.append({'fixture_id':f['id'],'source_sha256':f['sha256'],'known_full_page_quad':g,'samples':samples})
flat=[s for r in rows for s in r['samples']];assert len(rows)==6 and len(flat)==18
result={'date':'2026-11-14','scope':'Fixed renderer-known clean controls only; not original six photos','candidate':'scanic1.7.1/ml0.2.0','rows':rows,'metric_bindings':bindings,'preregistration_sha256':sha(ops/'synthetic-preregistration.json'),'truth_definition_sha256':sha(fixed/'truth-definition.json'),'summary':{'cases':len(rows),'samples':len(flat),'detected_samples':sum(s['detected'] for s in flat),'fallback_samples':sum(not s['detected'] for s in flat),'byte_identical_samples':sum(s['source_byte_identical'] for s in flat),'corner_pass_samples_including_noop_fallback':sum(s['corner_pass'] for s in flat),'maximum_wall_millis':max(s['wall_millis'] for s in flat),'maximum_process_rss_bytes':max(s['peak_process_rss_bytes'] for s in flat)},'ocr_inferences':0,'ocr_note':'Exact byte identity permits reuse of existing same-input Tesseract result; no OCR improvement inferred. Existing baseline text remains imperfect.','qualification':'not-qualified','reasons':['Existing public UVDoc00000 serious truncation remains','Clean controls all preserve original by explicit fallback; fallback is not detection success','Original-photo human truth, original photo2_2 content proof, owner device operation and full product resource qualification remain missing'],'requirements_newly_completed':0,'selfchecks':'Exact GT -> zero/pass; +100px -> reject; all 18 source and output hashes and same-input no-op/DocAligner hashes verified'}
(out/'clean-controls.json').write_text(json.dumps(result,indent=2)+'\n');shutil.copyfile(ops/'synthetic-preregistration.json',out/'synthetic-preregistration.json');print(json.dumps(result['summary']))
