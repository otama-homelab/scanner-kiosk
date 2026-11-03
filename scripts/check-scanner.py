#!/usr/bin/env python3
"""Verify the product native capture adapter with official SANE test backend."""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import subprocess

parser = argparse.ArgumentParser()
parser.add_argument('--binary', required=True)
parser.add_argument('--library-dir', action='append', required=True)
parser.add_argument('--receipt', required=True)
args = parser.parse_args()
root = Path(__file__).resolve().parents[1]
files = sorted([*root.glob('cmd/**/*.go'), *root.glob('internal/**/*.go'), *root.glob('internal/**/*.sql'), *root.glob('pkg/**/*.go'), root/'go.mod', root/'go.sum', Path(__file__).resolve()])
def hashes():
    return {str(p.relative_to(root)):hashlib.sha256(p.read_bytes()).hexdigest() for p in files}
before = hashes()
env = dict(os.environ, GOTOOLCHAIN='go1.27.3', SCANNER_TEST_SANE_BINARY=str(Path(args.binary).resolve()), SCANNER_TEST_SANE_LIBRARY_DIRS=':'.join(str(Path(p).resolve()) for p in args.library_dir))
run = subprocess.run(['go','test','-race','-count=1','-json','./internal/scanner/...','./internal/scannerdriver','./internal/scanjob'],cwd=root,env=env,capture_output=True,text=True,timeout=120)
events = [json.loads(line) for line in run.stdout.splitlines() if line.startswith('{')]
if run.returncode or any(e['Action']=='fail' or (e['Action']=='skip' and 'Test' in e) for e in events):
    raise RuntimeError(run.stdout + run.stderr)
passed = [e['Test'] for e in events if e['Action']=='pass' and 'Test' in e]
required = {'TestInstalledScanimageCapture/'+mode+'-'+dpi for mode in ['color','gray'] for dpi in ['300','600']} | {'TestESCLStatusControlsScanPublication/Idle','TestESCLStatusControlsScanPublication/Processing','TestInstalledScanimageCapabilityProbe','TestInstalledSaneScanJob','TestInstalledScanimageA4','TestInstalledScanimageBusyAndCancellation/busy','TestInstalledScanimageBusyAndCancellation/cancel'}
required |= {'TestRegisteredDriverResolvesExactPlanAndPreventsReexecution','TestUnresolvedDriverInspectionKeepsLeaseAndReservation'}
required |= {'TestInstalledSelectedProbeDoesNotReadScanner/'+mode for mode in ['gray','color']} | {'TestInstalledSelectedProbeRejectsSmallBed'}
required |= {'TestReadOnlyCaptureMetadataDoesNotClaimOrAcquire','TestReadOnlyMetadataRejectsInvalidOrUnresolvedInspection'}
required |= {'TestDifferentDevicesCaptureConcurrentlyAndSameDeviceWaits','TestDeviceUncertaintyDoesNotBlockOtherDeviceButCloseRetainsEvidence','TestJobUseFenceIsSharedAcrossDevices','TestCloseJoinsEveryDeviceAndRefusesNewOperations'}
assert required.issubset(passed) and before==hashes()
libraries = {str(p.resolve()) for directory in args.library_dir for p in Path(directory).glob('*.so*') if p.is_file()}
receipt = {'timestamp_utc':datetime.now(timezone.utc).isoformat(),'passed':True,'tests':passed,'source_sha256':before,'tool_sha256':{'binary':hashlib.sha256(Path(args.binary).read_bytes()).hexdigest(), **{p:hashlib.sha256(Path(p).read_bytes()).hexdigest() for p in sorted(libraries)}},'scope':'Product capture adapter with official SANE test:0 only: A4 grayscale 300dpi, 100x150mm color/gray 300/600dpi, injected busy, slow cancellation, base/selected-option backend inspection, coupled read-only capture metadata and registered driver per-device concurrency/serialization/shutdown unit gates, file validation/lifecycle and admitted scan-job publication with fixture idle and read-only eSCL observers against local HTTP fixtures. No Canon/airscan, real physical idle, application HTTP API or production deployment acceptance.'}
Path(args.receipt).write_text(json.dumps(receipt,indent=2)+'\n')
print(json.dumps({'passed':True,'tests':len(passed)}))
