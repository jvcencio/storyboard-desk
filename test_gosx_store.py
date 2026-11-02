"""Real standalone Go editor, Python project compatibility and OS ownership.

All projects, media and receipts are synthetic and confined to temporary storage.
No provider clients are started. The built editor runs with an empty PATH.
"""
import hashlib
from html.parser import HTMLParser
import io
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import tempfile
import unittest
import urllib.error
import urllib.parse
import urllib.request

import server
from platform_support import FileLock
from test_server import PNG
from test_music import wav_bytes

APP = Path(__file__).resolve().parent


class Forms(HTMLParser):
    def __init__(self, source):
        super().__init__()
        self.forms = []
        self.current = None
        self.feed(source)

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if tag == 'form':
            self.current = {'action': attrs.get('action'), 'values': {}}
            self.forms.append(self.current)
        if tag == 'input' and self.current is not None and attrs.get('name'):
            self.current['values'][attrs['name']] = attrs.get('value', '')

    def handle_endtag(self, tag):
        if tag == 'form':
            self.current = None


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None


@unittest.skipUnless(shutil.which('go'), 'Go is required for storage compatibility tests')
class GoStorageChecks(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.build = tempfile.TemporaryDirectory()
        cls.addClassCleanup(cls.build.cleanup)
        cls.binary = str(Path(cls.build.name)/('storyboard.exe' if os.name == 'nt' else 'storyboard'))
        result = subprocess.run(['go', 'build', '-o', cls.binary, '.'], cwd=APP/'gosx-host',
                                capture_output=True, text=True, timeout=120)
        if result.returncode:
            raise RuntimeError(result.stdout + result.stderr)

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.data = Path(self.temp.name)/'projects'
        self.data.mkdir()
        self.process = None
        self.addCleanup(self.stop_editor)
        with server.Store(self.data) as store:
            p = store.new({'title': 'Existing 雪 project', 'brief': 'Original brief', 'aspect': '16:9',
                           'music_video': True, 'lyrics': 'Original lyrics “here”.\n  Exact spaces',
                           'shots': [{'id': 'first', 'title': 'First', 'scene': 'Verse', 'composition': 'Framing'},
                                     {'id': 'second', 'title': 'Second', 'scene': 'Chorus'}]})
            self.pid = p['id']
            p = store.import_asset(self.pid, 'first', 'keyframe', '雪.png', io.BytesIO(PNG), len(PNG))
            p = store.choose(self.pid, 'first', 'keyframe', p['shots'][0]['assets'][0]['id'], True)
            track = wav_bytes(10)
            p = store.import_soundtrack(self.pid, 'song.wav', io.BytesIO(track), len(track), p['revision'])
            p['future'] = {'large': 900719925474099312345, 'nested': ['雪', {'enabled': True}]}
            p['shots'][0]['future'] = {'large': 900719925474099312345}
            p['shots'][0]['assets'][0]['future'] = {'source': 'preserve'}
            p['shots'][0]['direction_review'] = {'current_basis': 'retained receipt'}
            p['music_sections'] = [{'id': 'verse', 'label': 'Verse', 'clips': [], 'coverage': [],
                                    'start_seconds': 0, 'end_seconds': 10, 'future': {'keep': True}}]
            p = store.save(p, p['revision'])
            self.original = p
        self.path = self.data/self.pid/'project.json'
        receipt = self.data/'cloud/jobs/untouched.json'
        receipt.parent.mkdir(parents=True)
        receipt.write_text('{"status":"unknown","reservation":12.5}', encoding='utf-8')
        self.files = self.non_project_hashes()
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())

    def non_project_hashes(self):
        return {str(p.relative_to(self.data)): hashlib.sha256(p.read_bytes()).hexdigest()
                for p in self.data.rglob('*') if p.is_file() and p.name not in ('project.json', '.storyboard-owner.lock')}

    def start_editor(self):
        # Demonstrates that launch and operations do not invoke Python or Go tools.
        env = dict(os.environ, PATH='')
        self.process = subprocess.Popen([self.binary, '-editor', '-data-dir', str(self.data)],
                                        stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
                                        encoding='utf-8', env=env)
        line = self.process.stdout.readline().strip()
        if not line.startswith('GoSX editor: '):
            _, stderr = self.process.communicate(timeout=10)
            self.fail('Editor did not start: '+line+' '+stderr)
        self.url = line.removeprefix('GoSX editor: ')

    def stop_editor(self):
        if self.process is not None:
            if self.process.poll() is None:
                self.process.kill()
            self.process.communicate(timeout=10)
            self.process = None

    def page(self):
        with self.opener.open(self.url, timeout=10) as response:
            self.assertEqual(response.headers['Cache-Control'], 'no-store')
            return response.read().decode('utf-8')

    def form(self, action, shot='first'):
        return next(f['values'] for f in Forms(self.page()).forms
                    if f['action'] == action and (action == '/create' or f['values'].get('shot') == shot))

    def post(self, action, values, status=303, origin=None):
        request = urllib.request.Request(self.url.rstrip('/')+action,
            data=urllib.parse.urlencode(values).encode(),
            headers={'Content-Type': 'application/x-www-form-urlencoded',
                     'Origin': origin or self.url.rstrip('/')})
        with self.assertRaises(urllib.error.HTTPError) as result:
            self.opener.open(request, timeout=10)
        try:
            self.assertEqual(result.exception.code, status, result.exception.read().decode())
            if status == 303:
                self.assertTrue(result.exception.headers['Location'].startswith('/'))
        finally:
            result.exception.close()

    def test_standalone_editor_round_trip_preserves_existing_projects(self):
        self.start_editor()
        self.assertIn('Existing 雪 project', self.page())
        form = self.form('/save')
        before = self.path.read_bytes()
        self.post('/save', dict(form, csrf='wrong'), 403)
        self.post('/save', form, 403, 'https://unrelated.example')
        self.assertEqual(self.path.read_bytes(), before)
        self.post('/save', dict(form, title='Edited in GoSX <script>'))
        self.post('/save', dict(form, title='Stale overwrite'), 409)
        self.assertIn('&lt;script&gt;', self.page())
        stored = json.loads(self.path.read_text(encoding='utf-8'))
        self.assertTrue(server.approved(stored, stored['shots'][0]))
        form = self.form('/insert')
        self.post('/insert', dict(form, position='after'))
        self.post('/insert', dict(form, position='after'), 409)
        form = self.form('/save')
        self.post('/save', dict(form, composition='Changed visual context'))
        create = self.form('/create')
        self.post('/create', dict(create, title='New Go project', brief='Created without Python', aspect='1:1'))
        self.assertEqual(self.non_project_hashes(), self.files)
        self.stop_editor()
        # Reopen with the actual Python loader, including its derived approvals,
        # timing and coverage. It must understand both old and Go-created projects.
        with server.Store(self.data) as store:
            p = store.load(self.pid)
            self.assertEqual(p['revision'], self.original['revision']+3)
            self.assertEqual([s['id'] for s in p['shots']][::2], ['first', 'second'])
            self.assertEqual(p['shots'][1]['duration'], 8)
            self.assertEqual(p['shots'][1]['scene'], 'Verse')
            self.assertEqual(p['shots'][1]['assets'], [])
            self.assertIsNone(p['shots'][1]['approval'])
            self.assertEqual(p['shots'][0]['title'], 'Edited in GoSX <script>')
            self.assertFalse(p['shots'][0]['animation_ready'])
            for name in ('future', 'lyrics', 'soundtrack', 'soundtrack_history', 'music_sections'):
                self.assertEqual(p[name], self.original[name], name)
            for name in ('future', 'assets', 'selected_keyframe', 'approval', 'direction_review'):
                self.assertEqual(p['shots'][0][name], self.original['shots'][0][name], name)
            new = next(p for p in store.listing() if p['id'] != self.pid)
            self.assertEqual((new['title'], new['aspect'], new['revision']), ('New Go project', '1:1', 1))
            self.assertEqual(new['shots'][0]['duration'], 5)
            # Python can continue editing a Go-created project after handoff.
            store.update(new['id'], {'revision': 1, 'brief': 'Back in Python'})
        self.start_editor()
        self.assertIn('New Go project', self.page())

    def test_python_and_go_exclude_each_other_and_release_after_crash(self):
        with server.Store(self.data):
            result = subprocess.run([self.binary, '-editor', '-data-dir', str(self.data)],
                                    capture_output=True, text=True, timeout=10)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn('owns this data directory', result.stderr)
        self.start_editor()
        with self.assertRaisesRegex(RuntimeError, 'owns this data directory'):
            server.Store(self.data)
        result = subprocess.run([self.binary, '-editor', '-data-dir', str(self.data)],
                                capture_output=True, text=True, timeout=10)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('owns this data directory', result.stderr)
        self.stop_editor()
        with server.Store(self.data) as store:
            self.assertEqual(store.load(self.pid)['title'], self.original['title'])

    def test_explicit_initialization_and_missing_folder_guard(self):
        missing = Path(self.temp.name)/'new'
        result = subprocess.run([self.binary, '-board-preview', '-data-dir', str(missing)],
                                capture_output=True, text=True, timeout=10)
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(missing.exists())
        result = subprocess.run([self.binary, '-board-preview', '-data-dir', str(missing), '-init-data-dir'],
                                capture_output=True, text=True, timeout=10)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('GoSX storyboard preview', result.stdout)
        with server.Store(missing) as store:
            self.assertEqual(store.listing(), [])

    def test_go_derived_values_match_python_approval_and_prompt_contracts(self):
        p = self.original
        shot = p['shots'][0]
        oracle = Path(self.temp.name)/'oracle.json'
        oracle.write_text(json.dumps({'project': p, 'fingerprint': server.fingerprint(p, shot),
            'direction_basis': server.direction_basis(p, shot), 'prompt': server.prompt_package(p, shot),
            'music_timing': server.music_timing(p)}, ensure_ascii=False), encoding='utf-8')
        result = subprocess.run(['go', 'test', '-count=1', '-run', '^TestPythonDerivedParity$', './...'],
            cwd=APP/'gosx-host', env=dict(os.environ, STORYBOARD_PARITY_ORACLE=str(oracle)),
            capture_output=True, text=True, timeout=120)
        self.assertEqual(result.returncode, 0, result.stdout+result.stderr)

    def test_default_go_launch_works_without_python_and_releases_storage_on_exit(self):
        settings = Path(self.temp.name)/'runtime-settings.json'
        settings.write_text('{}', encoding='utf-8')
        self.process = subprocess.Popen([self.binary, '-no-open', '-port', '0',
            '-data-dir', str(self.data), '-settings', str(settings)], env=dict(os.environ, PATH=''),
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, encoding='utf-8')
        line = self.process.stdout.readline().strip()
        self.assertTrue(line.startswith('GoSX workspace: '), line)
        self.url = line.removeprefix('GoSX workspace: ')
        with self.opener.open(self.url+'api/health', timeout=10) as response:
            self.assertEqual(json.load(response)['backend'], 'go')
        for view in ('board', 'episode', 'review', 'direction', 'references', 'handoff', 'library', 'music', 'sections', 'exports'):
            with self.opener.open(self.url+'?view='+view, timeout=10) as response:
                text = response.read().decode('utf-8')
                self.assertIn('/workspace.js', text, view)
                self.assertNotIn('/app.js', text, view)
        self.stop_editor()
        with server.Store(self.data) as store:
            self.assertEqual(store.load(self.pid)['future'], self.original['future'])

    @unittest.skipIf(os.name == 'nt', 'POSIX launcher interrupt; Windows native close is verified separately')
    def test_python_compat_launcher_interrupt_stops_its_owned_go_child(self):
        settings = Path(self.temp.name)/'runtime-settings.json'
        settings.write_text('{}', encoding='utf-8')
        command = [self.binary, '-no-open', '-port', '0', '-data-dir', str(self.data), '-settings', str(settings)]
        script = ('import launch, argparse\n'
                  'launch.go_command = lambda: '+repr(command)+'\n'
                  'args = argparse.Namespace(browser=False, gosx=False, background=False, ask_openrouter_key=False)\n'
                  'raise SystemExit(launch.launch_go(args))\n')
        self.process = subprocess.Popen([sys.executable, '-c', script], cwd=APP,
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, encoding='utf-8',
            env=dict(os.environ, PATH='', PYTHONDONTWRITEBYTECODE='1'))
        line = self.process.stdout.readline().strip()
        self.assertTrue(line.startswith('GoSX workspace: '), line)
        self.process.send_signal(signal.SIGINT)
        _, error = self.process.communicate(timeout=15)
        self.assertEqual(self.process.returncode, 130, error)
        self.process = None
        with server.Store(self.data) as store:
            self.assertEqual(store.load(self.pid)['future'], self.original['future'])

    def test_complete_go_project_and_jobs_reopen_in_python(self):
        import cloud
        root = Path(self.temp.name)/'complete-go-fixture'
        result = subprocess.run(['go', 'test', '-count=1', '-run', '^TestCompleteCompatibilityFixture$', './...'],
            cwd=APP/'gosx-host', env=dict(os.environ, STORYBOARD_COMPLETE_FIXTURE=str(root)),
            capture_output=True, text=True, timeout=120)
        self.assertEqual(result.returncode, 0, result.stdout+result.stderr)
        with server.Store(root) as store:
            p = store.listing()[0]
            self.assertEqual(p['title'], 'Go compatibility 雪')
            self.assertEqual(p['lyrics'], 'Original lyrics “here”\n  Keep spacing')
            self.assertEqual(p['soundtrack']['duration_seconds'], 10)
            self.assertEqual(p['music_sections'][0]['end_seconds'], 5)
            self.assertEqual(p['music_cues'][0]['end_seconds'], 6.5)
            shot = p['shots'][0]
            self.assertTrue(shot['animation_ready'])
            self.assertEqual(len(shot['assets']), 3)
            self.assertTrue(any(a.get('local_generation') for a in shot['assets']))
            self.assertTrue(any(a.get('cloud_generation') for a in shot['assets']))
            engine = cloud.CloudEngine(store, server.approved, server.direction_basis, server.prompt_package,
                                       client=object(), start_worker=False)
            state = engine.status(p['id'])
            self.assertEqual(state['committed_usd'], .18)
            self.assertEqual(state['jobs'][0]['status'], 'done')
            self.assertEqual(state['brief']['approved_sha256'], cloud.digest(state['brief']['content']))

    def test_existing_legacy_worker_is_detected_before_opening_projects(self):
        path = self.data/'cloud/worker.lock'
        with FileLock(path) as legacy:
            self.assertTrue(legacy.acquire())
            before = self.path.read_bytes()
            result = subprocess.run([self.binary, '-editor', '-data-dir', str(self.data)],
                                    capture_output=True, text=True, timeout=10)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn('older Storyboard Desk generation worker', result.stderr)
            self.assertEqual(self.path.read_bytes(), before)
        self.start_editor()
        with FileLock(path) as legacy:
            self.assertFalse(legacy.acquire())
        self.stop_editor()
        with FileLock(path) as legacy:
            self.assertTrue(legacy.acquire())
