"""On-demand startup contracts, using temporary projects and loopback ports."""
import concurrent.futures
from http.server import BaseHTTPRequestHandler
import json
import os
from pathlib import Path
import signal
import socket
import tempfile
import threading
import unittest
from unittest.mock import patch
import urllib.request

import launch
from server import LocalHTTPServer


def free_url():
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        return 'http://127.0.0.1:%d/' % sock.getsockname()[1]



def prepare_with_diagnostics(data, url, state):
    try:
        return launch.prepare_background(data, url, state)
    except Exception as exc:
        log = Path(state)/'server.log'
        details = log.read_text(encoding='utf-8')[-6000:] if log.exists() else '(no server log)'
        raise RuntimeError(str(exc)+'\nTemporary server log:\n'+details) from exc


def stop_owned(reply):
    launch.stop_background(reply)


class LaunchChecks(unittest.TestCase):
    def test_explicit_new_storage_can_be_created_without_starting_another_server(self):
        with tempfile.TemporaryDirectory() as root, patch.object(launch, 'ready', return_value=True):
            data = Path(root)/'new-projects'
            reply = launch.prepare_background(data, create=True)
            self.assertEqual(reply['status'], 'reused')
            self.assertTrue(data.is_dir())

    @unittest.skipIf(os.name == 'nt', 'POSIX /Volumes mount guard')
    def test_missing_volume_cannot_be_created_even_with_create_enabled(self):
        with patch.object(Path, 'is_mount', return_value=False), patch.object(Path, 'mkdir') as mkdir:
            with self.assertRaisesRegex(RuntimeError, 'configured storage volume'):
                launch.prepare_background('/Volumes/UnavailableStoryboardDisk/projects', create=True)
            mkdir.assert_not_called()

    def test_missing_storage_does_not_reuse_or_start(self):
        with tempfile.TemporaryDirectory() as root, patch.object(launch, 'ready') as ready:
            with self.assertRaisesRegex(RuntimeError, 'storage folder is unavailable'):
                launch.prepare_background(Path(root)/'missing')
            ready.assert_not_called()

    def test_running_board_needs_no_new_process_or_runtime_files(self):
        with tempfile.TemporaryDirectory() as root, patch.object(launch, 'ready', return_value=True), \
                patch.object(launch.subprocess, 'Popen') as popen:
            state = Path(root)/'logs'
            reply = launch.prepare_background(root, state_dir=state)
            self.assertEqual(reply['status'], 'reused')
            self.assertFalse(state.exists())
            popen.assert_not_called()

    def test_real_cold_start_and_reuse_keep_one_server(self):
        with tempfile.TemporaryDirectory() as root:
            data = Path(root)/'projects'
            data.mkdir()
            url = free_url()
            first = prepare_with_diagnostics(data, url, Path(root)/'logs')
            try:
                self.assertEqual(first['status'], 'started')
                self.assertTrue(launch.ready(url))
                second = prepare_with_diagnostics(data, url, Path(root)/'logs')
                self.assertEqual(second['status'], 'reused')
                self.assertNotIn('pid', second)
                self.assertEqual(len(list(data.glob('*/project.json'))), 1)
            finally:
                stop_owned(first)

    def test_concurrent_launches_start_only_one_server(self):
        with tempfile.TemporaryDirectory() as root:
            data = Path(root)/'projects'
            data.mkdir()
            url = free_url()
            replies = []
            try:
                with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
                    futures = [pool.submit(prepare_with_diagnostics, data, url, Path(root)/'logs') for _ in range(2)]
                    replies = [future.result(timeout=35) for future in futures]
                self.assertEqual(sorted(r['status'] for r in replies), ['reused', 'started'])
                self.assertEqual(len(list(data.glob('*/project.json'))), 1)
            finally:
                for reply in replies:
                    stop_owned(reply)

    def test_foreign_listener_is_not_accepted_or_terminated(self):
        class Foreign(BaseHTTPRequestHandler):
            def do_GET(self):
                self.send_response(200)
                self.end_headers()
                self.wfile.write(json.dumps({'app': 'Another app'}).encode())
            def log_message(self, *args):
                pass
        httpd = LocalHTTPServer(('127.0.0.1', 0), Foreign)
        worker = threading.Thread(target=httpd.serve_forever, daemon=True)
        worker.start()
        try:
            url = 'http://127.0.0.1:%d/' % httpd.server_port
            with tempfile.TemporaryDirectory() as root:
                with self.assertRaisesRegex(RuntimeError, 'could not start'):
                    launch.prepare_background(root, url, Path(root)/'logs')
                opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
                with opener.open(url, timeout=2) as response:
                    self.assertEqual(json.load(response)['app'], 'Another app')
        finally:
            httpd.shutdown()
            httpd.server_close()
            worker.join(timeout=2)


if __name__ == '__main__':
    unittest.main()
