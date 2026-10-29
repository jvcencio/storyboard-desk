import base64,io,json,tempfile,unittest
from pathlib import Path
import cloud,server
PNG=b'\x89PNG\r\n\x1a\n'+b'fixture'
VIDEO=b'\x00\x00\x00\x20ftypisom'+b'fixture'
class Fake:
    def __init__(self):self.images=[];self.videos=[];self.polls=[];self.cost=.18
    def image(self,b):self.images.append(b);return {'data':[{'media_type':'image/png','b64_json':base64.b64encode(PNG).decode()}],'usage':{'cost':self.cost}}
    def video(self,b):self.videos.append(b);return {'id':'provider-123','status':'pending'}
    def poll(self,r):self.polls.append(r);return {'status':'completed','usage':{'cost':.64},'urls':['https://private.invalid/signed']}
    def download(self,r):return VIDEO
class CloudChecks(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.store=server.Store(self.temp.name)
        p=self.store.new({'title':'Cloud test','music_video':True,'aspect':'16:9','shots':[{'id':'one','title':'One','composition':'A seated man.'},{'id':'two','title':'Two'}]});self.pid=p['id']
        p=self.store.import_asset(self.pid,'one','keyframe','a.png',io.BytesIO(PNG),len(PNG));self.aid=p['shots'][0]['assets'][0]['id'];self.fake=Fake();self.e=self.engine();self.e.allowance(self.pid,5)
    def tearDown(self):self.temp.cleanup()
    def engine(self):
        e=cloud.CloudEngine(self.store,server.approved,server.direction_basis,server.prompt_package,client=self.fake,probe=lambda p,f:{'duration_seconds':8,'audio_tracks':0},start_worker=False);e.connected=True;return e
    def image(self,**extra):
        p=self.store.load(self.pid);r={'project':self.pid,'shot':'one','revision':p['revision'],'kind':'image','model':cloud.IMAGE_MODELS[0],'prompt':'One scene, same face.','reference_ids':[self.aid]};r.update(extra);return self.e.prepare(r)
    def video(self,**extra):
        p=self.store.choose(self.pid,'one','keyframe',self.aid,True);r={'project':self.pid,'shot':'one','revision':p['revision'],'kind':'video','model':cloud.VIDEO_MODELS[0],'prompt':'One slow nod.','duration':8,'resolution':'720p'};r.update(extra);return self.e.prepare(r)
    def start(self,q):return self.e.jobs[self.e.submit(q['id'],True)['id']]
    def test_free_preflight_and_exact_snapshot(self):
        q=self.image();self.assertFalse(self.fake.images);self.assertEqual(self.e.spent(self.pid),0)
        j=self.start(q);b=self.e.body(j);self.assertIn('INPUT IMAGE GUIDE',b['prompt']);self.assertTrue(b['input_references'][0]['image_url']['url'].startswith('data:image/png;base64,'));self.assertNotIn('Authorization',json.dumps(j))
    def test_budget_confirmation_and_connection_gates(self):
        q=self.image();self.e.allowance(self.pid,0)
        with self.assertRaises(ValueError):self.e.submit(q['id'],True)
        self.e.allowance(self.pid,5)
        with self.assertRaises(ValueError):self.e.submit(q['id'])
        self.e.connected=False
        with self.assertRaises(ValueError):self.e.submit(q['id'],True)
        self.assertFalse(self.e.jobs)
    def test_double_click_and_import_are_idempotent(self):
        q=self.image();j=self.start(q);again=self.e.submit(q['id'],True);self.assertEqual(j['id'],again['id']);self.e.process(j);self.e.process(j)
        p=self.store.load(self.pid);self.assertEqual(len(self.fake.images),1);self.assertEqual(len(p['shots'][0]['assets']),2);self.assertIsNone(p['shots'][0]['selected_keyframe']);self.assertEqual(self.e.spent(self.pid),.18);self.assertEqual(p['shots'][0]['assets'][-1]['cloud_generation']['prompt'],q['prompt'])
    def test_video_needs_approved_starter_and_audio_off(self):
        p=self.store.load(self.pid)
        with self.assertRaises(ValueError):self.e.prepare({'project':self.pid,'shot':'one','revision':p['revision'],'kind':'video','model':cloud.VIDEO_MODELS[0],'prompt':'Nod'})
        q=self.video();j=self.start(q);self.e.process(j);b=self.fake.videos[0];self.assertIs(b['generate_audio'],False);self.assertEqual(q['reserved_usd'],.64);self.assertEqual(b['frame_images'][0]['frame_type'],'first_frame')
        a=self.store.load(self.pid)['shots'][0]['assets'][-1];self.assertEqual(a['keyframe_id'],self.aid);self.assertEqual(a['audio_tracks'],0);self.assertEqual(a['duration_seconds'],8);self.assertIsNone(self.store.load(self.pid)['shots'][0]['selected_clip'])
    def test_invalid_params_and_model_fail_before_jobs(self):
        for settings in [{'duration':5},{'duration':True},{'resolution':'360p'},{'model':'untrusted/other'}]:
            with self.assertRaises(ValueError):self.video(**settings)
        for settings in [{'prompt':None},{'reference_ids':['bad']},{'reference_ids':[self.aid]*7}]:
            with self.assertRaises(ValueError):self.image(**settings)
        self.assertFalse(self.e.jobs)
    def test_stale_project_and_changed_catalog_rejected(self):
        q=self.image();p=self.store.load(self.pid);self.store.update(self.pid,{'revision':p['revision'],'look':'Changed'})
        with self.assertRaises(RuntimeError):self.e.submit(q['id'],True)
        q=self.image();next(m for m in self.e.catalog['images'] if m['id']==q['model'])['changed']=True
        with self.assertRaises(RuntimeError):self.e.submit(q['id'],True)
    def test_changed_reference_bytes_rejected(self):
        q=self.image();(self.store.folder(self.pid)/q['refs'][0]['path']).write_bytes(PNG+b'changed')
        with self.assertRaises(ValueError):self.e.submit(q['id'],True)
    def test_brief_content_approval_gate(self):
        c={'title':'Working brief','shots':[]};cloud.atomic(self.store.folder(self.pid)/'cloud/production-brief.json',{'content':c,'sha256':cloud.digest(c)})
        q=self.image()
        with self.assertRaises(ValueError):self.e.submit(q['id'],True)
        self.e.approve_brief(self.pid,cloud.digest(c));j=self.start(q);self.assertEqual(j['brief_sha256'],cloud.digest(c))
    def test_unknown_submit_not_replayed_after_restart(self):
        j=self.start(self.image());self.e.update(j,status='submitting');e=self.engine();self.assertEqual(e.jobs[j['id']]['status'],'unknown');self.assertTrue(e.queue.empty())
        with self.assertRaises(ValueError):e.resume(j['id'])
    def test_remote_job_resumes_by_polling_only(self):
        j=self.start(self.video());self.e.update(j,status='pending',remote_id='provider-123');e=self.engine();e.process(e.jobs[j['id']]);self.assertFalse(self.fake.videos);self.assertEqual(self.fake.polls,['provider-123'])
    def test_audio_violation_is_retained_not_imported(self):
        self.e.probe=lambda p,f:{'duration_seconds':8,'audio_tracks':1};j=self.start(self.video())
        with self.assertRaises(ValueError):self.e.process(j)
        self.assertTrue((self.e.root/j['result_file']).is_file());self.assertFalse(any(a['kind']=='clip' for a in self.store.load(self.pid)['shots'][0]['assets']))
    def test_late_result_keeps_actual_starter_binding(self):
        j=self.start(self.video());p=self.store.import_asset(self.pid,'one','keyframe','b.png',io.BytesIO(PNG+b'2'),len(PNG)+1);second=p['shots'][0]['assets'][-1]['id'];self.store.choose(self.pid,'one','keyframe',second,True);self.e.process(j)
        p=self.store.load(self.pid);a=p['shots'][0]['assets'][-1];self.assertEqual(a['keyframe_id'],self.aid);self.assertTrue(a['cloud_generation']['direction_stale_on_return']);self.assertEqual(p['shots'][0]['selected_keyframe'],second);self.assertTrue(p['shots'][0]['animation_ready'])
    def test_recovery_after_import_does_not_duplicate_media(self):
        j=self.start(self.video());self.e.process(j);j.pop('asset_id');self.e.update(j,status='importing');self.e.process(j);self.assertEqual(len(self.fake.videos),1);self.assertEqual(len([a for a in self.store.load(self.pid)['shots'][0]['assets'] if a['kind']=='clip']),1)
    def test_remote_urls_never_exposed_or_used(self):
        j=self.start(self.video());self.e.process(j);self.assertNotIn('private.invalid',json.dumps(self.e.status(self.pid)))
    def test_reported_cost_reconciled_even_above_estimate(self):
        self.fake.cost=1.2;j=self.start(self.image());self.e.process(j);self.assertEqual(self.e.spent(self.pid),1.2)
        with self.assertRaises(ValueError):self.e.allowance(self.pid,.5)
    def test_arbitrary_endpoints_rejected_before_key_read(self):
        c=cloud.Client(key_loader=lambda:(_ for _ in ()).throw(AssertionError('No key read')))
        for path in ['https://evil.invalid','videos/../auth/key','videos/a/content?url=x']:
            with self.assertRaises(ValueError):c.request(path)
    def test_only_one_live_worker_can_own_jobs(self):
        first=cloud.CloudEngine(self.store,server.approved,server.direction_basis,server.prompt_package,client=self.fake)
        try:
            with self.assertRaises(ValueError):cloud.CloudEngine(self.store,server.approved,server.direction_basis,server.prompt_package,client=self.fake)
        finally:first.stop.set();first.worker.join(timeout=2);first.worker_lock.close()
    def test_export_retains_brief_and_own_job_receipts(self):
        import zipfile
        path=self.store.folder(self.pid)/'cloud/production-brief.json';cloud.atomic(path,{'content':{'title':'Exportable brief'}})
        q=self.image();self.e.approve_brief(self.pid,q['brief_sha256']);j=self.start(q);self.e.process(j)
        foreign=dict(j,id='foreignjob',project_id='different');cloud.atomic(self.e.root/'jobs/foreignjob.json',foreign)
        with zipfile.ZipFile(self.store.folder(self.pid)/self.store.export(self.pid)) as z:
            self.assertIn('generation/production-brief.json',z.namelist())
            jobs=json.loads(z.read('generation/jobs.json'));self.assertEqual([v['project_id'] for v in jobs],[self.pid]);self.assertEqual(jobs[0]['id'],j['id'])
if __name__=='__main__':unittest.main()
