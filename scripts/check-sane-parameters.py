#!/usr/bin/env python3
"""Build the isolated SANE parameter helper and verify actual backend geometry."""
import argparse,hashlib,json,os,subprocess,tempfile
from pathlib import Path
from datetime import datetime,timezone
p=argparse.ArgumentParser()
for name in ['binary','include-dir','receipt']:p.add_argument('--'+name,required=True)
p.add_argument('--library-dir',action='append',required=True)
a=p.parse_args();root=Path(__file__).resolve().parents[1]
files=sorted([*root.glob('cmd/**/*.go'),*root.glob('internal/**/*.go'),*root.glob('internal/**/*.sql'),*root.glob('pkg/**/*.go'),root/'go.mod',root/'go.sum',Path(__file__).resolve()])
def hashes():return {str(f.relative_to(root)):hashlib.sha256(f.read_bytes()).hexdigest() for f in files}
before=hashes();include=Path(a.include_dir).resolve();libraries=[Path(directory).resolve() for directory in a.library_dir]
# CGO flag variables use their own tokenizer. Do not accept ambiguous flag paths.
for directory in [include,*libraries]:
 if not directory.is_dir() or any(char.isspace() or char in "'\"\\" for char in str(directory)):p.error('native include/library paths must be existing directories without flag quoting')
env=dict(os.environ,GOTOOLCHAIN='go1.27.3',CGO_ENABLED='1',CGO_CFLAGS='-I'+str(include),CGO_LDFLAGS=' '.join('-L'+str(directory) for directory in libraries))
with tempfile.TemporaryDirectory(prefix='scanner-parameters-') as temporary:
 helper=Path(temporary)/'sane-parameters';static=Path(temporary)/'scanner'
 subprocess.run(['go','build','-tags','saneparams','-o',str(helper),'./cmd/sane-parameters'],cwd=root,env=env,check=True,capture_output=True,timeout=90)
 symbols=subprocess.check_output(['nm','-u',str(helper)],text=True)
 for forbidden in ['sane_start','sane_read','sane_get_devices','sane_cancel']:
  assert forbidden not in symbols,'helper imports acquisition/discovery API'
 for required in ['sane_init','sane_open','sane_control_option','sane_get_parameters','sane_close','sane_exit']:assert required in symbols,'missing required standard SANE API'
 subprocess.run(['go','build','-o',str(static),'./cmd/scanner'],cwd=root,env=dict(env,CGO_ENABLED='0'),check=True,capture_output=True,timeout=90)
 assert 'INTERP' not in subprocess.check_output(['readelf','-l',str(static)],text=True),'main binary gained dynamic interpreter'
 env.update(SCANNER_TEST_SANE_PARAMETERS=str(helper),SCANNER_TEST_SANE_BINARY=str(Path(a.binary).resolve()),SCANNER_TEST_SANE_LIBRARY_DIRS=':'.join(str(directory) for directory in libraries))
 run=subprocess.run(['go','test','-race','-count=1','-tags','saneparams','-run','TestInstalledParameters|TestInstalledRegisteredDriver','-json','./internal/scanner/sane','./internal/scanjob'],cwd=root,env=env,capture_output=True,text=True,timeout=90)
 events=[json.loads(line) for line in run.stdout.splitlines() if line.startswith('{')]
 if run.returncode or any(event['Action'] in ['fail','skip'] for event in events):raise RuntimeError(run.stdout[-16000:]+run.stderr[-2000:])
 passed=[event['Test'] for event in events if event['Action']=='pass' and event.get('Test')]
 assert {'TestInstalledRegisteredDriverScanPublication/Idle','TestInstalledRegisteredDriverScanPublication/Processing','TestInstalledParametersWithoutAcquisition/gray','TestInstalledParametersWithoutAcquisition/color','TestInstalledParametersMatchActualBitmap/gray','TestInstalledParametersMatchActualBitmap/color','TestInstalledParametersRejectUnsupportedArea','TestInstalledParametersEnforcePixelLimit'}.issubset(passed)
 runtime_env={'LC_ALL':'C','LD_LIBRARY_PATH':':'.join(str(directory) for directory in libraries)}
 denied=subprocess.run([str(helper),'--device-name','test:0','--allow-test-backend','--size','a4','--color','gray','--dpi','300'],env=runtime_env,capture_output=True,timeout=5)
 assert denied.returncode==1 and not denied.stdout and denied.stderr==b'scanner parameters unavailable\n','helper accepted absent private configuration'
 checks=['isolated helper uses only standard parameter/configuration APIs','main API/worker builds with CGO disabled and no dynamic interpreter','private configuration required before native initialization','busy-read backend returns parameters without acquisition','actual test-backend PNG dimensions match parameter read-back','unsupported area and pixel limit rejected','registered driver to real executor publication and busy-idle gating']
 tools={'binary':hashlib.sha256(Path(a.binary).read_bytes()).hexdigest(),'header':hashlib.sha256((include/'sane/sane.h').read_bytes()).hexdigest(),**{str(file):hashlib.sha256(file.read_bytes()).hexdigest() for directory in libraries for file in directory.glob('*.so*') if file.is_file()}}
 assert before==hashes(),'sources changed during verification'
 receipt={'passed':True,'timestamp_utc':datetime.now(timezone.utc).isoformat(),'checks':checks,'tests':passed,'source_sha256':before,'tool_sha256':tools,'scope':'Separate product Go/cgo helper linked to official libsane; main binary remains CGO-free. Exact selected parameter read-back and actual virtual SANE capture only. Registered driver/executor composition uses virtual capture and local fixture eSCL status. No real Canon/airscan, deployment setup, readiness, recovery, HTTP scan admission or deployment acceptance.'}
 Path(a.receipt).write_text(json.dumps(receipt,indent=2)+'\n')
 print(json.dumps({'passed':True,'checks':len(checks),'tests':len(passed)}))
