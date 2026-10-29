import copy
import io
import json
from pathlib import Path
import tempfile
import threading
import unittest
import urllib.request
import urllib.error
import zipfile
from unittest.mock import patch

import server

PNG = b'\x89PNG\r\n\x1a\n' + b'fixture-not-a-render'


class StoreChecks(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.store = server.Store(self.temp.name)
        self.p = self.store.new(json.loads((server.APP/'cowboy.json').read_text(encoding='utf-8')))
        self.pid = self.p['id']
        self.sid = self.p['shots'][0]['id']

    def tearDown(self):
        self.temp.cleanup()

    def asset(self, kind='sketch'):
        p = self.store.import_asset(self.pid, self.sid, kind, 'image.png', io.BytesIO(PNG), len(PNG))
        return p, p['shots'][0]['assets'][-1]['id']

    def test_media_and_selection_survive_restart(self):
        p, aid = self.asset()
        self.store.choose(self.pid, self.sid, 'sketch', aid)
        reopened = server.Store(self.temp.name).load(self.pid)
        self.assertEqual(reopened['shots'][0]['selected_sketch'], aid)
        self.assertEqual((self.store.folder(self.pid)/reopened['shots'][0]['assets'][0]['path']).read_bytes(), PNG)

    def test_reorder_changes_export_order_and_preserves_identity(self):
        p, aid = self.asset()
        p = self.store.choose(self.pid, self.sid, 'sketch', aid)
        ids = [s['id'] for s in p['shots']]
        p = self.store.update(self.pid, {'revision':p['revision'], 'shots':list(reversed(p['shots']))})
        self.assertEqual(p['shots'][-1]['id'], self.sid)
        self.assertEqual(p['shots'][-1]['selected_sketch'], aid)
        package = self.store.folder(self.pid)/self.store.export(self.pid)
        with zipfile.ZipFile(package) as z:
            seq = json.loads(z.read('sequence.json'))
            self.assertEqual([s['shot_id'] for s in seq], list(reversed(ids)))
            self.assertTrue(any(n.startswith('selected/0030_'+self.sid) for n in z.namelist()))
            self.assertFalse(any('clip' in s['files'] for s in seq))

    def test_keyframe_approval_invalidates_on_visual_changes(self):
        p, aid = self.asset('keyframe')
        p = self.store.choose(self.pid,self.sid,'keyframe',aid,True)
        self.assertTrue(p['shots'][0]['animation_ready'])
        p = self.store.update(self.pid, {'revision':p['revision'], 'look':'Different wardrobe'})
        self.assertFalse(p['shots'][0]['animation_ready'])
        p = self.store.choose(self.pid,self.sid,'keyframe',aid,True)
        p['shots'][0]['continuity'] = 'Different hand'
        p = self.store.update(self.pid, {'revision':p['revision'], 'shots':p['shots']})
        self.assertFalse(p['shots'][0]['animation_ready'])

    def test_action_change_does_not_invalidate_starting_image(self):
        p, aid = self.asset('keyframe')
        p = self.store.choose(self.pid,self.sid,'keyframe',aid,True)
        p['shots'][0]['action'] = 'Make the gesture slower.'
        p = self.store.update(self.pid, {'revision':p['revision'], 'shots':p['shots']})
        self.assertTrue(p['shots'][0]['animation_ready'])

    def test_imported_plan_cannot_replace_assets_or_delete_media_shots(self):
        p, aid = self.asset()
        imported = copy.deepcopy(p['shots'])
        imported[0]['assets'] = []
        p = self.store.update(self.pid, {'revision':p['revision'], 'shots':imported})
        self.assertEqual(p['shots'][0]['assets'][0]['id'], aid)
        with self.assertRaises(ValueError):
            self.store.update(self.pid, {'revision':p['revision'], 'shots':p['shots'][1:]})

    def test_stale_revision_and_path_traversal_rejected(self):
        self.store.update(self.pid, {'revision':self.p['revision'], 'brief':'Updated'})
        with self.assertRaises(RuntimeError):
            self.store.update(self.pid, {'revision':self.p['revision'], 'brief':'Old'})
        with self.assertRaises(ValueError):
            self.store.load('../../private')

    def test_interrupted_or_mislabelled_upload_preserves_project(self):
        for raw, length in [(b'bad',10),(b'not a png',9)]:
            with self.assertRaises(ValueError):
                self.store.import_asset(self.pid,self.sid,'sketch','bad.png',io.BytesIO(raw),length)
        self.assertFalse(self.store.load(self.pid)['shots'][0]['assets'])
        self.assertFalse(list(self.store.root.rglob('*.part')))

    def test_prompt_research_and_provider_packet(self):
        p = self.store.load(self.pid)
        prompt = server.prompt_package(p,p['shots'][0],'flow')
        self.assertFalse(prompt['animation_ready'])
        self.assertIn('Flow',prompt['animation'])
        self.assertIn('SUBJECT ACTION AND TIMING',prompt['animation'])
        self.assertIn('CAMERA:',prompt['animation'])
        self.assertIn('archive.org',server.buckley_intake(p))

    def test_short_local_prompt_is_saved_and_used_without_narrative_notes(self):
        p=self.store.apply_plan(self.pid,{'shots':[{'id':self.sid,'local_prompt':'Anime airport. Foreground hand holds a boarding pass.'}]},self.p['revision'])
        p=self.store.load(self.pid)
        package=server.prompt_package(p,p['shots'][0])
        self.assertEqual(package['local_draft'],"Native 16:9. Anime airport. Foreground hand holds a boarding pass.")
        self.assertIn('SHARED CONTINUITY',package['composition'])
        self.assertNotIn('SHARED CONTINUITY',package['local_draft'])
        renderer=object.__new__(server.Renderer);renderer.store=self.store;renderer.queue=server.queue.Queue()
        job=renderer.submit(self.pid,self.sid)
        self.assertEqual(job['prompt'],package['local_draft'])
        self.assertEqual((job['width'],job['height']),(1024,576))

    def test_legacy_local_prompt_falls_back_and_does_not_change_direction_basis(self):
        p=self.store.load(self.pid);s=p['shots'][0]
        before=server.direction_basis(p,s)
        s.pop('local_prompt',None)
        self.assertEqual(before,server.direction_basis(p,s))
        package=server.prompt_package(p,s)
        self.assertIn(s['composition'],package['local_draft'])
        self.assertNotIn('SHARED CONTINUITY',package['local_draft'])

    def test_duplicate_ids_and_script_reference_rejected(self):
        raw=json.loads((server.APP/'cowboy.json').read_text(encoding='utf-8'))
        raw['shots'][1]['id']=raw['shots'][0]['id']
        with self.assertRaises(ValueError):self.store.new(raw)
        with self.assertRaises(ValueError):self.store.update(self.pid,{'revision':self.p['revision'],'references':[{'url':'javascript:alert(1)'}]})

    def test_compact_local_plan_merges_without_losing_other_shots(self):
        p, aid = self.asset()
        p = self.store.apply_plan(self.pid, {'shots':[{'id':self.sid,'action':'Lift the cigarette slowly.'}]}, p['revision'])
        self.assertEqual(len(p['shots']),3)
        self.assertEqual(p['shots'][0]['action'],'Lift the cigarette slowly.')
        self.assertEqual(p['shots'][0]['assets'][0]['id'],aid)
        self.assertEqual(p['shots'][1]['title'],'A small gesture')

    def test_ten_minute_episode_can_be_saved_reordered_and_exported(self):
        p=self.store.new({'title':'Ten-minute episode',
                          'shots':[{'id':f'ep-{i:03d}','title':f'Beat {i}','duration':5} for i in range(120)]})
        p=self.store.update(p['id'],{'revision':p['revision'],'shots':list(reversed(p['shots']))})
        self.assertEqual(p['shots'][0]['id'],'ep-119')
        with zipfile.ZipFile(self.store.folder(p['id'])/self.store.export(p['id'])) as z:
            sequence=json.loads(z.read('sequence.json'))
            prompt_names=[n for n in z.namelist() if n.startswith('prompts/')]
            self.assertEqual(prompt_names,sorted(prompt_names))
        self.assertEqual(len(sequence),120)
        self.assertEqual(sum(s['target_seconds'] for s in sequence),600)
        self.assertEqual(sequence[0]['shot_id'],'ep-119')

    def cloud_review(self, p, verdict='ready_for_review'):
        s=p['shots'][0]
        return {'shots':[{'id':s['id'],'basis':server.direction_basis(p,s),
                         'image_inspected':True,
                         'action':'Lift the right hand slowly, pause, then lower it.',
                         'camera':'Fixed seated eye-level camera.',
                         'grok_prompt':'Animate the attached image with one slow hand gesture. Keep the camera fixed.',
                         'verdict':verdict,'image_fixes':'','reference_application':'Borrow unhurried posture from the Archive reference.'}]}

    def test_claude_packet_includes_real_files_and_provisional_labels(self):
        p,aid=self.asset()
        text=server.claude_handoff(p)
        self.assertIn('not an approved starter',text)
        self.assertIn('archive.org/details/Smoking_Cowboy',text)
        self.assertIn(server.direction_basis(p,p['shots'][0]),text)
        self.assertIn('frames/'+self.sid+'.png',text)
        result=self.store.export_claude(self.pid)
        path=self.store.folder(self.pid)/result['url'].split('/'+self.pid+'/',1)[1]
        with zipfile.ZipFile(path) as z:
            self.assertEqual(z.read('frames/'+self.sid+'.png'),PNG)
            self.assertIn('CLAUDE_HANDOFF.txt',z.namelist())
            self.assertNotIn('project.json',z.namelist())

    def test_single_shot_handoff_uses_full_project_basis_and_only_its_frame(self):
        p,aid=self.asset('keyframe')
        p=self.store.choose(self.pid,self.sid,'keyframe',aid,True)
        target=p['shots'][0]
        packet=server.claude_handoff(p,shot_id=self.sid)
        context=json.loads(packet.split('PROJECT CONTEXT AND ATTACHMENT MAP\n',1)[1])
        self.assertEqual([s['id'] for s in context['shots']],[self.sid])
        self.assertEqual(context['shots'][0]['basis'],server.direction_basis(p,target))
        self.assertTrue(context['shots'][0]['starter_approved'])
        self.assertEqual(len(context['neighboring_shots']),1)
        result=self.store.export_claude(self.pid,shot_id=self.sid)
        out=self.store.folder(self.pid)/result['url'].split('/'+self.pid+'/',1)[1]
        with zipfile.ZipFile(out) as z:self.assertEqual([n for n in z.namelist() if n.startswith('frames/')],['frames/'+self.sid+'.png'])
        with self.assertRaises(ValueError):server.claude_handoff(p,shot_id='missing')
        with self.assertRaises(ValueError):server.claude_handoff(p,scene='scene',shot_id=self.sid)

    def test_claude_handoff_preserves_a_different_projects_tone_and_uncertainty(self):
        p=self.store.new({'title':'Mister Jorge — Episode 5 planning',
                          'brief':'An anxious, perceptive comic in an animated airport.',
                          'look':'Match the existing anime footage; source frames pending.',
                          'continuity':'Reported events and proposed developments remain separate.',
                          'shots':[{'id':'ep5-join','title':'Resume the existing cut'}]})
        packet=server.claude_handoff(p)
        self.assertIn('An anxious, perceptive comic',packet)
        self.assertIn("project's stated tone",packet)
        self.assertNotIn('quiet, unhurried mood',packet)
        self.assertNotIn('cigarette',packet)
        self.assertNotIn('Smoking_Cowboy',packet)
        self.assertIn('no supplied image; text-only planning',packet)
        self.assertIn('set verdict to needs_image_fix',packet)
        self.assertIn('Do not promote a proposal into established continuity',packet)

    def test_imported_grok_direction_is_used_for_bound_approved_image(self):
        p,aid=self.asset('keyframe')
        p=self.store.choose(self.pid,self.sid,'keyframe',aid,True)
        raw=self.cloud_review(p)
        p=self.store.apply_plan(self.pid,raw,p['revision'])
        package=server.prompt_package(p,p['shots'][0],'grok')
        self.assertTrue(package['optimized_direction_ready'])
        self.assertEqual(package['animation'],raw['shots'][0]['grok_prompt'])
        self.assertNotEqual(server.prompt_package(p,p['shots'][0],'flow')['animation'],raw['shots'][0]['grok_prompt'])
        self.assertEqual(p['shots'][0]['selected_keyframe'],aid)

    def test_cloud_direction_rejects_stale_source_without_overwriting(self):
        p,aid=self.asset('keyframe')
        p=self.store.choose(self.pid,self.sid,'keyframe',aid,True)
        raw=self.cloud_review(p)
        p=self.store.update(self.pid,{'revision':p['revision'],'look':'A changed visual treatment'})
        before=copy.deepcopy(p)
        with self.assertRaises(RuntimeError):self.store.apply_plan(self.pid,raw,p['revision'])
        self.assertEqual(self.store.load(self.pid),before)

    def test_direction_invalidates_after_brief_or_sequence_changes(self):
        p,aid=self.asset('keyframe')
        p=self.store.choose(self.pid,self.sid,'keyframe',aid,True)
        p=self.store.apply_plan(self.pid,self.cloud_review(p),p['revision'])
        self.assertTrue(server.optimized_direction_ready(p,p['shots'][0]))
        p=self.store.update(self.pid,{'revision':p['revision'],'shots':list(reversed(p['shots']))})
        s=next(s for s in p['shots'] if s['id']==self.sid)
        self.assertFalse(server.optimized_direction_ready(p,s))
        self.assertEqual(server.prompt_package(p,s)['direction_review_status'],'stale')

    def test_image_fix_verdict_never_auto_approves_a_draft(self):
        p,aid=self.asset()
        p=self.store.apply_plan(self.pid,self.cloud_review(p,'needs_image_fix'),p['revision'])
        s=p['shots'][0]
        self.assertFalse(s['animation_ready'])
        self.assertFalse(server.optimized_direction_ready(p,s))
        self.assertIsNone(s['selected_sketch'])
        self.assertIsNone(s['approval'])

    def test_uninspected_cloud_reply_cannot_enable_animation_direction(self):
        p,aid=self.asset('keyframe')
        p=self.store.choose(self.pid,self.sid,'keyframe',aid,True)
        raw=self.cloud_review(p)
        raw['shots'][0]['image_inspected']=False
        p=self.store.apply_plan(self.pid,raw,p['revision'])
        self.assertEqual(p['shots'][0]['direction_review']['verdict'],'needs_image_fix')
        self.assertFalse(server.optimized_direction_ready(p,p['shots'][0]))

    def test_portrait_change_and_shared_reference_are_preserved_and_exported(self):
        p,aid=self.asset('keyframe')
        p=self.store.choose(self.pid,self.sid,'keyframe',aid,True)
        p=self.store.update(self.pid,{'revision':p['revision'],'aspect':'9:16'})
        self.assertFalse(server.approved(p,p['shots'][0]))
        source=Path(self.temp.name)/'source.png';source.write_bytes(PNG)
        p=self.store.attach_visual_reference(self.pid,source,'Episode 5 character','User-supplied appearance reference; not a shot starter.')
        self.assertEqual(source.read_bytes(),PNG)
        self.assertEqual(len(p['visual_references']),1)
        self.assertEqual(p['shots'][0]['selected_keyframe'],aid)
        self.assertIn('Vertical 9:16 portrait',server.prompt_package(p,p['shots'][0])['composition'])
        result=self.store.export_claude(self.pid)
        out=self.store.folder(self.pid)/result['url'].split('/'+self.pid+'/',1)[1]
        with zipfile.ZipFile(out) as z:
            names=[n for n in z.namelist() if n.startswith('references/')]
            self.assertTrue(any(z.read(n)==PNG for n in names))
            context=json.loads(z.read('CLAUDE_HANDOFF.txt').decode().split('PROJECT CONTEXT AND ATTACHMENT MAP\n',1)[1])
            self.assertTrue(any(a['kind']=='character / style reference, not a shot starter' for a in context['attachments']))

    def test_portrait_is_default_without_changing_explicit_landscape(self):
        p=self.store.new({'title':'Portrait','shots':[{'title':'One'}]})
        self.assertEqual(p['aspect'],'9:16')
        self.assertEqual(self.p['aspect'],'16:9')

    def test_reusable_template_creates_separate_fresh_projects(self):
        raw=json.loads((server.APP/'templates/storyboard-project-template.json').read_text(encoding='utf-8'))
        p=self.store.new(raw);q=self.store.new(raw)
        self.assertNotEqual(p['id'],q['id'])
        self.assertEqual(len(p['shots']),3)
        self.assertEqual(p['aspect'],'9:16')
        self.assertTrue(all(not s['assets'] and not s['approval'] and not s['selected_clip'] for s in p['shots']))
        self.assertEqual(self.store.load(self.pid)['title'],self.p['title'])
        self.assertEqual(server.DRAFT_SIZES['9:16'],(576,1024))

    def test_episode_detail_survives_plan_import_and_scene_export(self):
        p=self.store.new({'title':'Episode pilot','runtime_limit_seconds':600,'episode_notes':'Good deed opens portal.',
            'shots':[{'id':'a','title':'Avoid','scene':'Airport','duration':8,'dialogue':'Keep walking.',
                      'start_state':'At gate','end_state':'Sees traveler','audience':'Recognizes fear','blocking':'terminal'},
                     {'id':'b','title':'Help','scene':'Good deed','duration':12,'anomaly':'Portal after help'}]})
        p=self.store.apply_plan(p['id'],{'shots':[{'id':'a','narration':'Everything is normal.'}]},p['revision'])
        self.assertEqual(p['shots'][0]['dialogue'],'Keep walking.')
        self.assertEqual(p['shots'][0]['start_state'],'At gate')
        prompts=server.prompt_package(p,p['shots'][0])
        self.assertIn('At gate',prompts['composition'])
        self.assertNotIn('END STATE: Sees traveler',prompts['composition'])
        self.assertIn('END STATE: Sees traveler',prompts['animation'])
        packet=server.claude_handoff(p,'Good deed')
        context=json.loads(packet.split('PROJECT CONTEXT AND ATTACHMENT MAP\n',1)[1])
        self.assertEqual([s['id'] for s in context['shots']],['b'])
        self.assertEqual(context['shots'][0]['basis'],server.direction_basis(p,p['shots'][1]))
        self.assertEqual(len(context['episode_map']),2)
        with zipfile.ZipFile(self.store.folder(p['id'])/self.store.export(p['id'])) as z:
            self.assertIn('DIALOGUE: Keep walking.',z.read('STORYBOARD.txt').decode())
            self.assertEqual(json.loads(z.read('episode-map.json'))[-1]['end_seconds'],20)


class HTTPChecks(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.store=server.Store(self.temp.name)
        p=self.store.new({'title':'Test','shots':[{'id':'test','title':'Test'}]})
        self.pid=p['id']
        self.httpd=server.LocalHTTPServer(('127.0.0.1',0),server.Handler)
        self.httpd.store=self.store;self.httpd.token='test-token'
        self.thread=threading.Thread(target=self.httpd.serve_forever,daemon=True);self.thread.start()
        self.url='http://127.0.0.1:'+str(self.httpd.server_port)
        self.opener=urllib.request.build_opener(urllib.request.ProxyHandler({}))

    def tearDown(self):
        self.httpd.shutdown();self.httpd.server_close();self.thread.join();self.temp.cleanup()

    def test_loopback_bind_does_not_require_reverse_dns(self):
        with patch.object(server.socket, 'getfqdn', side_effect=AssertionError('DNS is unavailable')):
            listener = server.LocalHTTPServer(('127.0.0.1', 0), server.Handler)
            try:
                self.assertEqual(listener.server_name, '127.0.0.1')
                self.assertEqual(listener.server_port, listener.socket.getsockname()[1])
            finally:
                listener.server_close()

    def test_mutations_require_token_and_same_origin(self):
        for headers in [{},{'X-Storyboard-Token':'test-token','Origin':'https://elsewhere.example'}]:
            req=urllib.request.Request(self.url+'/api/new',data=b'{}',headers=headers)
            with self.assertRaises(urllib.error.HTTPError) as e:self.opener.open(req)
            self.assertEqual(e.exception.code,403)

    def test_video_byte_ranges_and_traversal(self):
        p=self.store.folder(self.pid)/'media'/'test';p.mkdir(parents=True)
        data=b'0123456789abcdefgh';(p/'test.mp4').write_bytes(data)
        req=urllib.request.Request(self.url+f'/media/{self.pid}/media/test/test.mp4',headers={'Range':'bytes=4-9'})
        with self.opener.open(req) as r:
            self.assertEqual(r.status,206);self.assertEqual(r.read(),data[4:10])
            self.assertEqual(r.headers['Content-Range'],'bytes 4-9/18')
        req=urllib.request.Request(self.url+f'/media/{self.pid}/media/test/test.mp4',headers={'Range':'bytes=-3'})
        with self.opener.open(req) as r:self.assertEqual(r.read(),data[-3:])
        with self.assertRaises(urllib.error.HTTPError):self.opener.open(self.url+f'/media/{self.pid}/media/%2e%2e/%2e%2e/project.json')

    def test_start_page_and_health(self):
        with self.opener.open(self.url+'/') as r:
            page=r.read()
            self.assertIn(b'Storyboard Desk',page);self.assertIn("frame-ancestors 'none'",r.headers['Content-Security-Policy'])
        import re
        for script in re.findall(rb'<script[^>]+src="([^"]+)"',page):
            with self.opener.open(self.url+script.decode()) as r:
                self.assertEqual(r.status,200)
                self.assertIn('javascript',r.headers['Content-Type'])
        with self.opener.open(self.url+'/api/health') as r:self.assertEqual(json.load(r)['app'],'Storyboard Desk')

    def test_missing_video_tools_block_prepare_before_provider_work(self):
        with patch.object(server, 'require_video_tools', side_effect=ValueError('Install FFmpeg')):
            req=urllib.request.Request(self.url+'/api/cloud-prepare',data=b'{"kind":"video"}',headers={'X-Storyboard-Token':'test-token'})
            with self.assertRaises(urllib.error.HTTPError) as error:self.opener.open(req)
            self.assertEqual(error.exception.code,400)
            self.assertIn('Install FFmpeg',json.loads(error.exception.read())['error'])

    def test_malformed_json_returns_error(self):
        req=urllib.request.Request(self.url+'/api/new',data=b'[]',headers={'X-Storyboard-Token':'test-token'})
        with self.assertRaises(urllib.error.HTTPError) as e:self.opener.open(req)
        self.assertEqual(e.exception.code,400)

    def test_single_shot_claude_endpoint_preserves_scope(self):
        p=self.store.load(self.pid)
        p=self.store.update(self.pid,{'revision':p['revision'],'shots':p['shots']+[{'id':'other','title':'Other shot'}]})
        req=urllib.request.Request(self.url+'/api/claude',data=json.dumps({'project':self.pid,'shot':'test','text_only':True}).encode(),headers={'X-Storyboard-Token':'test-token'})
        with self.opener.open(req) as r:text=json.load(r)['text']
        context=json.loads(text.split('PROJECT CONTEXT AND ATTACHMENT MAP\n',1)[1])
        self.assertEqual([s['id'] for s in context['shots']],['test'])
        self.assertEqual(context['shots'][0]['basis'],server.direction_basis(p,p['shots'][0]))


if __name__=='__main__':unittest.main()
