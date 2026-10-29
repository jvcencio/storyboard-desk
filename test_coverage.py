import copy,json,unittest,zipfile
from pathlib import Path
import xml.etree.ElementTree as ET
import test_sections as fixture

class CoverageChecks(unittest.TestCase):
    setUp=fixture.SectionChecks.setUp
    tearDown=fixture.SectionChecks.tearDown
    p=fixture.SectionChecks.p
    create=fixture.SectionChecks.create
    edit=fixture.SectionChecks.edit
    add=fixture.SectionChecks.add
    row=fixture.SectionChecks.row
    def collect(self,**fields):
        raw={'source_shot':'one','source_asset':'clip0','in_seconds':1,'out_seconds':7};raw.update(fields)
        return self.store.coverage_add(self.pid,self.p()['revision'],self.sid,raw)
    def change(self,**fields):
        raw={'coverage':self.row()['coverage'][0]['id'],'operation':'update'};raw.update(fields)
        return self.store.coverage_edit(self.pid,self.p()['revision'],self.sid,raw)
    def permit_own_scene(self):
        p=self.p();p['shots'][0]['scene']='Chorus 1';p['shots'][1]['scene']='Chorus 2';self.store.save(p,p['revision'])
    def reserve(self):
        self.permit_own_scene();self.collect();self.change(exclusive=True)
    def fake(self,segments,target,aspect):
        target.write_bytes(b'silent reel');return {'duration_seconds':sum(s['end']-s['start'] for s in segments),'audio_tracks':0}
    def test_collect_is_independent_from_cut_and_duplicate_keeps_notes(self):
        before=copy.deepcopy(self.p()['shots']);first=self.collect();self.change(angle='Low angle',note='Check tail',in_seconds=2,out_seconds=4)
        again=self.collect(in_seconds=0,out_seconds=8)
        self.assertTrue(again['already_collected']);self.assertEqual(first['coverage_id'],again['coverage_id']);self.assertEqual(len(self.row()['coverage']),1);self.assertEqual(self.row()['clips'],[]);self.assertEqual(self.p()['shots'],before);self.assertEqual(self.row()['coverage'][0]['in_seconds'],2)
    def test_assembly_autocollects_full_source_and_removing_range_retains_option(self):
        self.add();self.assertEqual((self.row()['coverage'][0]['in_seconds'],self.row()['coverage'][0]['out_seconds']),(0,8));self.edit(operation='remove',entry=self.row()['clips'][0]['id']);self.assertEqual(len(self.row()['coverage']),1)
    def test_remove_referenced_option_blocked_and_undo_restores_bin(self):
        self.add()
        with self.assertRaises(ValueError):self.change(operation='remove')
        self.edit(operation='remove',entry=self.row()['clips'][0]['id']);self.change(operation='remove');self.assertEqual(self.row()['coverage'],[]);self.store.undo_section(self.pid,self.p()['revision']);self.assertEqual(len(self.row()['coverage']),1)
    def test_preferred_range_does_not_edit_assembly(self):
        self.add();before=copy.deepcopy(self.row()['clips']);self.change(in_seconds=3,out_seconds=5);self.assertEqual(self.row()['clips'],before)
    def test_order_changes_reel_only_and_is_undoable(self):
        self.add();self.collect(source_shot='two',source_asset='clip1');original=copy.deepcopy(self.row());self.change(operation='move',delta=1);self.assertEqual(self.row()['coverage'][0]['source_asset'],'clip1');self.assertEqual(self.row()['clips'],original['clips']);self.store.undo_section(self.pid,self.p()['revision']);self.assertEqual(self.row(),original)
    def test_existing_foreign_legacy_use_prevents_reservation(self):
        self.collect();before=self.p()
        with self.assertRaisesRegex(ValueError,'Conflicting use'):self.change(exclusive=True)
        self.assertEqual(self.p(),before)
    def test_reservation_blocks_other_section_pool_and_assembly(self):
        self.reserve();other=self.create(label='Chorus 2',start_seconds=80,end_seconds=90)['section_id'];before=self.p();raw={'source_shot':'one','source_asset':'clip0','in_seconds':0,'out_seconds':4}
        with self.assertRaisesRegex(ValueError,'Reserved for Chorus 1'):self.store.coverage_add(self.pid,before['revision'],other,raw)
        with self.assertRaisesRegex(ValueError,'Reserved for Chorus 1'):self.store.section_edit(self.pid,before['revision'],other,dict(raw,operation='add'))
        self.assertEqual(self.p(),before)
    def test_shared_options_allowed_until_one_is_reserved(self):
        self.permit_own_scene();self.collect();other=self.create(label='Other',start_seconds=80,end_seconds=90)['section_id'];self.store.coverage_add(self.pid,self.p()['revision'],other,{'source_shot':'one','source_asset':'clip0','in_seconds':0,'out_seconds':8})
        with self.assertRaisesRegex(ValueError,'Conflicting use'):self.change(exclusive=True)
    def test_legacy_placement_and_scene_change_cannot_bypass_reservation(self):
        self.reserve();before=self.p()
        with self.assertRaisesRegex(ValueError,'Reserved for Chorus 1'):self.store.place_clip(self.pid,'one','clip0','two',before['revision'],0,4)
        p=self.p();p['shots'][0]['scene']='Different'
        with self.assertRaisesRegex(ValueError,'Reserved for Chorus 1'):self.store.save(p,p['revision'])
        self.assertEqual(self.p(),before)
    def test_duplicate_bytes_cannot_bypass_reservation(self):
        self.reserve();p=self.p();p['shots'][1]['assets'].append(dict(p['shots'][0]['assets'][0],id='same-bytes'));p['shots'][1]['selected_clip']=None;self.store.save(p,p['revision']);before=self.p()
        with self.assertRaisesRegex(ValueError,'Reserved for Chorus 1'):self.store.choose(self.pid,'two','clip','same-bytes')
        self.assertEqual(self.p(),before)
    def test_release_allows_reuse_even_after_soundtrack_replaced(self):
        self.reserve();p=self.p();data=fixture.audio(110);import io
        self.store.import_soundtrack(self.pid,'replacement.wav',io.BytesIO(data),len(data),p['revision']);self.change(exclusive=False)
        p=self.p();self.store.place_clip(self.pid,'one','clip0','two',p['revision'],0,4)
    def test_full_reel_exceeds_target_and_cut_map_preserves_order(self):
        self.collect();self.collect(source_shot='two',source_asset='clip1');p=self.p();s=copy.deepcopy(self.row());s['end_seconds']=64;self.store.save_section(self.pid,p['revision'],s);before=self.p();result=self.store.export_reel(self.pid,before['revision'],self.sid,renderer=self.fake)
        self.assertEqual((result['duration_seconds'],result['target_seconds']),(16,4));manifest=json.loads((self.store.folder(self.pid)/'exports'/Path(result['map_url']).name).read_text(encoding='utf-8'));self.assertEqual([(c['reel_in_seconds'],c['reel_out_seconds']) for c in manifest['cuts']],[(0,8),(8,16)]);self.assertEqual(self.p(),before)
    def test_reel_scope_and_inclusion(self):
        self.add(in_seconds=1,out_seconds=3);self.collect(source_shot='two',source_asset='clip1');self.change(in_seconds=3,out_seconds=7);self.change(coverage=self.row()['coverage'][1]['id'],include_in_handoff=False)
        for scope,expected in [('coverage',8),('preferred',4),('assembly',2)]:self.assertEqual(self.store.export_reel(self.pid,self.p()['revision'],self.sid,scope,self.fake)['duration_seconds'],expected)
    def test_reel_failure_cleanup_and_invalid_scope(self):
        self.collect();before=self.p()
        def fail(segments,target,aspect):target.write_bytes(b'partial');raise ValueError('failed')
        with self.assertRaises(ValueError):self.store.export_reel(self.pid,before['revision'],self.sid,renderer=fail)
        with self.assertRaises(ValueError):self.store.export_reel(self.pid,before['revision'],self.sid,scope='invalid',renderer=self.fake)
        self.assertEqual(list(self.store.folder(self.pid).glob('exports/*')),[]);self.assertEqual(self.p(),before)
    def test_coverage_package_options_do_not_lengthen_xml(self):
        self.add(in_seconds=0,out_seconds=2);self.collect(source_shot='two',source_asset='clip1')
        def render(src,dst,start,end):dst.write_bytes(b'picture');return {'duration_seconds':end-start,'audio_tracks':0}
        path=self.store.folder(self.pid)/self.store.export_section(self.pid,self.p()['revision'],self.sid,render,include_coverage=True)
        with zipfile.ZipFile(path) as z:
            m=json.loads(z.read('section.json'));self.assertEqual(m['assembly_seconds'],2);self.assertEqual(len(m['coverage_options']),2);xml=ET.fromstring(z.read('Final Cut.fcpxml'));self.assertEqual(xml.find('.//sequence').get('duration'),'18s');self.assertEqual(len(xml.findall('.//event/asset-clip')),2)
    def test_bad_range_or_reservation_flag_never_saves(self):
        self.collect();before=self.p()
        for fields in ({'in_seconds':8},{'out_seconds':9},{'exclusive':'yes'},{'include_in_handoff':1}):
            with self.assertRaises(ValueError):self.change(**fields)
        self.assertEqual(self.p(),before)

if __name__=='__main__':unittest.main()
