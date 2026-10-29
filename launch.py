#!/usr/bin/env python3
"""Start/reuse the local storyboard app. No login item or service installation."""
from pathlib import Path
import argparse
import fcntl
import getpass
import json
import os
import subprocess
import sys
import time
import urllib.request
import urllib.error

APP = Path(__file__).resolve().parent
sys.path.insert(0, str(APP))
from settings import CONFIG
URL = 'http://127.0.0.1:%d/' % CONFIG['port']
DATA = CONFIG['data_dir']


def ready(url=URL):
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    try:
        with opener.open(url+'api/health', timeout=1) as r:
            return json.load(r).get('app') == 'Storyboard Desk'
    except (OSError, ValueError):
        return False


def prepare_background(data_dir=DATA, url=URL, state_dir=None, create=False):
    """On-demand startup for Mini Meter; no browser, model or login service."""
    data_dir = Path(data_dir)
    if str(data_dir).startswith('/Volumes/') and not (Path('/Volumes')/data_dir.parts[2]).is_mount():
        raise RuntimeError('Connect the configured storage volume before opening Storyboard Desk: '+str(data_dir))
    if not data_dir.is_dir():
        if not create:
            raise RuntimeError('Project storage folder is unavailable: '+str(data_dir))
        data_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
    if ready(url):
        return {'app': 'Storyboard Desk', 'url': url, 'status': 'reused'}
    state_dir = Path(state_dir or Path.home()/'Library/Logs/Storyboard Desk')
    state_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
    # Concurrent button clicks/launches share one startup decision.
    with (state_dir/'launch.lock').open('a') as lock:
        deadline = time.monotonic() + 12
        while True:
            try:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                break
            except BlockingIOError:
                if time.monotonic() >= deadline:
                    raise RuntimeError('Storyboard Desk is still starting. Try again shortly.')
                time.sleep(.1)
        if ready(url):
            return {'app': 'Storyboard Desk', 'url': url, 'status': 'reused'}
        from urllib.parse import urlsplit
        port = urlsplit(url).port
        log_path = state_dir/'server.log'
        with log_path.open('ab') as log:
            proc = subprocess.Popen(
                [sys.executable, '-I', '-B', str(APP/'server.py'), '--data-dir', str(data_dir), '--port', str(port)],
                stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT,
                start_new_session=True, close_fds=True,
                env=dict(os.environ, PYTHONDONTWRITEBYTECODE='1'))
        try:
            startup_deadline = time.monotonic() + 15
            while time.monotonic() < startup_deadline:
                if proc.poll() is not None:
                    raise RuntimeError('Storyboard Desk could not start. Check '+str(log_path))
                if ready(url):
                    return {'app': 'Storyboard Desk', 'url': url, 'status': 'started', 'pid': proc.pid}
                time.sleep(.1)
            raise RuntimeError('Storyboard Desk did not become ready. Check '+str(log_path))
        except BaseException:
            if proc.poll() is None:
                proc.terminate()
                try:
                    proc.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    proc.kill()
                    proc.wait(timeout=5)
            raise


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--background', action='store_true', help='Start/reuse the server without a Terminal lifetime')
    parser.add_argument('--no-open', action='store_true', help='Return startup JSON for Mini Meter; do not open a browser')
    parser.add_argument('--ask-openrouter-key', action='store_true', help='Read an optional API key without echo/history; keep it only in this server process environment')
    args = parser.parse_args()
    if args.no_open and not args.background:
        parser.error('--no-open requires --background')
    if args.ask_openrouter_key:
        if ready():
            raise SystemExit('Stop the existing Storyboard Desk server before starting a session with a new API key.')
        key = getpass.getpass('OpenRouter API key (hidden; not saved): ').strip()
        if not key:
            raise SystemExit('No API key supplied.')
        os.environ['OPENROUTER_API_KEY'] = key
    if args.background:
        try:
            result = prepare_background(create=True)
        except (OSError, ValueError, RuntimeError) as error:
            raise SystemExit(str(error))
        if not args.no_open:
            subprocess.run(['/usr/bin/open', URL], check=True)
        print(json.dumps(result), flush=True)
        return
    proc = None
    if not ready():
        proc = subprocess.Popen([sys.executable, str(APP/'server.py'), '--data-dir', str(DATA), '--port', str(CONFIG['port'])])
        for _ in range(80):
            if proc.poll() is not None:
                raise SystemExit('Storyboard Desk could not start. Check the configured storage and the message above.')
            if ready():
                break
            time.sleep(.1)
        else:
            proc.terminate()
            proc.wait(timeout=5)
            raise SystemExit('The local dashboard did not become ready.')
    subprocess.run(['/usr/bin/open', URL], check=True)
    print('Storyboard Desk is open at '+URL, flush=True)
    if proc:
        print('Keep this window open. Control-C stops the app; project files remain in '+str(DATA)+'.', flush=True)
        try:
            proc.wait()
        except KeyboardInterrupt:
            proc.send_signal(2)
            proc.wait(timeout=25)


if __name__ == '__main__':
    main()
