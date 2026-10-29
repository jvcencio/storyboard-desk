import copy
import hashlib
import io
import json
from pathlib import Path
import tempfile
import unittest
import wave
import zipfile

import server


class LibraryChecks(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.store = server.Store(self.temp.name)
        self.p = self.store.new({'title':'Clip library', 'music_video':True,
            'lyrics':'[Chorus] Exact original words “here”.',
            'shots':[{'id':'source', 'title':'Source pool', 'duration':4},
                     {'id':'later', 'title':'Later chorus', 'duration':4}]})
        self.pid = self.p['id']
        payload = b'\x00\x00\x00\x20ftypisom' + b'unchanged source fixture'
        self.p = self.store.import_asset(self.pid, 'source', 'clip', 'original.mp4', io.BytesIO(payload), len(payload))
        a = self.p['shots'][0]['assets'][0]
        a.update(duration_seconds=8, in_seconds=1, out_seconds=7)
        self.aid = a['id']
        self.p['shots'][0]['selected_clip'] = self.aid
        self.p = self.store.save(self.p, self.p['revision'])

    def tearDown(self):
        self.temp.cleanup()

    def place(self, target, start=0, end=4, source=None):
        p = self.store.load(self.pid)
        return self.store.place_clip(self.pid, 'source', source or self.aid, target,
                                     p['revision'], start, end)

    def audio(self, seconds=8):
        output = io.BytesIO()
        with wave.open(output, 'wb') as wav:
            wav.setnchannels(1); wav.setsampwidth(2); wav.setframerate(8000)
            wav.writeframes(b'\0\0' * (8000 * seconds))
        data = output.getvalue(); p = self.store.load(self.pid)
        return self.store.import_soundtrack(self.pid, 'song.wav', io.BytesIO(data), len(data), p['revision'])

    def test_independent_uses_never_duplicate_or_trim_source_file(self):
        original = copy.deepcopy(self.p['shots'][0]['assets'][0])
        p = self.place('source', .5, 4)
        first = p['shots'][0]['assets'][-1]
        p = self.place('later', 2, 6)
        second = p['shots'][1]['assets'][-1]
        self.assertEqual(first['path'], second['path'])
        self.assertEqual(first['source_ref']['asset_id'], self.aid)
        self.assertEqual(len(list(self.store.folder(self.pid).rglob('*.mp4'))), 1)
        p = self.store.trim_clip(self.pid, 'later', second['id'], p['revision'], 3, 5)
        self.assertEqual(p['shots'][0]['assets'][0], original)
        self.assertEqual(p['shots'][0]['assets'][-1]['in_seconds'], .5)
        self.assertEqual(p['shots'][1]['assets'][-1]['in_seconds'], 3)
        self.assertEqual(p['lyrics'], self.p['lyrics'])
        reopened = server.Store(self.temp.name).load(self.pid)
        self.assertEqual(reopened['shots'][1]['selected_clip'], second['id'])

    def test_reference_of_reference_flattens_to_original(self):
        p = self.place('source', 1, 4)
        use = p['shots'][0]['assets'][-1]
        p = self.place('later', 4, 7, use['id'])
        self.assertEqual(p['shots'][1]['assets'][-1]['source_ref']['asset_id'], self.aid)

    def test_invalid_or_stale_placement_does_not_mutate_project(self):
        before = self.store.load(self.pid)
        for start,end in [(-1,2),(2,2),(0,9),(float('nan'),4),(0,float('inf'))]:
            with self.assertRaises(ValueError):
                self.store.place_clip(self.pid, 'source', self.aid, 'later', before['revision'], start, end)
        with self.assertRaises(RuntimeError):
            self.store.place_clip(self.pid, 'source', self.aid, 'later', before['revision']-1, 0, 4)
        self.assertEqual(self.store.load(self.pid), before)
        p = self.store.bin_asset(self.pid, 'source', self.aid, before['revision'])
        with self.assertRaises(ValueError):
            self.store.place_clip(self.pid, 'source', self.aid, 'later', p['revision'], 0, 4)

    def test_undo_restores_prior_selection_without_deleting_references(self):
        p = self.place('source')
        first = p['shots'][0]['selected_clip']
        p = self.place('later')
        second = p['shots'][1]['selected_clip']
        p = self.store.undo_placement(self.pid, p['revision'])
        self.assertIsNone(p['shots'][1]['selected_clip'])
        self.assertEqual(p['shots'][1]['assets'][-1]['id'], second)
        p = self.store.undo_placement(self.pid, p['revision'])
        self.assertEqual(p['shots'][0]['selected_clip'], self.aid)
        self.assertEqual(p['shots'][0]['assets'][-1]['id'], first)
        with self.assertRaises(ValueError): self.store.undo_placement(self.pid, p['revision'])

    def test_undo_cannot_overwrite_a_later_manual_selection(self):
        p = self.place('source')
        p = self.store.choose(self.pid, 'source', 'clip', self.aid)
        with self.assertRaises(ValueError): self.store.undo_placement(self.pid, p['revision'])
        self.assertEqual(self.store.load(self.pid)['shots'][0]['selected_clip'], self.aid)

    def test_export_and_coverage_use_placement_ranges_and_lineage(self):
        self.audio()
        p = self.place('source', 0, 2)
        p = self.place('later', 4, 7)
        self.assertEqual(p['music_timing']['covered_seconds'], 5)
        self.assertEqual(p['music_timing']['gap_seconds'], 3)
        with zipfile.ZipFile(self.store.folder(self.pid)/self.store.export(self.pid)) as z:
            sequence = json.loads(z.read('sequence.json'))
            self.assertEqual([(s['in_seconds'],s['out_seconds']) for s in sequence], [(0,2),(4,7)])
            self.assertTrue(all(s['source_ref']['asset_id']==self.aid for s in sequence))
            self.assertTrue(all(s['mute_clip_audio'] for s in sequence))
            self.assertEqual(z.read(sequence[0]['files']['clip']), z.read(sequence[1]['files']['clip']))

    def test_review_metadata_leaves_source_and_cut_unchanged(self):
        p = self.store.review_clip(self.pid, 'source', self.aid, self.p['revision'], 'candidate',
                                  'Crew insert','Supporting coverage','Trim before the late camera change.')
        a = p['shots'][0]['assets'][0]
        self.assertEqual(a['editorial_review']['setup'], 'Crew insert')
        self.assertEqual((a['in_seconds'],a['out_seconds']), (1,7))
        self.assertEqual(p['shots'][0]['selected_clip'], self.aid)
        self.assertEqual(hashlib.sha256((self.store.folder(self.pid)/a['path']).read_bytes()).hexdigest(), a['sha256'])
        with self.assertRaises(ValueError):
            self.store.review_clip(self.pid, 'source', self.aid, p['revision'], 'approved','x','x','x')

    def test_song_cues_are_track_linked_and_do_not_retime_storyboard(self):
        p = self.audio()
        slots = copy.deepcopy(p['music_timing']['slots'])
        p = self.store.music_cue(self.pid, p['revision'], 'Chorus 1', 2.125)
        cue = p['music_cues'][0]
        self.assertEqual(cue['track_sha256'], p['soundtrack']['sha256'])
        self.assertEqual(p['music_timing']['slots'], slots)
        self.assertEqual(p['lyrics'], self.p['lyrics'])
        with zipfile.ZipFile(self.store.folder(self.pid)/self.store.export(self.pid)) as z:
            self.assertEqual(json.loads(z.read('music-cues.json'))[0]['seconds'], 2.125)
        p = self.audio(6)
        self.assertNotEqual(p['music_cues'][0]['track_sha256'], p['soundtrack']['sha256'])
        with self.assertRaises(ValueError):
            self.store.music_cue(self.pid,p['revision'],'',None,cue['id'],True)

    def test_invalid_cues_and_stale_saves_leave_recording_and_cues_intact(self):
        p = self.audio()
        for label,seconds in [('',1),('Verse',-1),('Chorus',9),('Cue',float('nan'))]:
            with self.assertRaises(ValueError): self.store.music_cue(self.pid,p['revision'],label,seconds)
        with self.assertRaises(RuntimeError): self.store.music_cue(self.pid,p['revision']-1,'Cue',2)
        self.assertNotIn('music_cues',self.store.load(self.pid))


if __name__=='__main__': unittest.main()
