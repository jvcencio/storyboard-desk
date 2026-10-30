"""Exercise the Go compatibility client against the actual local Python server."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import threading
import unittest

import server


@unittest.skipUnless(shutil.which('go'), 'Go is required for the compatibility test')
class GoSXBridgeChecks(unittest.TestCase):
    def test_real_service_read_save_and_revision_conflict(self):
        with tempfile.TemporaryDirectory() as root:
            store = server.Store(root)
            store.new({'title': 'Test', 'shots': [{'title': 'Before', 'composition': 'Keep the framing'}]})
            listener = server.LocalHTTPServer(('127.0.0.1', 0), server.Handler)
            listener.store = store
            listener.token = 'test-token'
            thread = threading.Thread(target=listener.serve_forever, daemon=True)
            thread.start()
            try:
                env = dict(os.environ, STORYBOARD_TEST_URL=f'http://127.0.0.1:{listener.server_port}/')
                result = subprocess.run(['go', 'test', '-count=1', './...'],
                                        cwd=Path(__file__).parent/'gosx-host', env=env,
                                        capture_output=True, text=True, timeout=120)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                result = subprocess.run(['go', 'run', '.', '--url', env['STORYBOARD_TEST_URL'],
                                         '--board-preview'],
                                        cwd=Path(__file__).parent/'gosx-host', env=env,
                                        capture_output=True, text=True, timeout=120)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertIn('GoSX edit', result.stdout)
                self.assertNotIn('test-token', result.stdout)

            finally:
                listener.shutdown()
                listener.server_close()
                thread.join()
