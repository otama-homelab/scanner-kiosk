from pathlib import Path
import ast,json,hashlib,math
import numpy as np
repo=Path('/home/yamato/otama-homelab/scanner-kiosk');root=Path('/tmp/scanner-fairscan-parity.43pB9h');out=repo/'docs/design/research/correction-fairscan-mask-parity-v060';bindings=[]
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

result={'synthetic_rows':rows,'summary':{'total':len(rows),'quad_present':sum(r['quad_present'] for r in rows),'quad_null':sum(not r['quad_present'] for r in rows),'corner_pass':sum(r['corner_pass'] for r in rows),'no_op_corner_pass':sum(r['no_op_corner_pass'] for r in rows),'threshold_max_corner_error_diagonal':0.02},'existing_metric_bindings':bindings};(out/'metrics.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps(result['summary']))
