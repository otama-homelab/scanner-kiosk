from pathlib import Path
import json,hashlib,shutil,subprocess
import numpy as np
from PIL import Image
p=Path('/tmp/scanner-v060-paired');(p/'evaluation').mkdir(exist_ok=True);(p/'clean').mkdir(exist_ok=True);(p/'shadow').mkdir(exist_ok=True);root=Path.cwd();h=lambda p:hashlib.sha256(p.read_bytes()).hexdigest();fixture=lambda key,source,variant:{'id':key,'source_class':'public_synthetic','privacy':'public-synthetic','media_type':'image/png','page_ids':['page_'+key.removeprefix('fixture_')],'languages':[],'document_type':'synthetic-shadow-pair','expected_outputs':['correction'],'recipe':{'kind':'pattern','source':source,'variant':variant}}
for name in ['mixed','table']:shutil.copyfile(root/f'scripts/fixtures/ocr/page_ocr_{name}_001.png',p/'clean'/f'{name}.png')
for name in ['blue','peach']:shutil.copyfile(root/f'scripts/fixtures/generated-correction/reference-{name}.png',p/'clean'/f'{name}.png')
cat={'format':'scanner-eval-fixtures','version':1,'fixtures':[fixture('fixture_pair_faint_'+n,'clean/'+n+'.png','faint') for n in ['mixed','table']]};f=p/'evaluation/faint.json';f.write_text(json.dumps(cat))
for n in ['mixed','table']:subprocess.run([str(p/'materialize'),str(f),'fixture_pair_faint_'+n,str(p/'clean'/('faint_'+n+'.png'))],check=True,capture_output=True)
names=['mixed','table','faint_mixed','faint_table','blue','peach'];cat['fixtures']=[fixture('fixture_pair_shadow_'+n,'clean/'+n+'.png','shadow') for n in names];f=p/'evaluation/shadow.json';f.write_text(json.dumps(cat));rows=[]
for n in names:
 out=p/'shadow'/(n+'.png');cmd=[str(p/'materialize'),str(f),'fixture_pair_shadow_'+n,str(out)];subprocess.run(cmd,check=True,capture_output=True);a=np.asarray(Image.open(p/'clean'/(n+'.png')).convert('RGB'),dtype=np.int16);b=np.asarray(Image.open(out).convert('RGB'),dtype=np.int16);assert a.shape==b.shape and np.all(b<=a) and np.any(b<a)
 # Repeated generation must be byte-identical; no random recipe or model-driven truth.
 before=h(out);subprocess.run(cmd,check=True,capture_output=True);assert h(out)==before
 attributes=(['thin-text','digits','fine-lines'] if n.startswith('faint_') else ['digits'] if n=='mixed' else ['digits','fine-lines'] if n=='table' else ['photograph','colored-paper','stamp'])
 rows.append({'id':n,'clean':str(p/'clean'/(n+'.png')),'clean_sha256':h(p/'clean'/(n+'.png')),'shadow':str(out),'shadow_sha256':h(out),'size':list(Image.open(out).size),'attributes':attributes,'ground_truth':'exact reference raster independent of evaluated correction model; synthetic only','generator':'existing evaluation.ResolveFixture pattern/shadow (no coordinate change)','deterministic_replay':True})
coverage={k:sum(k in r['attributes'] for r in rows) for k in ['thin-text','digits','photograph','colored-paper','stamp']};assert all(v>=2 for v in coverage.values());assert len(rows)==6
manifest={'authorization':'Owner accepts generated images; existing deterministic shadow recipe reused unchanged; no new shadow/correction algorithm.','rows':rows,'clean_control_ids':names,'coverage':coverage,'coverage_scope':'synthetic controls, imagegen visual elements are not human transcriptions; digits/glyphs from fixed renderer','generator_sha256':h(root/'internal/evaluation/catalog.go'),'before_new_correction_inference':True,'limits':{'threads':2,'timeout_seconds':30,'product_rss_mib':1024,'diagnostic_rss_mib':2048,'temp_mib':256,'installed_package_mib':256},'thresholds':{'shadow_psnr_improvement_db':1,'clean_ssim_min':0.99,'clean_channel_mae_max':1/255},'model_eligibility':'No unknown-license FSENet/SyntheticDoc checkpoint will execute; truth preparation independent of model eligibility.'};(p/'paired-truth.json').write_text(json.dumps(manifest,indent=2)+'\n')
print(json.dumps({'pairs':len(rows),'clean_controls':len(names),'coverage':coverage}))
