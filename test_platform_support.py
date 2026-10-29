"""Actual OS lock behavior, including a holder that crashes."""
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

from platform_support import FileLock

APP = Path(__file__).resolve().parent


class LockChecks(unittest.TestCase):
    def test_separate_process_is_excluded_then_can_acquire_after_close(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp)/'owner.lock'
            code = 'from platform_support import FileLock; import sys; lock=FileLock(sys.argv[1]); print(lock.acquire()); lock.close()'
            with FileLock(path) as owner:
                self.assertTrue(owner.acquire())
                result = subprocess.run([sys.executable, '-c', code, str(path)], cwd=APP, capture_output=True, text=True, check=True)
                self.assertEqual(result.stdout.strip(), 'False')
            result = subprocess.run([sys.executable, '-c', code, str(path)], cwd=APP, capture_output=True, text=True, check=True)
            self.assertEqual(result.stdout.strip(), 'True')

    def test_crashed_holder_releases_the_os_lock(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp)/'owner.lock'
            code = 'from platform_support import FileLock; import sys,time; lock=FileLock(sys.argv[1]); print(lock.acquire(),flush=True); time.sleep(30)'
            process = subprocess.Popen([sys.executable, '-c', code, str(path)], cwd=APP, stdout=subprocess.PIPE, text=True)
            try:
                self.assertEqual(process.stdout.readline().strip(), 'True')
                with FileLock(path) as contender:
                    self.assertFalse(contender.acquire())
                    process.kill(); process.wait(timeout=5)
                    self.assertTrue(contender.acquire())
            finally:
                if process.poll() is None: process.kill(); process.wait(timeout=5)
                process.stdout.close()
