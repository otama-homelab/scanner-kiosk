import hashlib
import contextlib
import io
import importlib.util
import json
from pathlib import Path
import subprocess
import unittest
from unittest.mock import patch
import test_publication_preflight as fixtures


class WritePublicationTest(unittest.TestCase):
    def setUp(self):
        self.f = fixtures.PublicationPreflightTest()
        self.f.setUp()
        self.addCleanup(self.f.doCleanups)
        self.plan = self.f.prepare()
        self.candidate = self.f.candidate.output
        self.path = self.f.candidate.root/'plan.json'
        self.path.write_text(json.dumps(self.plan))
        self.plan_hash = hashlib.sha256(self.path.read_bytes()).hexdigest()
        spec = importlib.util.spec_from_file_location('write_publication', Path(__file__).with_name('write-publication.py'))
        self.module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.module)
        self.events, self.releases, self.tags = [], [], {}
        self.visibility, self.package_visibility = 'private', 'private'
        self.package_repository = 'otama-homelab/scanner-kiosk'
        self.tesseract_package_visibility = None
        self.tesseract_package_repository = None
        self.package_missing = False
        self.upload_failed, self.check_result = False, 'success'
        self.asset_mismatch, self.push_failed, self.source_removed = False, False, False
        self.fail_checks_after_upload = False
        self.sha = self.f.git.sha
        self.remote_head = self.sha
        self.ref = 'refs/heads/main'
        self.image_id = self.plan['image_id']
        self.registry_digest = 'sha256:'+'d'*64
        self.tesseract_image_id=self.plan['tesseract_image_id']
        self.tesseract_digest='sha256:'+'e'*64
        self.local_tags={};self.fail_tag=None
        self.env = {'GH_TOKEN': 'fixture-not-a-real-token', 'GITHUB_ACTOR': 'fixture',
            'GITHUB_ACTIONS': 'true', 'GITHUB_REPOSITORY': 'otama-homelab/scanner-kiosk',
            'GITHUB_EVENT_NAME': 'push', 'GITHUB_REF': 'refs/heads/main', 'GITHUB_SHA': self.sha,
            'SCANNER_PUBLISH_ENABLED': 'true'}

    def api(self, path, env, body=None, missing=False, paginate=False):
        if path == 'repos/otama-homelab/scanner-kiosk':
            return {'visibility': self.visibility}
        if path.endswith('/git/ref/heads/main'):
            return {'object': {'sha': self.remote_head, 'type': 'commit'}}
        if '/git/ref/tags/' in path:
            return {'object': {'sha': self.sha, 'type': 'commit'}}
        if '/compare/' in path:
            return {'status': 'identical', 'merge_base_commit': {'sha': '0'*40 if self.source_removed else self.sha}}
        if '/check-runs?' in path:
            return [{'check_runs': [{'id': 1, 'name': 'required-checks', 'head_sha': self.sha,
                'status': 'completed', 'conclusion': ('failure' if self.fail_checks_after_upload and 'upload-assets' in self.events else self.check_result),
                'app': {'slug': 'github-actions'}}]}]
        if '/packages/container/' in path:
            if self.package_missing and not self.tags:
                return None
            terminal=path.endswith('/scanner-kiosk-tesseract')
            return {'visibility': (self.tesseract_package_visibility if terminal and self.tesseract_package_visibility is not None else self.package_visibility),
                    'repository': {'full_name': (self.tesseract_package_repository if terminal and self.tesseract_package_repository is not None else self.package_repository)}}
        if '/releases?' in path:
            return [self.releases]
        if path.endswith('/releases') and body is not None:
            self.events.append('reserve-draft')
            release = {**body, 'id': 42}
            self.releases.append(release)
            return release
        if path.endswith('/releases/42/assets'):
            return [[{'name': name, 'size': (self.candidate/name).stat().st_size,
                      'digest': 'sha256:'+('0'*64 if self.asset_mismatch else hashlib.sha256((self.candidate/name).read_bytes()).hexdigest())}
                     for name in self.plan['uploads']]]
        if path.endswith('/releases/42') and body is not None:
            self.events.append('publish-release' if body.get('draft') is False else 'finalize-release')
            release = next(r for r in self.releases if r['id'] == 42)
            release.update(body)
            return release
        raise AssertionError(('unexpected API request', path, body))

    def registry(self, tag, env, registry='ghcr.io/otama-homelab/scanner-kiosk'):
        key=('tesseract-' if registry.endswith('-tesseract') else '')+tag
        return self.tags.get(key)

    def command(self, args, env, input_data=None, timeout=60, allow_failure=False):
        if args[:2] == ['gh', 'release']:
            self.events.append('upload-assets')
            if self.upload_failed:
                raise RuntimeError('fixture upload failed')
        elif args[:2] == ['docker', 'push']:
            tag = ('tesseract-' if args[-1].split(':',1)[0].endswith('-tesseract') else '')+args[-1].rsplit(':', 1)[1]
            self.events.append('push-'+tag)
            if self.push_failed or tag==self.fail_tag:
                raise RuntimeError('fixture registry push failed')
            image_id=self.local_tags[args[-1]]
            self.tags[tag] = {'digest': self.tesseract_digest if image_id==self.tesseract_image_id else self.registry_digest,
                              'image_id':image_id}
        elif args[:3] == ['docker', 'image', 'inspect']:
            return json.dumps([{'Id': args[-1], 'Config': {'Labels': {'org.opencontainers.image.source': 'https://github.com/otama-homelab/scanner-kiosk'}}}]).encode()
        elif args[:2]==['docker','tag']:
            self.local_tags[args[-1]]=args[-2]
        elif args[:2] == ['docker', 'load']:
            self.events.append('load-image')
        elif args[:2] not in [['docker', 'login'], ['docker', 'tag']]:
            raise AssertionError(('unexpected command', args))
        return b''

    def run_writer(self, apply=False):
        with patch.dict('os.environ', self.env), patch.object(self.module, 'api', self.api), \
                patch.object(self.module, 'registry_image', self.registry), \
                patch.object(self.module, 'command', self.command):
            return self.module.publish(self.candidate, self.path, self.plan_hash,
                self.ref, self.sha, 'push', self.f.git.repo, apply=apply)

    def development_preview(self):
        self.plan.pop('approval_sha256')
        self.plan.update(development_preview=True, release_tag='preview-source-'+self.sha,
                         policy_sha256=hashlib.sha256(self.module.policy.PREVIEW_REVIEW.read_bytes()).hexdigest())
        self.plan['uploads'] = [name for name in self.plan['uploads'] if name not in ['scanner-onefile-linux-amd64', 'scanner-image.tar','scanner-tesseract-image.tar']]
        self.visibility = self.plan['visibility'] = 'public'
        self.path.write_text(json.dumps(self.plan))
        self.plan_hash = hashlib.sha256(self.path.read_bytes()).hexdigest()
        self.env['SCANNER_PUBLISH_ENABLED'] = 'false'

    def test_both_packages_are_checked_before_any_writes(self):
        for field, value in [('tesseract_package_visibility', 'public'),
                             ('tesseract_package_repository', 'another/repository')]:
            with self.subTest(field=field):
                setattr(self, field, value)
                with self.assertRaises(ValueError):
                    self.run_writer(apply=True)
                self.assertEqual(self.events, [])
                setattr(self, field, None)

    def test_published_references_separate_core_and_ocr_images(self):
        self.run_writer(apply=True)
        for role, image_id in [('core', self.image_id), ('tesseract', self.tesseract_image_id)]:
            registry = self.module.REGISTRIES[role]
            self.assertEqual(self.local_tags[registry+':'+self.plan['image_tag']], image_id)
            self.assertEqual(self.local_tags[registry+':preview'], image_id)

    def test_runtime_images_have_separate_registry_packages(self):
        self.assertEqual(self.module.REGISTRIES, {
            'core': 'ghcr.io/otama-homelab/scanner-kiosk',
            'tesseract': 'ghcr.io/otama-homelab/scanner-kiosk-tesseract'})
        self.assertEqual(self.plan['image_tag'], self.plan['tesseract_image_tag'])

    def test_main_preview_keeps_sources_and_never_pushes_latest(self):
        self.development_preview()
        self.package_missing = True
        result = self.run_writer(apply=True)
        self.assertTrue(result['published'])
        self.assertEqual(set(self.tags), {self.plan['image_tag'], 'tesseract-'+self.plan['tesseract_image_tag'],'preview','tesseract-preview'})
        self.assertEqual(self.releases[0]['tag_name'], 'preview-source-'+self.sha)
        self.assertTrue(self.releases[0]['prerelease'])
        self.assertIn('G5 acceptance remains incomplete', self.releases[0]['body'])
        self.assertLess(self.events.index('publish-release'), self.events.index('push-'+self.plan['image_tag']))
        self.assertEqual(result['package_visibility'], 'private')

    def test_preview_writer_requires_git_checkout_validation_before_writes(self):
        self.development_preview()
        with patch.object(self.module.policy.refs, 'release_ref', side_effect=FileNotFoundError('git')):
            with self.assertRaises(FileNotFoundError):
                self.run_writer(apply=True)
        self.assertEqual(self.events, [])

    def test_preview_alias_recognizes_its_source_only_prerelease_owner(self):
        self.development_preview()
        self.releases = [{'tag_name': 'preview-source-'+self.sha, 'draft': False, 'prerelease': True,
                          'body': self.module.body_text(self.plan, self.registry_digest,self.tesseract_digest)}]
        self.tags['preview'] = {'digest': self.registry_digest, 'image_id': self.image_id}
        with patch.object(self.module, 'api', self.api), patch.object(self.module, 'registry_image', self.registry):
            state = self.module.fresh_state(self.plan, {})
        self.assertEqual(state['preview_source_sha'], self.sha)

    def test_preview_never_opens_the_version_tag_release_gate(self):
        self.development_preview()
        self.ref = self.env['GITHUB_REF'] = 'refs/tags/v1.0.0'
        with self.assertRaises(ValueError):
            self.run_writer(apply=True)
        self.assertEqual(self.events, [])

    def test_preview_requires_the_same_reviewed_policy_and_repository_link(self):
        self.development_preview()
        self.plan['policy_sha256'] = '0'*64
        self.path.write_text(json.dumps(self.plan))
        self.plan_hash = hashlib.sha256(self.path.read_bytes()).hexdigest()
        with self.assertRaises(ValueError):
            self.run_writer(apply=True)
        self.assertEqual(self.events, [])
        self.plan['policy_sha256'] = hashlib.sha256(self.module.policy.PREVIEW_REVIEW.read_bytes()).hexdigest()
        self.path.write_text(json.dumps(self.plan))
        self.plan_hash = hashlib.sha256(self.path.read_bytes()).hexdigest()
        self.package_repository = 'another/repository'
        with self.assertRaises(ValueError):
            self.run_writer(apply=True)
        self.assertEqual(self.events, [])

    def test_dry_run_performs_no_release_or_registry_writes(self):
        result = self.run_writer()
        self.assertFalse(result['published'])
        self.assertEqual(self.events, [])

    def test_sources_are_published_before_image_and_aliases_are_last(self):
        result = self.run_writer(apply=True)
        self.assertTrue(result['published'])
        self.assertLess(self.events.index('upload-assets'), self.events.index('publish-release'))
        self.assertLess(self.events.index('publish-release'), self.events.index('push-'+self.plan['image_tag']))
        self.assertEqual(self.events[-2:], ['push-preview','push-tesseract-preview'])
        self.assertLess(self.events.index('push-tesseract-'+self.plan['tesseract_image_tag']),self.events.index('push-preview'))

    def test_failed_upload_leaves_draft_and_no_image_or_alias(self):
        self.upload_failed = True
        with self.assertRaises(RuntimeError):
            self.run_writer(apply=True)
        self.assertTrue(self.releases[0]['draft'])
        self.assertEqual(self.tags, {})

    def test_upload_digest_mismatch_keeps_draft_and_does_not_push(self):
        self.asset_mismatch = True
        with self.assertRaises(ValueError):
            self.run_writer(apply=True)
        self.assertTrue(self.releases[0]['draft'])
        self.assertEqual(self.tags, {})

    def test_image_failure_retains_accessible_source_and_leaves_aliases_untouched(self):
        self.push_failed = True
        with self.assertRaises(RuntimeError):
            self.run_writer(apply=True)
        self.assertFalse(self.releases[0]['draft'])
        self.assertEqual(self.releases[0]['make_latest'], 'false')
        self.assertNotIn('push-preview', self.events)
        self.assertEqual(self.tags, {})

    def test_source_removed_from_remote_main_is_rejected_before_writes(self):
        self.source_removed = True
        with self.assertRaises(ValueError):
            self.run_writer(apply=True)
        self.assertEqual(self.events, [])

    def test_existing_version_is_not_overwritten(self):
        self.tags[self.plan['image_tag']] = {'digest': self.registry_digest, 'image_id': self.image_id}
        with self.assertRaises(ValueError):
            self.run_writer(apply=True)
        self.assertEqual(self.events, [])

    def test_existing_terminal_version_is_not_overwritten(self):
        self.tags['tesseract-'+self.plan['tesseract_image_tag']]={'digest':self.tesseract_digest,'image_id':self.tesseract_image_id}
        with self.assertRaises(ValueError):self.run_writer(apply=True)
        self.assertEqual(self.events,[])

    def test_terminal_push_failure_keeps_sources_and_moves_neither_alias(self):
        self.fail_tag='tesseract-'+self.plan['tesseract_image_tag']
        with self.assertRaises(RuntimeError):self.run_writer(apply=True)
        self.assertFalse(self.releases[0]['draft'])
        self.assertIn(self.plan['image_tag'],self.tags)
        self.assertNotIn('preview',self.tags);self.assertNotIn('tesseract-preview',self.tags)
        self.assertFalse(self.module.publication_metadata(self.releases[0])['tesseract_image_digest'])

    def test_both_variants_keep_monotonic_aliases_after_a_partial_previous_update(self):
        git=self.f.git
        git.git('-c','user.name=Fixture','-c','user.email=fixture@example.test','commit','--allow-empty','-m','newer')
        newer=git.git('rev-parse','HEAD');git.git('update-ref','refs/remotes/origin/main',newer)
        git.git('checkout','--detach',self.sha);self.remote_head=newer
        old={**self.plan,'release_tag':'preview-source-'+self.sha}
        new={**self.plan,'source_sha':newer,'version':'preview-'+newer[:12],
             'image_id':'sha256:'+'f'*64,'tesseract_image_id':'sha256:'+'9'*64}
        old_images={'preview':{'image_id':self.image_id,'digest':self.registry_digest},
                    'tesseract-preview':{'image_id':self.tesseract_image_id,'digest':self.tesseract_digest}}
        new_images={'preview':{'image_id':new['image_id'],'digest':'sha256:'+'8'*64},
                    'tesseract-preview':{'image_id':new['tesseract_image_id'],'digest':'sha256:'+'7'*64}}
        self.releases=[{'tag_name':'preview-source-'+self.sha,'draft':False,'prerelease':True,
                        'body':self.module.body_text(old,self.registry_digest,self.tesseract_digest)},
                       {'tag_name':'preview-source-'+newer,'draft':False,'prerelease':True,
                        'body':self.module.body_text(new,new_images['preview']['digest'],new_images['tesseract-preview']['digest'])}]
        for advanced in ['preview','tesseract-preview']:
            with self.subTest(advanced=advanced):
                self.tags={**old_images,advanced:new_images[advanced]}
                with patch.object(self.module,'api',self.api),patch.object(self.module,'registry_image',self.registry):
                    state=self.module.fresh_state(self.plan,{},git.repo)
                self.assertEqual(state['preview_source_sha'],newer)
                current=self.module.policy.publication_plan('refs/heads/main',self.sha,'push',state,git.repo)
                self.assertEqual(current['aliases'],[])

    def test_legacy_core_preview_owner_can_be_migrated_without_a_terminal_alias(self):
        metadata={'format':1,'source_sha':self.sha,'manifest_sha256':self.plan['manifest_sha256'],
                  'image_id':self.image_id,'image_digest':self.registry_digest}
        self.releases=[{'tag_name':'preview-source-'+self.sha,'draft':False,'prerelease':True,
                        'body':self.module.MARKER+json.dumps(metadata)+' -->'}]
        self.tags['preview']={'digest':self.registry_digest,'image_id':self.image_id}
        with patch.object(self.module,'api',self.api),patch.object(self.module,'registry_image',self.registry):
            state=self.module.fresh_state(self.plan,{},self.f.git.repo)
        self.assertEqual(state['preview_source_sha'],self.sha)

    def test_fresh_service_state_prevents_late_old_preview_rollback(self):
        git = self.f.git
        git.git('-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.test',
                'commit', '--allow-empty', '-m', 'newer')
        newer = git.git('rev-parse', 'HEAD')
        git.git('update-ref', 'refs/remotes/origin/main', newer)
        git.git('checkout', '--detach', self.sha)
        self.remote_head = newer
        owner_plan = {**self.plan, 'source_sha': newer, 'version': 'preview-'+newer[:12],
                      'image_id': 'sha256:'+'c'*64}
        owner = {'digest': 'sha256:'+'e'*64, 'image_id': owner_plan['image_id']}
        self.tags['preview'] = owner.copy()
        self.releases.append({'id': 101, 'tag_name': 'preview-'+newer, 'draft': False,
                              'prerelease': True, 'body': self.module.body_text(owner_plan, owner['digest'],self.tesseract_digest)})
        self.assertEqual(self.plan['aliases'], ['preview'])
        result = self.run_writer(apply=True)
        self.assertEqual(result['aliases'], [])
        self.assertEqual(self.tags['preview'], owner)
        self.assertNotIn('push-preview', self.events)

    def test_source_only_release_is_reported_incomplete_without_relabelling_draft(self):
        self.releases.append({'id': 101, 'tag_name': 'v2.0.0', 'draft': False, 'prerelease': False,
                              'body': self.module.body_text({**self.plan, 'version': 'v2.0.0'})})
        with patch.object(self.module, 'api', self.api), patch.object(self.module, 'registry_image', self.registry):
            state = self.module.fresh_state(self.plan, {})
        self.assertFalse(state['releases'][0]['publication_complete'])
        self.assertFalse(state['releases'][0]['draft'])
        self.assertNotIn('publication_complete', self.releases[0])

    def test_live_repository_or_package_visibility_mismatch_fails_before_writes(self):
        for field in ['visibility', 'package_visibility']:
            with self.subTest(field=field), self.assertRaises(ValueError):
                setattr(self, field, 'public')
                self.run_writer(apply=True)
            setattr(self, field, 'private')
        self.assertEqual(self.events, [])

    def test_failed_current_checks_are_not_replaced_by_an_approval(self):
        self.check_result = 'failure'
        with self.assertRaises(ValueError):
            self.run_writer(apply=True)
        self.assertEqual(self.events, [])

    def test_new_failed_checks_during_upload_prevent_publication(self):
        self.fail_checks_after_upload = True
        with self.assertRaises(ValueError):
            self.run_writer(apply=True)
        self.assertTrue(self.releases[0]['draft'])
        self.assertEqual(self.tags, {})

    def test_stable_latest_waits_for_registry_alias_verification(self):
        self.ref = 'refs/tags/v1.10.0'
        self.f.git.git('tag', 'v1.10.0')
        self.f.set_identity({**self.f.candidate.version, 'version': 'v1.10.0', 'revision': self.sha})
        self.f.candidate.output = self.f.candidate.root/'stable-candidate'
        receipt = self.f.candidate.run_stage()
        self.candidate = self.f.candidate.output
        self.f.approval['manifest_sha256'] = receipt['manifest_sha256']
        raw = (json.dumps(self.f.approval)+'\n').encode()
        self.f.approval_path.write_bytes(raw)
        self.plan = self.f.module.prepare_publication(self.candidate, self.f.approval_path,
            hashlib.sha256(raw).hexdigest(), self.ref, self.sha, 'push',
            {'releases': [], 'image_tags': {'core': [], 'tesseract': []}, 'preview_source_sha': None}, self.f.git.repo)
        self.path.write_text(json.dumps(self.plan))
        self.plan_hash = hashlib.sha256(self.path.read_bytes()).hexdigest()
        self.env['GITHUB_REF'] = self.ref
        self.run_writer(apply=True)
        self.assertEqual(self.events[-3:], ['push-latest','push-tesseract-latest', 'finalize-release'])
        self.assertEqual(self.releases[0]['make_latest'], 'true')
        self.assertFalse(self.releases[0]['prerelease'])

    def test_changed_plan_or_asset_fails_before_service_writes(self):
        self.path.write_text('{}')
        with self.assertRaises(ValueError):
            self.run_writer(apply=True)
        self.assertEqual(self.events, [])

    def test_apply_requires_enabled_push_workflow_context(self):
        self.env['SCANNER_PUBLISH_ENABLED'] = 'false'
        with self.assertRaises(ValueError):
            self.run_writer(apply=True)
        self.assertEqual(self.events, [])

    def test_registry_authentication_failure_is_not_an_absent_tag(self):
        result = subprocess.CompletedProcess([], 1, b'', b'unauthorized: authentication required')
        with patch.object(self.module, 'command', return_value=result), self.assertRaises(RuntimeError):
            self.module.registry_image('preview', {})

    def test_registry_not_found_and_single_platform_digest(self):
        missing = subprocess.CompletedProcess([], 1, b'', b'no such manifest: ghcr.io/otama-homelab/scanner-kiosk:preview')
        with patch.object(self.module, 'command', return_value=missing):
            self.assertIsNone(self.module.registry_image('preview', {}))
        raw = json.dumps({'Descriptor': {'digest': self.registry_digest},
                          'SchemaV2Manifest': {'config': {'digest': self.image_id}}}).encode()
        found = subprocess.CompletedProcess([], 0, raw, b'')
        with patch.object(self.module, 'command', return_value=found):
            self.assertEqual(self.module.registry_image('preview', {}),
                             {'digest': self.registry_digest, 'image_id': self.image_id})

    def test_github_authentication_failure_is_not_an_absent_package(self):
        denied = subprocess.CompletedProcess([], 1, b'', b'gh: Resource not accessible (HTTP 403)')
        with patch.object(self.module, 'command', return_value=denied), self.assertRaises(RuntimeError):
            self.module.api('orgs/otama-homelab/packages/container/scanner-kiosk', {}, missing=True)


