#!/usr/bin/env python3
"""Import an operator-approved plan, bound to an immutable artifact in this push run."""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re

spec=importlib.util.spec_from_file_location('stage',Path(__file__).with_name('stage-candidate.py'))
stage=importlib.util.module_from_spec(spec);spec.loader.exec_module(stage)


def load(raw,digest,sha,output):
    if (not isinstance(sha,str) or not re.fullmatch(r'[0-9a-f]{40}|[0-9a-f]{64}',sha)
            or not isinstance(digest,str) or not re.fullmatch('[0-9a-f]{64}',digest)
            or not 0<len(raw)<=2<<20 or hashlib.sha256(raw).hexdigest()!=digest):
        raise ValueError('protected plan hash mismatch or missing plan')
    plan=json.loads(raw,object_pairs_hook=stage.unique_object)
    if (plan.get('repository')!='otama-homelab/scanner-kiosk' or plan.get('source_sha')!=sha
            or type(plan.get('artifact_id')) is not int or not 0<plan['artifact_id']<1<<63
            or plan.get('external_writes') is not False):
        raise ValueError('plan source/repository/artifact mismatch')
    output.parent.mkdir(parents=True,exist_ok=True,mode=0o700)
    with output.open('xb') as stream:stream.write(raw)
    output.chmod(0o444)
    return plan['artifact_id']


def artifact_name(plan_path,metadata_path,run_id):
    plan=stage.read_json(plan_path);metadata=stage.read_json(metadata_path)
    run=metadata['workflow_run'];name=metadata['name']
    if (metadata['id']!=plan['artifact_id'] or metadata['expired'] is not False
            or run['id']!=run_id or run['head_sha']!=plan['source_sha']
            or run['repository_id']!=run['head_repository_id']
            or not re.fullmatch('staged-candidate-'+re.escape(plan['source_sha'])+'-[1-9][0-9]*',name)
            or type(metadata['size_in_bytes']) is not int or not 0<metadata['size_in_bytes']<=3<<30):
        raise ValueError('approved artifact is not from this exact source/run')
    return name


if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--plan',type=Path,required=True)
    parser.add_argument('--sha');parser.add_argument('--metadata',type=Path);parser.add_argument('--run-id',type=int)
    args=parser.parse_args()
    if args.metadata:
        print(artifact_name(args.plan,args.metadata,args.run_id))
    else:
        print(load(os.environ.get('PLAN_JSON','').encode(),os.environ.get('PLAN_SHA256',''),args.sha,args.plan))
