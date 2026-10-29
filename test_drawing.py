"""Reference inputs and rejected takes must not become silent identity/approval shortcuts."""
import copy
import hashlib
import io
from pathlib import Path
import queue
import tempfile
import unittest

import server
from test_server import PNG


class DrawingChecks(unittest.TestCase):
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory()
        self.store=server.Store(self.tmp.name)
        self.p=self.store.new({'title':'Pilot','music_video':True,'look':'Illustrated stage.',
            'continuity':'UNRELATED CHORUS STORY',
            'shots':[{'id':'s','title':'Chair','local_prompt':'One drawn man beside flat cardboard cutouts.',
                      'local_negative_prompt':'conference table, photograph'}]})
        self.pid=self.p['id'];self.sid='s'
        self.renderer=object.__new__(server.Renderer)
        self.renderer.store=self.store;self.renderer.queue=queue.Queue()

    def tearDown(self):
        self.tmp.cleanup()

    def asset(self,kind='sketch'):
        p=self.store.import_asset(self.pid,self.sid,kind,'input.png',io.BytesIO(PNG),len(PNG))
        return p,p['shots'][0]['assets'][-1]['id']

    def test_drawing_prompt_excludes_story_and_music_requires_explicit_visual_prompt(self):
        package=server.prompt_package(self.p,self.p['shots'][0])
        self.assertNotIn('UNRELATED CHORUS',package['local_draft'])
        self.assertEqual(self.renderer.submit(self.pid,self.sid)['negative_prompt'],'conference table, photograph')
        p=self.store.load(self.pid);p['shots'][0]['local_prompt']=''
        self.store.update(self.pid,{'revision':p['revision'],'shots':p['shots']})
        with self.assertRaisesRegex(ValueError,'short visual'):
            self.renderer.submit(self.pid,self.sid)

    def test_image_job_snapshots_actual_bytes_and_offline_cli_receives_them(self):
        p,aid=self.asset()
        j=self.renderer.submit(self.pid,self.sid,mode='image',image_id=aid,strength=.4,revision=p['revision'])
        input_path=self.store.folder(self.pid)/j['input_image']['snapshot_path']
        self.assertEqual(input_path.read_bytes(),PNG)
        cmd=server.Renderer.command(j,input_path.parent)
        self.assertEqual(cmd[cmd.index('--image')+1],str(input_path))
        self.assertEqual(cmd[cmd.index('--strength')+1],'0.4')
        self.assertEqual(j['input_image']['sha256'],hashlib.sha256(PNG).hexdigest())
        self.assertTrue({'--local','--offline','--no-download-missing'}.issubset(cmd))
        self.assertNotIn('--cloud-compute',cmd)

    def test_shared_reference_is_actual_image_input_not_just_a_name(self):
        image=Path(self.tmp.name)/'reference.png';image.write_bytes(PNG)
        p=self.store.attach_visual_reference(self.pid,image,'Cast','Appearance reference')
        ref=p['visual_references'][0]
        j=self.renderer.submit(self.pid,self.sid,'image',ref['id'],.3)
        self.assertEqual(j['input_image']['id'],ref['id'])
        self.assertEqual((self.store.folder(self.pid)/j['input_image']['snapshot_path']).read_bytes(),PNG)
        self.assertIn('not a character identity',j['input_image']['use'])

    def test_missing_binned_or_tampered_input_is_rejected(self):
        for image_id in ('../outside','unknown'):
            with self.assertRaises(ValueError):self.renderer.submit(self.pid,self.sid,'image',image_id)
        p,aid=self.asset();p=self.store.bin_asset(self.pid,self.sid,aid,p['revision'])
        with self.assertRaises(ValueError):self.renderer.submit(self.pid,self.sid,'image',aid)
        p=self.store.bin_asset(self.pid,self.sid,aid,p['revision'],True)
        (self.store.folder(self.pid)/p['shots'][0]['assets'][0]['path']).write_bytes(PNG+b'changed')
        with self.assertRaisesRegex(ValueError,'changed'):self.renderer.submit(self.pid,self.sid,'image',aid)

    def test_reference_path_cannot_escape_project(self):
        p,aid=self.asset();p['shots'][0]['assets'][0]['path']='../../reference.png'
        self.store.save(p,p['revision'])
        with self.assertRaises(ValueError):self.renderer.submit(self.pid,self.sid,'image',aid)

    def test_strength_modes_and_revision_are_validated_before_queueing(self):
        for strength in (-.1,1,float('nan'),float('inf'),True,'0.4'):
            with self.assertRaises(ValueError):self.renderer.submit(self.pid,self.sid,strength=strength)
        with self.assertRaises(ValueError):self.renderer.submit(self.pid,self.sid,'image')
        with self.assertRaises(ValueError):self.renderer.submit(self.pid,self.sid,'text','unknown')
        with self.assertRaises(RuntimeError):self.renderer.submit(self.pid,self.sid,revision=0)
        self.assertEqual(self.renderer.queue.qsize(),0)

    def test_bin_retains_file_provenance_and_restore_does_not_select(self):
        p,aid=self.asset();p=self.store.choose(self.pid,self.sid,'sketch',aid)
        path=self.store.folder(self.pid)/p['shots'][0]['assets'][0]['path']
        p=self.store.bin_asset(self.pid,self.sid,aid,p['revision'],reason='Wrong character and layout')
        self.assertIsNone(p['shots'][0]['selected_sketch'])
        self.assertIsNone(server.direction_image(p['shots'][0]))
        self.assertEqual(path.read_bytes(),PNG)
        with self.assertRaises(ValueError):self.store.choose(self.pid,self.sid,'sketch',aid)
        with self.assertRaises(RuntimeError):self.store.bin_asset(self.pid,self.sid,aid,0,True)
        p=self.store.bin_asset(self.pid,self.sid,aid,p['revision'],True)
        self.assertIsNone(p['shots'][0]['selected_sketch'])
        self.assertEqual(len(p['shots'][0]['assets'][0]['bin_history']),2)

    def test_binning_selected_starter_revokes_approval_and_handoff_image(self):
        p,aid=self.asset('keyframe');p=self.store.choose(self.pid,self.sid,'keyframe',aid,True)
        self.assertTrue(p['shots'][0]['animation_ready'])
        p=self.store.bin_asset(self.pid,self.sid,aid,p['revision'])
        self.assertFalse(p['shots'][0]['animation_ready'])
        self.assertIsNone(server.direction_image(p['shots'][0]))
        self.assertIn('no supplied image',server.claude_handoff(p))


if __name__=='__main__':unittest.main()
