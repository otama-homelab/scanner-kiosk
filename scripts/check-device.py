#!/usr/bin/env python3
"""Explicit, read-only registered eSCL/SANE device inspection; never acquires images."""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import urllib.request
import xml.etree.ElementTree as ET


HARNESS = r'''
package main
import (
 "context"
 "encoding/json"
 "os"
 "path/filepath"
 "time"
 "scanner-kiosk/internal/scanner/escl"
 "scanner-kiosk/internal/scanner/sane"
)
func main() {
 report := map[string]any{"passed":false,"cleanup_verified":false,"acquisition_invoked":false}
 defer func(){ json.NewEncoder(os.Stdout).Encode(report) }()
 ctx, cancel := context.WithTimeout(context.Background(),90*time.Second)
 defer cancel()
 work, device, endpoint := os.Args[1], os.Args[2], os.Args[3]
 directory, err := sane.WriteConfig(work,device,endpoint)
 stopped := true
 defer func(){
  if !stopped { report["retained_scratch"] = work; return }
  if directory != "" {
   for _, name := range []string{"dll.conf","airscan.conf"} {
    err := os.Remove(filepath.Join(directory,name))
    if err != nil && !os.IsNotExist(err) { report["retained_scratch"] = work; return }
   }
   if os.Remove(directory) != nil { report["retained_scratch"] = work; return }
  }
  if os.Remove(work) != nil { report["retained_scratch"] = work; return }
  report["cleanup_verified"] = true
 }()
 if err != nil { report["stage"]="configuration"; return }
 adapter, err := sane.New(sane.Options{Binary:os.Args[4],ParametersBinary:os.Args[5],
  WorkRoot:work,ConfigDir:directory,DeviceID:device,DeviceName:"airscan:e0:"+device,
  LibraryDirs:os.Args[6:],MaxBytes:1<<20,MaxPixels:40000000,Timeout:10*time.Second,StopGrace:3*time.Second})
 if err != nil { report["stage"]="adapter"; report["error"]=err.Error(); return }
 var lastStop int64
 closeAttempt := func(attempt *sane.ProbeAttempt) bool {
  if attempt == nil { return true }
  lastStop = attempt.StoppedAtMS()
  if lastStop <= 0 || attempt.Close() != nil { stopped=false; return false }
  return true
 }
 caps, attempt, err := adapter.Probe(ctx)
 cleaned := closeAttempt(attempt)
 if err != nil || !cleaned { report["stage"]="base_options"; if err!=nil {report["error"]=err.Error()}; return }
 report["capabilities"]=caps
 selections := []sane.Selection{{Size:"a4",Color:"gray",DPI:300},{Size:"a4",Color:"color",DPI:300},{Size:"photo10x15",Color:"color",DPI:600}}
 results := []any{}
 for _, selected := range selections {
  _, attempt, err := adapter.ProbeSelection(ctx,selected)
  cleaned := closeAttempt(attempt)
  if err != nil || !cleaned { report["stage"]="selected_options"; report["selection"]=selected; if err!=nil {report["error"]=err.Error()}; return }
  params, attempt, err := adapter.ProbeParameters(ctx,selected)
  cleaned = closeAttempt(attempt)
  if err != nil || !cleaned { report["stage"]="parameters"; report["selection"]=selected; if err!=nil {report["error"]=err.Error()}; return }
  results = append(results,params)
 }
 report["parameters"]=results
 observer, err := escl.New([]escl.Device{{ID:device,Endpoint:endpoint}},escl.Options{Timeout:5*time.Second,MaxResponseBytes:256<<10})
 if err!=nil { report["stage"]="observer"; return }
 // This follows stopped local metadata probes, not an acquisition. It provides
 // no exclusive-writer proof and must never initialize/release a durable lease.
 observed, err := observer.ObserveIdle(ctx,device,lastStop)
 if err!=nil { report["stage"]="idle_observation";report["error"]=err.Error();return }
 report["idle_observed_at_ms"]=observed
 report["passed"]=true
}
'''


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, response, code, message, headers, new_url):
        return None


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--endpoint', required=True)
    parser.add_argument('--device-id', required=True)
    parser.add_argument('--binary', required=True)
    parser.add_argument('--parameters-binary', required=True)
    parser.add_argument('--library-dir', action='append', required=True)
    parser.add_argument('--receipt', type=Path, required=True)
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    # Validate through the product's serializer before any request/native call.
    # The harness performs that validation before adapter construction; HTTP
    # inspection below runs only if the supervised product probe succeeds.
    sources = sorted([*root.glob('internal/**/*.go'), *root.glob('internal/**/*.sql'),
                      *root.glob('pkg/**/*.go'), root/'go.mod', root/'go.sum', Path(__file__)])
    hashes = lambda: {str(p.relative_to(root)): hashlib.sha256(p.read_bytes()).hexdigest() for p in sources}
    before = hashes()
    tools = [Path(args.binary).resolve(), Path(args.parameters_binary).resolve()]
    tools += sorted({p.resolve() for directory in args.library_dir for p in Path(directory).glob('*.so*') if p.is_file()})
    tool_hashes = {str(p): hashlib.sha256(p.read_bytes()).hexdigest() for p in tools}
    work = tempfile.mkdtemp(prefix='scanner-device-readonly-')
    with tempfile.TemporaryDirectory(prefix='.device-readonly-', dir=root) as build:
        source, binary = Path(build)/'main.go', Path(build)/'inspect'
        source.write_text(HARNESS)
        subprocess.run(['go', 'build', '-o', str(binary), str(source)], cwd=root,
                       env=dict(os.environ, CGO_ENABLED='0'), check=True, timeout=60)
        process = subprocess.run([str(binary), work, args.device_id, args.endpoint,
                                  str(Path(args.binary).resolve()), str(Path(args.parameters_binary).resolve()),
                                  *[str(Path(p).resolve()) for p in args.library_dir]],
                                 cwd=root, capture_output=True, text=True, timeout=120)
        if process.returncode:
            raise RuntimeError('read-only harness failed; private scratch retained at '+work)
        result = json.loads(process.stdout)
    metadata = {}
    if result.get('passed'):
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
        # Whitelist summaries: omit serials, UUIDs, network and job identifiers.
        allowed = {'MakeAndModel', 'Version', 'State', 'ColorMode', 'XResolution',
                   'YResolution', 'MaxWidth', 'MaxHeight', 'DocumentFormat'}
        for suffix in ['ScannerCapabilities', 'ScannerStatus']:
            request = urllib.request.Request(args.endpoint.rstrip('/')+'/'+suffix, method='GET',
                                             headers={'Cache-Control':'no-cache, no-store', 'Accept-Encoding':'identity'})
            with opener.open(request, timeout=8) as response:
                data = response.read((256 << 10)+1)
                if len(data) > 256 << 10:
                    raise RuntimeError('device metadata exceeds bound')
                tree = ET.fromstring(data)
                fields = {}
                for node in tree.iter():
                    name = node.tag.rsplit('}', 1)[-1]
                    if name in allowed:
                        fields.setdefault(name, []).append((node.text or '').strip()[:120])
                metadata[suffix] = {'http_status':response.status, 'bytes':len(data),
                                    'sha256':hashlib.sha256(data).hexdigest(), 'fields':fields}
    if before != hashes():
        raise RuntimeError('sources changed during inspection')
    receipt = {'passed':bool(result.get('passed') and result.get('cleanup_verified')),
               'timestamp_utc':datetime.now(timezone.utc).isoformat(), 'source_sha256':before,
               'tool_sha256':tool_hashes, 'product_probe':result, 'device_metadata':metadata,
               'scope':'Read-only real device metadata, exact selected options and pre-start SANE parameters through official airscan. No image acquisition, writer fencing, durable readiness/lease mutation, geometry after acquisition, cancellation, PDF/OCR, UI or deployment acceptance.'}
    args.receipt.write_text(json.dumps(receipt, indent=2)+'\n')
    print(json.dumps({'passed':receipt['passed'], 'product_probe':result}))
    return 0 if receipt['passed'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
