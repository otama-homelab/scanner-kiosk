#!/usr/bin/env python3
"""Check implemented Go contracts and design examples without deploying them."""
import argparse
from datetime import datetime,timezone
import hashlib,json,os,subprocess,sys
from pathlib import Path

parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--receipt',type=Path,required=True)
parser.add_argument('--validator-python',default=sys.executable)
args=parser.parse_args();root=Path(__file__).resolve().parent.parent
files=[root/'go.mod',root/'go.sum',root/'api/openapi.json',root/'.env.example',Path(__file__).resolve(),*sorted((root/'cmd').rglob('*.go')),*sorted((root/'internal').rglob('*.go')),*sorted((root/'internal').rglob('*.sql')),*sorted((root/'pkg').rglob('*.go')),*sorted((root/'deploy').rglob('*.yaml'))]
hashes=lambda:{str(p.relative_to(root)):hashlib.sha256(p.read_bytes()).hexdigest() for p in files}
before=hashes();checks=[]
def run(name,command,*,environment=None,expected=0,redact_output=False,timeout=180):
    process=subprocess.run(command,cwd=root,capture_output=True,text=True,env=environment,timeout=timeout)
    passed=process.returncode==expected if expected==0 else process.returncode!=0
    item={'name':name,'passed':passed,'exit_code':process.returncode}
    if not passed and not redact_output:item.update(stdout=process.stdout,stderr=process.stderr)
    checks.append(item)
    if not passed:raise RuntimeError(name+' failed')
    return process.stdout
