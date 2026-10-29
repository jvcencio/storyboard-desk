import copy,json,unittest,zipfile
from pathlib import Path
import test_sections as fixture

class PortionChecks(unittest.TestCase):
    setUp=fixture.SectionChecks.setUp
    tearDown=fixture.SectionChecks.tearDown
    p=fixture.SectionChecks.p
    create=fixture.SectionChecks.create
    edit=fixture.SectionChecks.edit
    add=fixture.SectionChecks.add
    row=fixture.SectionChecks.row
    def split(self,**fields):
        raw={'start_seconds':0,'end_seconds':60,'part_seconds':15,'prefix':'Part'};raw.update(fields)
        return self.store.split_sections(self.pid,self.p()['revision'],raw)
    def test_four_portions_keep_chorus_and_cut_intact(self):
        before=self.p();r=self.split();p=self.p();new=[self.store.section(p,sid) for sid in r['section_ids']]
        self.assertEqual([(s['start_seconds'],s['end_seconds'],s['kind']) for s in new],[(0,15,'none'),(15,30,'none'),(30,45,'none'),(45,60,'none')]);self.assertEqual(p['music_sections'][-1],before['music_sections'][0]);self.assertEqual(p['shots'],before['shots']);self.assertEqual(p['soundtrack'],before['soundtrack']);self.assertEqual(p['lyrics'],before['lyrics']);self.assertTrue(all(not s['clips'] and not s['coverage'] for s in new));self.store.undo_section(self.pid,p['revision']);self.assertEqual(self.p()['music_sections'],before['music_sections'])
    def test_exact_fractional_end_kept_with_short_final_portion(self):
        r=self.split(start_seconds=.25,end_seconds=32.5,part_seconds=15);rows=[self.store.section(self.p(),sid) for sid in r['section_ids']];self.assertEqual([(s['start_seconds'],s['end_seconds']) for s in rows],[(.25,15.25),(15.25,30.25),(30.25,32.5)])
    def test_invalid_batch_never_partial_saves(self):
        before=self.p()
        for fields in ({'part_seconds':0},{'part_seconds':True},{'part_seconds':float('nan')},{'part_seconds':.01},{'end_seconds':101},{'end_seconds':61},{'start_seconds':-1},{'kind':'invented'},{'prefix':'x'*100}):
            with self.assertRaises(ValueError):self.split(**fields)
        self.assertEqual(self.p(),before)
    def test_stale_and_duplicate_split_rejected(self):
        r=self.split();before=self.p()
        with self.assertRaises(ValueError):self.split()
        with self.assertRaises(RuntimeError):self.store.split_sections(self.pid,before['revision']-1,{'start_seconds':0,'end_seconds':60,'part_seconds':15})
        self.assertEqual(self.p(),before)
    def test_none_and_prechorus_tags_retroactive_without_changing_media(self):
        self.add();p=self.p();section=copy.deepcopy(self.row());section['kind']='none';self.store.save_section(self.pid,p['revision'],section);before=self.p();row=self.row()['coverage'][0];self.store.coverage_edit(self.pid,before['revision'],self.sid,{'coverage':row['id'],'operation':'update','music_tag':'pre-chorus'});after=self.p();self.assertEqual(after['shots'],before['shots']);self.assertEqual(self.row()['clips'],section['clips']);self.assertEqual(self.row()['coverage'][0]['music_tag'],'pre-chorus');self.assertEqual(self.row()['kind'],'none')
    def test_invalid_clip_tag_and_reel_preference_do_not_save(self):
        self.add();before=self.p();row=self.row()['coverage'][0]
        with self.assertRaises(ValueError):self.store.coverage_edit(self.pid,before['revision'],self.sid,{'coverage':row['id'],'operation':'update','music_tag':'imaginary'})
        section=copy.deepcopy(self.row());section['reel_scope']='imaginary'
        with self.assertRaises(ValueError):self.store.save_section(self.pid,before['revision'],section)
        self.assertEqual(self.p(),before)
    def test_clip_tags_retained_in_both_editor_handoffs(self):
        self.add();row=self.row()['coverage'][0];self.store.coverage_edit(self.pid,self.p()['revision'],self.sid,{'coverage':row['id'],'operation':'update','music_tag':'intro'})
        def render(segments,dst,aspect):dst.write_bytes(b'picture');return {'duration_seconds':sum(c['end']-c['start'] for c in segments),'audio_tracks':0}
        result=self.store.export_reel(self.pid,self.p()['revision'],self.sid,renderer=render);manifest=json.loads((self.store.folder(self.pid)/'exports'/Path(result['map_url']).name).read_text(encoding='utf-8'));self.assertEqual(manifest['cuts'][0]['music_tag'],'intro')
        def range_render(src,dst,start,end):dst.write_bytes(b'picture');return {'duration_seconds':end-start,'audio_tracks':0}
        path=self.store.folder(self.pid)/self.store.export_section(self.pid,self.p()['revision'],self.sid,range_render,True)
        with zipfile.ZipFile(path) as z:self.assertEqual(json.loads(z.read('section.json'))['coverage_options'][0]['music_tag'],'intro')
if __name__=='__main__':unittest.main()
