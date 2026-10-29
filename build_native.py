#!/usr/bin/env python3
"""Build the small macOS video helpers for this Mac's architecture."""
from pathlib import Path
import os
import platform
import shutil
import subprocess
import sys
import tempfile

APP = Path(__file__).resolve().parent
HELPERS = ('media_probe', 'export_range', 'export_reel')


def main():
    if sys.platform != 'darwin':
        raise SystemExit('Native video helpers require macOS. Board planning and manual media organization can run without them.')
    xcrun = shutil.which('xcrun')
    if not xcrun:
        raise SystemExit('Install Apple Command Line Tools first: xcode-select --install')
    compiler = subprocess.run([xcrun, '--find', 'swiftc'], capture_output=True, text=True)
    if compiler.returncode:
        raise SystemExit('Swift compiler unavailable. Install Apple Command Line Tools: xcode-select --install')
    sdk = subprocess.run([xcrun, '--sdk', 'macosx', '--show-sdk-path'], capture_output=True, text=True, check=True).stdout.strip()
    target_arch = platform.machine()
    if target_arch not in ('arm64', 'x86_64'):
        raise SystemExit('Build the helpers on an Apple Silicon or Intel Mac.')
    with tempfile.TemporaryDirectory(prefix='sbd-build-', dir=APP) as tmp:
        for name in HELPERS:
            target = Path(tmp)/name
            print('Building '+name+'…', flush=True)
            subprocess.run([xcrun, '--sdk', 'macosx', 'swiftc', '-sdk', sdk,
                            '-target', target_arch+'-apple-macosx13.0', '-O',
                            str(APP/(name+'.swift')), '-o', str(target)], check=True)
            target.chmod(0o755)
            os.replace(target, APP/name)
    print('Video helpers ready. Run: python3 launch.py')


if __name__ == '__main__':
    main()