try:
    text=run('Go contracts and storage with race detector',['go','test','-race','-count=1','-json','./...'],environment=dict(os.environ,GOTOOLCHAIN='go1.27.4'),timeout=600)
    events=[json.loads(line) for line in text.splitlines() if line.startswith('{')]
    checks[-1]['passed_tests']=[e['Test'] for e in events if e.get('Action')=='pass' and e.get('Test')]
    validation=run('OpenAPI specification',[args.validator_python,'-c',"import json;from openapi_spec_validator import validate;s=json.load(open('api/openapi.json'));validate(s);ids=[o['operationId'] for m in s['paths'].values() for o in m.values()];assert len(ids)==len(set(ids));assert not any('paperless' in p for p in s['paths']);print(json.dumps({'operations':len(ids),'sha256':__import__('hashlib').sha256(open('api/openapi.json','rb').read()).hexdigest()}))"])
    checks[-1]['result']=json.loads(validation)
    values=['--set-string','image.repository=registry.example.test/scanner-kiosk','--set-string','image.digest=sha256:'+'1'*64,'--set-string','state.existingClaim=scanner-state','--set-string','documents.existingClaim=scanner-documents','--set-string','apiSecretName=fixture-api-secrets','--set-string','workerSecretName=fixture-worker-secrets']
    run('Helm lint',['helm','lint','deploy/helm',*values])
    rendered=run('Helm render',['helm','template','scanner-kiosk','deploy/helm',*values])
    # Validate semantic design boundaries with a local YAML parser; no cluster request.
    inspection=subprocess.run([args.validator_python,'-c',"import sys,yaml,json;x=list(yaml.safe_load_all(sys.stdin));d=next(v for v in x if v['kind']=='Deployment');s=d['spec'];assert s['replicas']==1 and s['strategy']['type']=='Recreate';p=s['template']['spec'];assert 'fsGroup' not in p['securityContext'];assert p['securityContext']['runAsUser']==10001 and p['securityContext']['runAsGroup']==10001;c={v['name']:v for v in p['containers']};assert set(c)=={'api','worker'};assert c['api']['envFrom'][0]['secretRef']['name']!=c['worker']['envFrom'][0]['secretRef']['name'];v=p['volumes'];assert v[0]['persistentVolumeClaim']['claimName']!=v[1]['persistentVolumeClaim']['claimName'];assert not any('OIDC' in e['name'] for e in c['worker']['env']);assert next(e['value'] for e in c['worker']['env'] if e['name']=='GOMEMLIMIT')=='1024MiB';assert not any(e['name']=='GOMEMLIMIT' for e in c['api']['env']);native={'SCANNER_CLEANUP_TIMEOUT_SECONDS','SCANNER_SCAN_CONCURRENCY','SCANNER_ACQUISITION_ENABLED'};assert native.issubset({e['name'] for e in c['worker']['env']}) and not native.intersection({e['name'] for e in c['api']['env']});print(json.dumps({'replicas':1,'roles':list(c),'separate_secrets':True,'distinct_storage_claims':True}))"],input=rendered,cwd=root,capture_output=True,text=True,timeout=30)
    if inspection.returncode:raise RuntimeError('Helm semantic inspection failed: '+inspection.stderr)
    checks.append({'name':'Helm semantic boundaries','passed':True,'result':json.loads(inspection.stdout)})
    run('Helm native runtime contracts',[args.validator_python,'scripts/test_helm_native.py'])
    run('Helm rejects missing required deployment inputs',['helm','template','scanner-kiosk','deploy/helm'],expected=1)
    run('Helm rejects reused document/state storage',['helm','template','scanner-kiosk','deploy/helm',*values,'--set-string','documents.existingClaim=scanner-state'],expected=1)
    run('Helm rejects mutable image tag',['helm','template','scanner-kiosk','deploy/helm',*values,'--set-string','image.digest=latest'],expected=1)
    environment=dict(os.environ,GOMEMLIMIT='1152MiB',SCANNER_IMAGE='registry.example.test/scanner-kiosk@sha256:'+'1'*64,SCANNER_PUBLIC_URL='https://scanner.example.test',SCANNER_SCANNERS_JSON='[{"id":"fixture_scanner_0001","name":"Fixture scanner","driver":"sane-airscan","endpoint":"http://192.0.2.10/eSCL"}]',SCANNER_STATE_HOST_PATH='/tmp/scanner-compose-state-fixture',SCANNER_DOCUMENT_HOST_PATH='/tmp/scanner-compose-documents-fixture',SCANNER_OIDC_CLIENT_SECRET='synthetic-secret-never-for-use',SCANNER_OIDC_ISSUER='https://issuer.example.test',SCANNER_OIDC_CLIENT_ID='fixture',SCANNER_AUTHENTICATE_TYPE='oidc',SCANNER_AUTH_LOGIN_REQUESTS_PER_MINUTE='12',SCANNER_AUTH_LOGIN_BURST='6',SCANNER_OCR_PLUGINS='tesseract',SCANNER_OCR_LLM_PROFILES_JSON='[]',SCANNER_OCR_LLM_TEXT_LAYER_FONT='/fonts/approved.ttf',SCANNER_GLOBAL_MAX_BYTES='536870912',SCANNER_MIN_FREE_BYTES='33554432',SCANNER_USER_MAX_BYTES='268435456',SCANNER_JOB_MAX_BYTES='104857600',SCANNER_OIDC_CLIENT_AUTH='client_secret_post',SCANNER_SESSION_IDLE_SECONDS='1200',SCANNER_CLEANUP_TIMEOUT_SECONDS='17',SCANNER_SCAN_CONCURRENCY='2',SCANNER_ACQUISITION_ENABLED='true',SCANNER_SCANIMAGE_PATH='/opt/scanner/scanimage',SCANNER_SANE_PARAMETERS_PATH='/opt/scanner/sane-parameters',SCANNER_SANE_LIBRARY_DIRS_JSON='["/opt/scanner/lib"]')
    composition=json.loads(run('Compose config rendering',['docker','compose','-f','deploy/compose.yaml','config','--format','json'],environment=environment,redact_output=True))
    api,worker=composition['services']['api'],composition['services']['worker']
    assert not any(k.startswith('SCANNER_OIDC_') for k in worker['environment'])
    for key in ['SCANNER_CLEANUP_TIMEOUT_SECONDS','SCANNER_SCAN_CONCURRENCY','SCANNER_ACQUISITION_ENABLED','SCANNER_SCANIMAGE_PATH','SCANNER_SANE_PARAMETERS_PATH','SCANNER_SANE_LIBRARY_DIRS_JSON']:
        assert worker['environment'][key]==environment[key] and key not in api['environment']
    for key in ['SCANNER_AUTHENTICATE_TYPE','SCANNER_AUTH_LOGIN_REQUESTS_PER_MINUTE','SCANNER_AUTH_LOGIN_BURST']:
        assert api['environment'][key]==environment[key] and key not in worker['environment']
    assert worker['environment']['GOMEMLIMIT']=='1152MiB' and 'GOMEMLIMIT' not in api['environment']
    assert 'SCANNER_AUTH_MODE' not in api['environment']
    assert api['environment']['SCANNER_GLOBAL_MAX_BYTES']==worker['environment']['SCANNER_GLOBAL_MAX_BYTES']=='536870912'
    assert api['environment']['SCANNER_MIN_FREE_BYTES']==worker['environment']['SCANNER_MIN_FREE_BYTES']=='33554432'
    assert worker['environment']['SCANNER_USER_MAX_BYTES']=='268435456' and worker['environment']['SCANNER_JOB_MAX_BYTES']=='104857600'
    assert api['environment']['SCANNER_OIDC_CLIENT_AUTH']=='client_secret_post'
    assert api['environment']['SCANNER_SESSION_IDLE_SECONDS']=='1200' and 'SCANNER_SESSION_IDLE_SECONDS' not in worker['environment']
    assert api['environment']['SCANNER_OIDC_CLIENT_SECRET']=='synthetic-secret-never-for-use'
    assert api['environment']['SCANNER_OCR_LLM_PROFILES_JSON']==worker['environment']['SCANNER_OCR_LLM_PROFILES_JSON']
    assert api['environment']['SCANNER_OCR_LLM_TEXT_LAYER_FONT']==worker['environment']['SCANNER_OCR_LLM_TEXT_LAYER_FONT']=='/fonts/approved.ttf'
    assert len(worker['volumes'])==2 and worker['read_only'] and api['read_only']
    checks.append({'name':'Compose role/environment boundaries','passed':True})
except Exception as error:
    checks.append({'name':'verification completion','passed':False,'error':str(error)})
unchanged=before==hashes();passed=unchanged and all(c['passed'] for c in checks)
receipt={'timestamp_utc':datetime.now(timezone.utc).isoformat(),'passed':passed,'source_unchanged_during_checks':unchanged,'source_sha256':before,'checks':checks,'scope':'Go component/worker runtime tests (including isolated local HTTP/native-tool fixtures) and API/deployment-design validation. No application image build, persistent/deployed endpoint, real IdP/scanner/external OCR service, production retention/GC acceptance or deployment.'}
args.receipt.write_text(json.dumps(receipt,ensure_ascii=False,indent=2)+'\n')
print(json.dumps({'passed':passed,'checks':len(checks),'go_tests':len(checks[0].get('passed_tests',[])),'receipt':str(args.receipt)}))
sys.exit(0 if passed else 1)
