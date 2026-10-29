import copy,hashlib,io,json,tempfile,unittest,wave,zipfile
from pathlib import Path
import xml.etree.ElementTree as ET
import server,sections

def audio(seconds=100):
    out=io.BytesIO()
    with wave.open(out,'wb') as w:w.setnchannels(1);w.setsampwidth(2);w.setframerate(8000);w.writeframes(b'\0\0'*int(seconds*8000))
    return out.getvalue()
class SectionChecks(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.store=server.Store(self.temp.name);p=self.store.new({'title':'Song test','music_video':True,'aspect':'16:9','lyrics':'Exact original lyrics','shots':[{'id':'one','title':'Party'},{'id':'two','title':'Reaction'}]});self.pid=p['id'];data=audio();p=self.store.import_soundtrack(self.pid,'song.wav',io.BytesIO(data),len(data),p['revision'])
        self.original_audio=data
        for i,s in enumerate(p['shots']):
            path=self.store.folder(self.pid)/f'media/{s["id"]}/source.mp4';path.parent.mkdir(exist_ok=True);path.write_bytes(b'media fixture '+bytes([i]));s['assets'].append({'id':'clip'+str(i),'kind':'clip','path':str(path.relative_to(self.store.folder(self.pid))),'sha256':hashlib.sha256(path.read_bytes()).hexdigest(),'duration_seconds':8,'in_seconds':0,'out_seconds':8,'take':1});s['selected_clip']='clip'+str(i)
        self.store.save(p,p['revision']);self.sid=self.create()['section_id']
    def tearDown(self):self.temp.cleanup()
    def p(self):return self.store.load(self.pid)
    def create(self,**kwargs):
        raw={'label':'Chorus 1','kind':'chorus','start_seconds':60,'end_seconds':78};raw.update(kwargs);return self.store.save_section(self.pid,self.p()['revision'],raw)
    def edit(self,**kwargs):return self.store.section_edit(self.pid,self.p()['revision'],self.sid,kwargs)
    def add(self,**kwargs):
        raw={'operation':'add','source_shot':'one','source_asset':'clip0','in_seconds':1,'out_seconds':7};raw.update(kwargs);return self.edit(**raw)
    def row(self):return self.store.section(self.p(),self.sid)
    def test_exact_anchor_range_cue_and_lyrics_preserved(self):
        p=self.p();p=self.store.music_cue(self.pid,p['revision'],"she’s not wrong, so i let it go on",76,end_seconds=79)
        self.assertEqual((self.row()['start_seconds'],self.row()['end_seconds']),(60,78));self.assertEqual(p['music_cues'][0]['end_seconds'],79);self.assertEqual(p['lyrics'],'Exact original lyrics')
    def test_invalid_section_times_duplicate_names_and_stale_revision(self):
        for x in ({'start_seconds':float('nan')},{'end_seconds':101},{'end_seconds':60},{'start_seconds':True},{'label':''},{'kind':'unknown'}):
            with self.assertRaises(ValueError):self.create(label='Other',**{k:v for k,v in x.items() if k!='label'}) if 'label' not in x else self.create(**x)
        with self.assertRaises(ValueError):self.create()
        with self.assertRaises(RuntimeError):self.store.save_section(self.pid,self.p()['revision']-1,{'label':'Other','start_seconds':0,'end_seconds':10})
    def test_reuse_ranges_independent_and_insert_order(self):
        before=copy.deepcopy(self.p()['shots']);self.add();first=self.row()['clips'][0]['id'];self.add(anchor=first,position='before',in_seconds=0,out_seconds=2);self.add(anchor=first,position='after',in_seconds=2,out_seconds=4)
        self.assertEqual([(c['in_seconds'],c['out_seconds']) for c in self.row()['clips']],[(0,2),(1,7),(2,4)]);self.assertEqual(self.p()['shots'],before)
    def test_reorder_remove_and_undo(self):
        self.add();self.add(source_shot='two',source_asset='clip1');original=copy.deepcopy(self.row()['clips']);self.edit(operation='move',entry=original[1]['id'],delta=-1);self.assertEqual(self.row()['clips'][0]['id'],original[1]['id']);self.edit(operation='remove',entry=original[0]['id']);self.assertEqual(len(self.row()['clips']),1);self.store.undo_section(self.pid,self.p()['revision']);self.assertEqual(len(self.row()['clips']),2)
    def test_edit_ranges_cannot_replace_or_invent_source(self):
        self.add();row=copy.deepcopy(self.row());row['clips'][0].update(in_seconds=2,out_seconds=3,source_asset='not-real');self.store.save_section(self.pid,self.p()['revision'],row);self.assertEqual(self.row()['clips'][0]['source_asset'],'clip0');self.assertEqual(self.row()['clips'][0]['in_seconds'],2)
        row=copy.deepcopy(self.row());row['clips'][0]['id']='invented'
        with self.assertRaises(ValueError):self.store.save_section(self.pid,self.p()['revision'],row)
    def test_invalid_media_range_anchor_or_delta_does_not_save(self):
        before=self.p()
        for args in ({'in_seconds':-1},{'out_seconds':9},{'out_seconds':float('inf')},{'position':'before','anchor':'missing'},{'source_asset':'foreign'}):
            with self.assertRaises(ValueError):self.add(**args)
        self.assertEqual(self.p(),before)
        self.add();first=self.row()['clips'][0]['id']
        with self.assertRaises(ValueError):self.edit(operation='move',entry=first,delta=-1)
    def test_binned_changed_and_external_source_rejected(self):
        p=self.p();p['shots'][0]['assets'][0]['binned_at']='test';self.store.save(p,p['revision'])
        with self.assertRaises(ValueError):self.add()
        p=self.p();p['shots'][0]['assets'][0].pop('binned_at');p['shots'][0]['assets'][0]['path']='../../outside.mp4';self.store.save(p,p['revision'])
        with self.assertRaises(ValueError):self.add()
    def test_sections_remain_bound_to_old_recording(self):
        row=copy.deepcopy(self.row());p=self.p();data=audio(110);self.store.import_soundtrack(self.pid,'replacement.wav',io.BytesIO(data),len(data),p['revision']);self.assertEqual(self.row(),row)
        with self.assertRaises(ValueError):self.add()
        with self.assertRaises(ValueError):self.store.save_section(self.pid,self.p()['revision'],row)
    def test_invalid_lyric_ranges_never_save(self):
        before=self.p()
        for end in (76,101,float('nan'),True):
            with self.assertRaises(ValueError):self.store.music_cue(self.pid,before['revision'],'Lyric',76,end_seconds=end)
        self.assertEqual(self.p(),before)
    def test_export_keeps_source_handles_silent_ranges_and_crossing_cue(self):
        self.add();self.add(in_seconds=2,out_seconds=5);p=self.p();self.store.music_cue(self.pid,p['revision'],'Crossing phrase',76,end_seconds=79);p=self.p();self.store.music_cue(self.pid,p['revision'],'Previous phrase ends at section start',50,end_seconds=60);p=self.p();self.store.music_cue(self.pid,p['revision'],'Next section point',78);p=self.p();calls=[]
        def renderer(src,dst,start,end):calls.append((start,end));dst.write_bytes(b'silent MP4');return {'duration_seconds':end-start,'audio_tracks':0}
        path=self.store.folder(self.pid)/self.store.export_section(self.pid,p['revision'],self.sid,renderer)
        with zipfile.ZipFile(path) as z:
            manifest=json.loads(z.read('section.json'));self.assertEqual(len(manifest['cues']),1);self.assertEqual(manifest['assembly_seconds'],9);self.assertEqual(manifest['cues'][0]['end_seconds'],79);self.assertEqual(len([n for n in z.namelist() if n.startswith('sources/')]),1);self.assertEqual(z.read('soundtrack/original.wav'),self.original_audio)
            with wave.open(io.BytesIO(z.read('soundtrack/section.wav')),'rb') as wav:self.assertEqual(wav.getnframes()/wav.getframerate(),18)
            xml=ET.fromstring(z.read('Final Cut.fcpxml'));self.assertEqual(xml.find('.//sequence').get('duration'),'18s');self.assertTrue(all(n.get('srcEnable')=='video' for n in xml.findall('.//asset-clip') if n.get('lane')=='1'));marker=next(n for n in xml.findall('.//marker') if n.get('value')=='Crossing phrase');self.assertEqual(marker.get('duration'),'2s');self.assertIn('79',z.read('lyric-cues.csv').decode())
        self.assertEqual(self.p(),p);self.assertEqual(calls,[(1,7),(2,5)])
    def test_export_failure_cleans_partial_and_retains_project(self):
        self.add();before=self.p()
        def fail(*a):raise ValueError('conversion failed')
        with self.assertRaises(ValueError):self.store.export_section(self.pid,before['revision'],self.sid,fail)
        self.assertFalse(list(self.store.folder(self.pid).glob('exports/*')));self.assertEqual(self.p(),before)
    def test_general_export_retains_all_section_metadata(self):
        self.add();p=self.p()
        with zipfile.ZipFile(self.store.folder(self.pid)/self.store.export(self.pid)) as z:self.assertEqual(json.loads(z.read('song-sections.json')),p['music_sections'])
if __name__=='__main__':unittest.main()
