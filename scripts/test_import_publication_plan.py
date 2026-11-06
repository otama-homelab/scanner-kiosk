import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

spec=importlib.util.spec_from_file_location('import_plan',Path(__file__).with_name('import-publication-plan.py'))
module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)


class ImportPlanTests(unittest.TestCase):
    def test_trusted_hash_and_same_run_artifact_allow_an_approved_earlier_attempt(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);plan=root/'plan.json';sha='a'*40
            raw=json.dumps({'repository':'otama-homelab/scanner-kiosk','source_sha':sha,'artifact_id':42,'external_writes':False}).encode()
            with self.assertRaises(ValueError):module.load(raw,'0'*64,sha,plan)
            self.assertFalse(plan.exists())
            for key,value in [('source_sha','b'*40),('artifact_id',True),('artifact_id',0),('external_writes',True)]:
                changed=json.loads(raw);changed[key]=value;changed_raw=json.dumps(changed).encode()
                with self.assertRaises(ValueError):module.load(changed_raw,hashlib.sha256(changed_raw).hexdigest(),sha,plan)
                self.assertFalse(plan.exists())
            self.assertEqual(module.load(raw,hashlib.sha256(raw).hexdigest(),sha,plan),42)
            metadata={'id':42,'expired':False,'name':'staged-candidate-'+sha+'-1','size_in_bytes':100,
                      'workflow_run':{'id':99,'head_sha':sha,'repository_id':7,'head_repository_id':7}}
            path=root/'artifact.json';path.write_text(json.dumps(metadata))
            self.assertEqual(module.artifact_name(plan,path,99),metadata['name'])
            for key,value in [('id',43),('expired',True),('name','other-artifact')]:
                changed=copy.deepcopy(metadata);changed[key]=value;path.write_text(json.dumps(changed))
                with self.assertRaises(ValueError):module.artifact_name(plan,path,99)
            for key,value in [('head_sha','b'*40),('head_repository_id',8)]:
                changed=copy.deepcopy(metadata);changed['workflow_run'][key]=value;path.write_text(json.dumps(changed))
                with self.assertRaises(ValueError):module.artifact_name(plan,path,99)
            path.write_text(json.dumps(metadata))
            with self.assertRaises(ValueError):module.artifact_name(plan,path,100)
            with self.assertRaises(FileExistsError):module.load(raw,hashlib.sha256(raw).hexdigest(),sha,plan)


    def test_preview_selects_latest_valid_same_run_attempt_without_trusting_other_artifacts(self):
        sha='a'*40
        row={'id':42,'expired':False,'name':'staged-candidate-'+sha+'-1','size_in_bytes':100,
             'workflow_run':{'id':99,'head_sha':sha,'repository_id':7,'head_repository_id':7}}
        newer=copy.deepcopy(row);newer.update(id=44,name='staged-candidate-'+sha+'-2')
        pages=[{'artifacts':[row,{'name':'other-artifact'}]},{'artifacts':[newer]}]
        self.assertEqual(module.preview_artifact(pages,sha,99),newer['name'])
        for key,value in [('head_sha','b'*40),('head_repository_id',8),('id',100)]:
            broken=copy.deepcopy(newer);broken['workflow_run'][key]=value
            with self.subTest(key=key),self.assertRaises(ValueError):
                module.preview_artifact([{'artifacts':[broken]}],sha,99)
        invalid_id=copy.deepcopy(row);invalid_id['id']=True
        with self.assertRaises(ValueError):module.preview_artifact([{'artifacts':[invalid_id]}],sha,99)
        for rows in [[],[row,row]]:
            with self.assertRaises(ValueError):module.preview_artifact([{'artifacts':rows}],sha,99)


if __name__=='__main__':unittest.main()
