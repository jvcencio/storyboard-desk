import io
import json
from pathlib import Path
import tempfile
import unittest
import wave
import zipfile

import server


def wav_bytes(seconds=2):
    output = io.BytesIO()
    with wave.open(output, 'wb') as wav:
        wav.setnchannels(1)
        wav.setsampwidth(2)
        wav.setframerate(8000)
        wav.writeframes(b'\0\0' * int(8000 * seconds))
    return output.getvalue()


class MusicChecks(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.store = server.Store(self.temp.name)
        self.p = self.store.new({'title': 'Music pilot', 'music_video': True,
            'lyrics': '[Verse] Exact punctuation “here”  ',
            'shots': [{'id': 'a', 'title': 'Opening', 'scene': 'Verse', 'duration': 1},
                      {'id': 'b', 'title': 'Next', 'scene': 'Chorus', 'duration': 1}]})
        self.pid = self.p['id']

    def tearDown(self):
        self.temp.cleanup()

    def audio(self, seconds=4):
        data = wav_bytes(seconds)
        p = self.store.load(self.pid)
        return self.store.import_soundtrack(self.pid, 'track.wav', io.BytesIO(data), len(data), p['revision'])

    def clip(self):
        p = self.store.load(self.pid)
        p['shots'][0]['assets'].append({'id': 'clip', 'kind': 'clip', 'path': 'media/a/source.mp4',
            'duration_seconds': 2, 'take': 1, 'in_seconds': 0, 'out_seconds': 2})
        p['shots'][0]['selected_clip'] = 'clip'
        return self.store.save(p, p['revision'])

    def test_insert_targets_identity_and_preserves_media_without_copying_approval(self):
        p = self.clip()
        response = self.store.insert_shot(self.pid, 'a', 'after', p['revision'])
        q, sid = response['project'], response['shot_id']
        self.assertEqual([s['id'] for s in q['shots']], ['a', sid, 'b'])
        self.assertEqual(q['shots'][0]['selected_clip'], 'clip')
        self.assertEqual(q['shots'][1]['scene'], 'Verse')
        self.assertEqual(q['shots'][1]['assets'], [])
        self.assertIsNone(q['shots'][1]['approval'])
        response = self.store.insert_shot(self.pid, 'a', 'before', q['revision'])
        self.assertEqual(response['project']['shots'][0]['id'], response['shot_id'])
        with self.assertRaises(RuntimeError):
            self.store.insert_shot(self.pid, 'a', 'after', p['revision'])
        with self.assertRaises(ValueError):
            self.store.insert_shot(self.pid, 'missing', 'after', response['project']['revision'])

    def test_soundtrack_history_and_exact_lyrics_survive_reopen(self):
        first = self.audio(4)
        old = first['soundtrack']
        second = self.audio(5)
        self.assertEqual(second['soundtrack_history'][0], old)
        self.assertTrue((self.store.folder(self.pid)/old['path']).is_file())
        reopened = server.Store(self.temp.name).load(self.pid)
        self.assertEqual(reopened['soundtrack']['duration_seconds'], 5)
        self.assertEqual(reopened['lyrics'], '[Verse] Exact punctuation “here”  ')
        self.assertEqual(reopened['music_timing']['unallocated_seconds'], 3)

    def test_bad_audio_or_stale_revision_never_replaces_soundtrack(self):
        p = self.audio()
        before = self.store.load(self.pid)
        for data in [b'not wav', wav_bytes()[:16]]:
            with self.assertRaises(ValueError):
                self.store.import_soundtrack(self.pid, 'bad.wav', io.BytesIO(data), len(data), p['revision'])
        with self.assertRaises(RuntimeError):
            data = wav_bytes()
            self.store.import_soundtrack(self.pid, 'track.wav', io.BytesIO(data), len(data), p['revision'] - 1)
        self.assertEqual(self.store.load(self.pid), before)
        self.assertFalse(list(self.store.root.rglob('*.part')))

    def test_coverage_counts_empty_ranges_and_tail_without_stretching(self):
        p = self.audio(4)
        p = self.clip()
        p = self.store.trim_clip(self.pid, 'a', 'clip', p['revision'], 1.5, 2)
        self.assertEqual(p['music_timing']['covered_seconds'], .5)
        self.assertEqual(p['music_timing']['gap_seconds'], 3.5)
        self.assertEqual(p['music_timing']['unallocated_seconds'], 2)
        self.assertEqual(p['music_timing']['slots'][1]['start_seconds'], 1)
        # Editing a brief retains its per-take ranges and duration.
        q = self.store.update(self.pid, {'revision': p['revision'], 'shots': p['shots']})
        self.assertEqual(q['shots'][0]['assets'][0]['in_seconds'], 1.5)

    def test_coverage_excludes_media_after_track_and_rejects_invalid_trim(self):
        p = self.audio(.5)
        p = self.clip()
        self.assertEqual(p['music_timing']['covered_seconds'], .5)
        self.assertEqual(p['music_timing']['overrun_seconds'], 1.5)
        for start, end in [(-1, 1), (1, 1), (0, 3), (float('nan'), 1), (0, float('inf'))]:
            with self.assertRaises(ValueError):
                self.store.trim_clip(self.pid, 'a', 'clip', p['revision'], start, end)
        with self.assertRaises(RuntimeError):
            self.store.trim_clip(self.pid, 'a', 'clip', p['revision'] - 1, 0, 1)

    def test_music_context_invalidates_direction_and_requests_silent_generation(self):
        before = server.direction_basis(self.p, self.p['shots'][0])
        p = self.audio()
        self.assertNotEqual(before, server.direction_basis(p, p['shots'][0]))
        self.assertIn('Disable audio generation', server.prompt_package(p, p['shots'][0])['animation'])
        with self.assertRaises(ValueError):
            self.store.update(self.pid, {'revision': p['revision'], 'music_video': 'yes'})

    def test_music_export_preserves_original_track_and_explicit_edit_instructions(self):
        p = self.audio()
        with zipfile.ZipFile(self.store.folder(self.pid)/self.store.export(self.pid)) as archive:
            self.assertEqual(archive.read('soundtrack/original.wav'), wav_bytes(4))
            self.assertEqual(archive.read('lyrics.txt').decode(), self.p['lyrics'])
            sequence = json.loads(archive.read('sequence.json'))
            self.assertTrue(all(s['mute_clip_audio'] for s in sequence))
            self.assertEqual(json.loads(archive.read('music-timing.json'))['gap_seconds'], 4)


if __name__ == '__main__':
    unittest.main()
