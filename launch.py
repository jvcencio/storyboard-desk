#!/usr/bin/env python3
"""Start/reuse the local storyboard app. No login item or service installation."""
from pathlib import Path
import argparse
import getpass
import json
import os
import subprocess
import sys
import time
import urllib.request
import urllib.error
import webbrowser

APP = Path(__file__).resolve().parent
sys.path.insert(0, str(APP))
from settings import CONFIG
from platform_support import FileLock, stop_process
URL = 'http://127.0.0.1:%d/' % CONFIG['port']
DATA = CONFIG['data_dir']
_owned_children = {}


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
    default_state = Path.home()/'Library/Logs/Storyboard Desk' if sys.platform == 'darwin' else Path(data_dir)/'runtime'
    state_dir = Path(state_dir or default_state)
    state_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
    # Concurrent button clicks/launches share one startup decision.
    with FileLock(state_dir/'launch.lock') as lock:
        deadline = time.monotonic() + 20
        while not lock.acquire():
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
                [sys.executable, '-X', 'utf8', '-I', '-B', str(APP/'server.py'), '--data-dir', str(data_dir), '--port', str(port)],
                stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT,
                start_new_session=os.name != 'nt', close_fds=True,
                creationflags=subprocess.CREATE_NO_WINDOW if os.name == 'nt' else 0,
                env=dict(os.environ, PYTHONDONTWRITEBYTECODE='1'))
        try:
            startup_deadline = time.monotonic() + 15
            while time.monotonic() < startup_deadline:
                if proc.poll() is not None:
                    raise RuntimeError('Storyboard Desk could not start. Check '+str(log_path))
                if ready(url):
                    _owned_children[proc.pid] = proc
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



def stop_background(reply):
    """For an embedding caller: stop only a server this process actually started."""
    process = _owned_children.pop(reply.get('pid'), None)
    if process is not None:
        stop_process(process)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--gosx', action='store_true', help='Open the optional GoSX native desktop host instead of the browser')
    parser.add_argument('--background', action='store_true', help='Start/reuse the server without a Terminal lifetime')
    parser.add_argument('--no-open', action='store_true', help='Return startup JSON for Mini Meter; do not open a browser')
    parser.add_argument('--ask-openrouter-key', action='store_true', help='Read an optional API key without echo/history; keep it only in this server process environment')
    args = parser.parse_args()
    if args.gosx and args.background:
        parser.error('--gosx runs in the foreground; omit --background')
    host = None
    if args.gosx:
        default_host = APP/'gosx-host'/('storyboard-desk-gosx.exe' if os.name == 'nt' else 'storyboard-desk-gosx')
        if sys.platform == 'darwin':
            default_host = APP/'gosx-host/Storyboard Desk.app/Contents/MacOS/storyboard-desk-gosx'
        host = Path(os.environ.get('STORYBOARD_GOSX_HOST', str(default_host)))
        if not host.is_file():
            parser.error('Build the GoSX host first: python3 build_desktop.py. See docs/PORTABILITY.md.')
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
            webbrowser.open(URL)
        print(json.dumps(result), flush=True)
        return
    proc = None
    if not ready():
        proc = subprocess.Popen([sys.executable, '-X', 'utf8', str(APP/'server.py'), '--data-dir', str(DATA), '--port', str(CONFIG['port'])])
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
    try:
        if host:
            host_env = {k:v for k,v in os.environ.items() if k != 'OPENROUTER_API_KEY'}
            result = subprocess.run([str(host), '--url', URL], env=host_env)
            if result.returncode:
                raise SystemExit('The GoSX desktop host could not open. See docs/PORTABILITY.md; browser mode remains available.')
        else:
            webbrowser.open(URL)
            print('Storyboard Desk is open at '+URL, flush=True)
            if proc:
                print('Keep this window open. Control-C stops the app; project files remain in '+str(DATA)+'.', flush=True)
                proc.wait()
    except KeyboardInterrupt:
        pass
    finally:
        if proc:
            stop_process(proc)


if __name__ == '__main__':
    main()