class PublicationDiagnosticsTest(unittest.TestCase):
    def setUp(self):
        spec = importlib.util.spec_from_file_location('publication_diagnostics', Path(__file__).with_name('write-publication.py'))
        self.module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.module)

    def test_failed_command_keeps_reason_without_credentials_or_signed_urls(self):
        token = 'fixture-secret-token'
        result = subprocess.CompletedProcess([], 1, b'',
            ('denied: '+token+' https://registry.example/blob?sig=private\n'+'x'*4096).encode())
        with patch.object(self.module.subprocess, 'run', return_value=result), self.assertRaises(RuntimeError) as raised:
            self.module.command(['docker', 'push', 'fixture'], {'GH_TOKEN': token}, input_data=b'private-stdin')
        message = str(raised.exception)
        self.assertIn('docker push failed with exit 1: denied:', message)
        for secret in [token, 'registry.example', 'sig=private', 'private-stdin']:
            self.assertNotIn(secret, message)
        self.assertLess(len(message), 2100)

    def test_cli_preserves_sanitized_failure_reason_and_stop_instruction(self):
        args = ['writer', '--candidate', '/candidate', '--plan', '/plan', '--plan-sha256', '0'*64,
                '--ref', 'refs/tags/v0.2.25', '--sha', '0'*40, '--event', 'push']
        output = io.StringIO()
        with patch('sys.argv', args), patch.object(self.module, 'publish', side_effect=RuntimeError('docker push failed')), contextlib.redirect_stderr(output), self.assertRaises(SystemExit) as raised:
            self.module.main()
        self.assertEqual(raised.exception.code, 1)
        self.assertIn('docker push failed', output.getvalue())
        self.assertIn('never overwrite it', output.getvalue())


if __name__ == '__main__':
    unittest.main()
