from pathlib import Path
import json,hashlib
repo=Path('/home/yamato/otama-homelab/scanner-kiosk');out=repo/'docs/design/research/correction-fairscan-relative-v060';p=repo/'docs/design/research/correction-fairscan-numeric-v060/metrics.json';d=json.loads(p.read_text());rows=[]
for kind,items in [('synthetic',d['synthetic_rows']),('additional_grid_endpoints',d['real_additional_rows'])]:
 for r in items:
  noop_errors=r.get('no_op_corner_errors_diagonal')
  # Synthetic no-op error is exactly zero at all four known renderer corners.
  noop_mean=sum(noop_errors)/len(noop_errors) if noop_errors else 0.0
  noop_max=max(noop_errors) if noop_errors else r['no_op_max_corner_error_diagonal']
  q=r['quad'];base={'id':r['id'],'stratum':kind,'quad_present':q is not None,'no_op_mean':noop_mean,'no_op_max':noop_max,'internal_fallback':r['internal_fallback']}
  if q is None:base.update(disposition='no_quad',candidate_mean=None,candidate_max=None,paired_mean_change=None,paired_max_change=None)
  else:
   mean=sum(r['corner_errors_diagonal'])/len(r['corner_errors_diagonal']);mx=r['max_corner_error_diagonal'];dm=mean-noop_mean;dx=mx-noop_max
   status='improved_both' if dm<0 and dx<0 else 'worse_both' if dm>0 and dx>0 else 'equal_both' if dm==0 and dx==0 else 'tradeoff'
   base.update(disposition=status,candidate_mean=mean,candidate_max=mx,paired_mean_change=dm,paired_max_change=dx)
  rows.append(base)
assert len(rows)==9 and sum(r['quad_present'] for r in rows)==5 and sum(r['disposition']=='no_quad' for r in rows)==4
summaries=[]
for stratum,total in [('synthetic',6),('additional_grid_endpoints',3)]:
 r=[r for r in rows if r['stratum']==stratum];assert len(r)==total;counts={key:sum(x['disposition']==key for x in r) for key in ['improved_both','worse_both','tradeoff','equal_both','no_quad']};assert sum(counts.values())==total;summaries.append({'stratum':stratum,'denominator':total,'counts':counts,'detected_only_aggregate':'not reported as overall performance'})
res={'source_metrics':str(p.relative_to(repo)),'source_sha256':hashlib.sha256(p.read_bytes()).hexdigest(),'historical_2percent_max_corner_gate':d['synthetic_summary'],'interpretation':'historical pilot cutoff retained; not proven user usability boundary; no replacement pass threshold','rows':rows,'strata':summaries,'pooled_accuracy':'not calculated','preview_only_qualified':False,'preview_gate_reasons':['synthetic detected cases worsen geometry','content region/line retention not measured with a compatible map','original real6/photo2_2 content truth missing','owner/device acceptance and candidate-bound product regression missing']}
(out/'relative.json').write_text(json.dumps(res,indent=2)+'\n');print(json.dumps(summaries))
