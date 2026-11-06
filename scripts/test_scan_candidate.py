import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

spec=importlib.util.spec_from_file_location('scan_candidate',Path(__file__).with_name('scan-candidate.py'))
module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)


class CandidateScanTests(unittest.TestCase):
    def test_core_and_terminal_images_have_separate_scan_inputs(self):
        self.assertEqual(module.INPUTS['image'],'image-sbom/image.syft.json')
        self.assertEqual(module.INPUTS['tesseract-image'],'tesseract-image-sbom/image.syft.json')

    def test_findings_remain_failed_threshold_and_tool_errors_fail_collection(self):
        for code in [0,2,1]:
            with self.subTest(code=code),tempfile.TemporaryDirectory() as directory:
                root=Path(directory);(root/'bin').mkdir();(root/'bin/grype').write_bytes(b'tool')
                candidate=root/'candidate';candidate.mkdir()
                for name in set(module.INPUTS.values()):
                    path=candidate/name;path.parent.mkdir(exist_ok=True);path.write_text('{}')
                def invoke(command,**kwargs):
                    self.assertEqual(kwargs['env']['GRYPE_DB_AUTO_UPDATE'],'false')
                    report=Path(command[command.index('--file')+1]);report.write_text(json.dumps({'matches':[],'descriptor':{'name':'grype'}}))
                    return subprocess.CompletedProcess(command,code)
                with patch.object(module,'WORK',root),patch.object(module.subprocess,'check_output',return_value=b'{"valid":true}'),patch.object(module.subprocess,'run',side_effect=invoke):
                    if code==1:
                        with self.assertRaises(RuntimeError):module.scan(candidate,root/'reports')
                    else:
                        result=module.scan(candidate,root/'reports')
                        self.assertTrue(result['scans_completed'])
                        self.assertEqual(result['advisory_threshold_passed'],code==0)
                        self.assertFalse(result['publication_approved'])
                receipt=json.loads((root/'reports/receipt.json').read_text())
                if code==1:self.assertFalse(receipt['scans_completed'])


if __name__=='__main__':unittest.main()
