#!/usr/bin/env python3
"""Bind a same-run development artifact or import an operator-approved formal plan."""
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


def validate_artifact(plan,metadata,run_id):
    run=metadata['workflow_run'];name=metadata['name']
    if (type(metadata['id']) is not int or not 0<metadata['id']<1<<63
            or type(run['repository_id']) is not int or run['repository_id']<=0
            or metadata['id']!=plan['artifact_id'] or metadata['expired'] is not False
            or run['id']!=run_id or run['head_sha']!=plan['source_sha']
            or run['repository_id']!=run['head_repository_id']
            or not re.fullmatch('staged-candidate-'+re.escape(plan['source_sha'])+'-[1-9][0-9]*',name)
            or type(metadata['size_in_bytes']) is not int or not 0<metadata['size_in_bytes']<=3<<30):
        raise ValueError('approved artifact is not from this exact source/run')
    return name


def artifact_name(plan_path,metadata_path,run_id):
    return validate_artifact(stage.read_json(plan_path),stage.read_json(metadata_path),run_id)


def preview_artifact(pages,sha,run_id):
    if not isinstance(sha,str) or not re.fullmatch(r'[0-9a-f]{40}|[0-9a-f]{64}',sha) or type(run_id) is not int or run_id<=0:
        raise ValueError('invalid preview source/run identity')
    if not isinstance(pages,list) or not 0<len(pages)<=8:
        raise ValueError('incomplete or oversized preview artifact pages')
    candidates=[]
    for page in pages:
        rows=page['artifacts']
        if not isinstance(rows,list) or len(rows)>100:
            raise ValueError('invalid preview artifact page')
        for row in rows:
            match=re.fullmatch('staged-candidate-'+re.escape(sha)+'-([1-9][0-9]*)',row['name'])
            if not match or row.get('expired') is True:
                continue
            validate_artifact({'artifact_id':row['id'],'source_sha':sha},row,run_id)
            candidates.append((int(match[1]),row['name']))
    if not candidates or len({attempt for attempt,name in candidates})!=len(candidates):
        raise ValueError('missing or ambiguous staged preview candidate')
    return max(candidates)[1]


if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--plan',type=Path)
    parser.add_argument('--preview-run-id',type=int)
    parser.add_argument('--sha');parser.add_argument('--metadata',type=Path);parser.add_argument('--run-id',type=int)
    args=parser.parse_args()
    if args.preview_run_id is not None:
        if not args.metadata or args.plan:parser.error('preview selection requires only source/run and paginated artifact metadata')
        print(preview_artifact(stage.read_json(args.metadata),args.sha,args.preview_run_id))
    elif args.plan is None:
        parser.error('protected publication requires --plan')
    elif args.metadata:
        print(artifact_name(args.plan,args.metadata,args.run_id))
    else:
        print(load(os.environ.get('PLAN_JSON','').encode(),os.environ.get('PLAN_SHA256',''),args.sha,args.plan))
