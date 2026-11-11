from pathlib import Path
import ast,json,hashlib,math
import numpy as np
repo=Path('/home/yamato/otama-homelab/scanner-kiosk');root=Path('/tmp/scanner-fairscan-numeric.3LsDmU');out=repo/'docs/design/research/correction-fairscan-numeric-v060';bindings=[]
def statement(path,target,contains=None):
 text=path.read_text();tree=ast.parse(text)
 for node in ast.walk(tree):
  if isinstance(node,ast.Assign) and any(isinstance(t,ast.Name) and t.id==target for t in node.targets):
   segment=ast.get_source_segment(text,node)
   if contains is None or contains in segment:
    bindings.append({'source':str(path.relative_to(repo)),'source_sha256':hashlib.sha256(path.read_bytes()).hexdigest(),'line':node.lineno,'exact_statement':segment})
    return compile(ast.Module(body=[node],type_ignores=[]),str(path),'exec')
 raise RuntimeError(target)
syn=repo/'docs/design/research/correction-detection-v060/measure.py';real=repo/'docs/design/research/correction-validation-v060/docaligner-metrics.py'
syn_gt=statement(syn,'gt','w-1');syn_error=statement(syn,'err','q-gt');real_gt=statement(real,'corners','np.stack');real_error=statement(real,'err','q-corners')
# Copy unchanged result expressions too, including the preregistered synthetic2% gate.
def dict_value(path,key):
 text=path.read_text()
 for node in ast.walk(ast.parse(text)):
  if isinstance(node,ast.Dict):
   for k,v in zip(node.keys,node.values):
    if isinstance(k,ast.Constant) and k.value==key:
     bindings.append({'source':str(path.relative_to(repo)),'source_sha256':hashlib.sha256(path.read_bytes()).hexdigest(),'line':v.lineno,'exact_expression':ast.get_source_segment(text,v)})
     return compile(ast.Expression(v),str(path),'eval')
 raise RuntimeError(key)
max_expr=dict_value(syn,'max_corner_error_diagonal');pass_expr=dict_value(syn,'corner_pass');mean_expr=dict_value(real,'corner_mean_error_fraction_diagonal')
def synthetic_errors(q,w,h):
 context={'np':np,'math':math,'q':q,'w':w,'ht':h};exec(syn_gt,context);exec(syn_error,context)
 return context['gt'],context['err'],float(eval(max_expr,context)),bool(eval(pass_expr,context))
selfq=np.array([[0,0],[899,0],[899,699],[0,699]])
assert synthetic_errors(selfq,900,700)[2:]==(0.0,True)
assert synthetic_errors(selfq+100,900,700)[3] is False
rows=[];raw=json.loads((root/'synthetic-coordinates.json').read_text())['rows']
for r in raw:
 w,h=r['width'],r['height'];ctx={'np':np,'w':w,'ht':h};exec(syn_gt,ctx);gt=ctx['gt'];_,noop_err,noop_max,noop_pass=synthetic_errors(gt,w,h)
 d={'id':r['id'],'width':w,'height':h,'truth':gt.tolist(),'quad':r['quad'],'quad_present':r['quad'] is not None,'internal_fallback':r['internal_fallback'],'no_op_max_corner_error_diagonal':noop_max,'no_op_corner_pass':noop_pass}
 if r['quad'] is not None:
  _,err,maxerr,passed=synthetic_errors(np.array(r['quad']),w,h);d.update(corner_errors_diagonal=err.tolist(),max_corner_error_diagonal=maxerr,corner_pass=passed)
 else:d.update(corner_errors_diagonal=None,max_corner_error_diagonal=None,corner_pass=False,disposition='no detection; no fallback substituted')
 rows.append(d)
realrows=[];truth=json.loads((out/'real-truth-bindings.json').read_text())['rows'];byid={r['id']:r for r in truth}
for r in json.loads((root/'real-coordinates.json').read_text())['rows']:
 t=byid[r['id']];g=np.load(t['grid_npy']);assert hashlib.sha256(Path(t['grid_npy']).read_bytes()).hexdigest()==t['grid_npy_sha256'];w,h=r['width'],r['height'];ctx={'np':np,'math':math,'g':g,'w':w,'h':h};exec(real_gt,ctx);corners=ctx['corners']
 oldcat=Path('/tmp/scanner-v060')/('docaligner-truth-'+r['id'].removeprefix('real_'))/'repetitions/001-cold/artifact-catalog.json';c=json.loads(oldcat.read_text());assert c['fixture_sha256']==t['source_sha256'];a=c['artifacts'][0];assert a['sha256']==t['source_sha256'] and a['parameters']['fallback_reason']=='unsafe-or-missing-corners';noop=np.array(json.loads(a['parameters']['crop_quad']));ctx['q']=noop;exec(real_error,ctx);nooperrs=ctx['err'];noopmean=float(eval(mean_expr,ctx))
 d={'id':r['id'],'width':w,'height':h,'truth_grid_endpoints':corners.tolist(),'quad':r['quad'],'quad_present':r['quad'] is not None,'internal_fallback':r['internal_fallback'],'no_op_quad':noop.tolist(),'no_op_corner_errors_diagonal':nooperrs.tolist(),'no_op_mean_corner_error_diagonal':noopmean,'no_op_binding':{'path':str(oldcat),'sha256':hashlib.sha256(oldcat.read_bytes()).hexdigest()},'quality_pass':'not defined for these additional diagnostics','scope':t['corner_scope']}
 if r['quad'] is not None:
  ctx['q']=np.array(r['quad']);exec(real_error,ctx);err=ctx['err'];d.update(corner_errors_diagonal=err.tolist(),mean_corner_error_diagonal=float(eval(mean_expr,ctx)),max_corner_error_diagonal=float(err.max()))
 else:d.update(corner_errors_diagonal=None,mean_corner_error_diagonal=None,max_corner_error_diagonal=None,disposition='no detection; no fallback substituted')
 # Same unchanged real metric returns zero when supplied quad equals its established GT endpoints.
 ctx['q']=corners;exec(real_error,ctx);assert np.all(ctx['err']==0)
 realrows.append(d)
assert len(rows)==6 and len(realrows)==3
result={'synthetic_rows':rows,'synthetic_summary':{'quad_present':sum(r['quad_present'] for r in rows),'quad_null':sum(not r['quad_present'] for r in rows),'corner_pass':sum(r['corner_pass'] for r in rows),'corner_fail_or_missing':sum(not r['corner_pass'] for r in rows),'no_op_corner_pass':sum(r['no_op_corner_pass'] for r in rows),'threshold_max_corner_error_diagonal':0.02},'real_additional_rows':realrows,'real_summary':{'quad_present':sum(r['quad_present'] for r in realrows),'quad_null':sum(not r['quad_present'] for r in realrows),'original9_overlap':0,'quality_gate':'no new threshold for additional diagnostic3'},'existing_metric_bindings':bindings,'selfchecks':'exactGTerror0 and syntheticlargeoffsetreject; realexactgridendpoint0'}
(out/'metrics.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps(result['synthetic_summary']));print(json.dumps(result['real_summary']))
