"""Small OS adapters. Locks are held by the OS and released after a crash."""
import errno
import os
from pathlib import Path
import signal
import stat
import subprocess

if os.name == 'nt':
    import msvcrt
else:
    import fcntl


class FileLock:
    def __init__(self, path):
        path = Path(path)
        if path.is_symlink():
            raise ValueError('Lock file must be a regular file.')
        fd = os.open(path, os.O_CREAT | os.O_RDWR | getattr(os, 'O_NOFOLLOW', 0), 0o600)
        try:
            if not stat.S_ISREG(os.fstat(fd).st_mode):
                raise ValueError('Lock file must be a regular file.')
            self.file = os.fdopen(fd, 'r+b')
        except BaseException:
            os.close(fd)
            raise
        self.locked = False

    def acquire(self):
        if self.locked:
            return True
        try:
            if os.name == 'nt':
                self.file.seek(0)
                msvcrt.locking(self.file.fileno(), msvcrt.LK_NBLCK, 1)
            else:
                fcntl.flock(self.file, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except OSError as exc:
            if exc.errno in (errno.EACCES, errno.EAGAIN, errno.EDEADLK):
                return False
            raise
        self.locked = True
        # Both OS adapters can lock past EOF. Initialize only after acquiring so
        # a Windows contender never writes into another owner's locked byte.
        if self.file.seek(0, 2) == 0:
            self.file.write(b'\0')
            self.file.flush()
        return True

    def close(self):
        if self.file.closed:
            return
        try:
            if self.locked and os.name == 'nt':
                self.file.seek(0)
                msvcrt.locking(self.file.fileno(), msvcrt.LK_UNLCK, 1)
        finally:
            self.locked = False
            self.file.close()

    def __enter__(self):
        return self

    def __exit__(self, *args):
        self.close()


def stop_process(process):
    """Stop only a child owned by this launcher, on console and GUI hosts."""
    if process.poll() is not None:
        return
    if os.name == 'nt':
        process.terminate()
    else:
        process.send_signal(signal.SIGINT)
    try:
        process.wait(timeout=25)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait(timeout=5)
