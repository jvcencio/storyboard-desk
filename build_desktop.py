#!/usr/bin/env python3
"""Build the optional GoSX host; never starts a backend or submits model work."""
import argparse
import hashlib
import io
import os
from pathlib import Path
import platform
import shutil
import subprocess
import sys
import urllib.request
import zipfile

APP = Path(__file__).resolve().parent
WEBVIEW2_VERSION = '1.0.4314.35'
WEBVIEW2_SHA256 = '90778deb1b77e36292828b4b5833c9255c27533f40e0ddd55891ae718944edc4'


def install_loader(folder, arch):
    # Microsoft-supplied loader only. The installed Evergreen Runtime stays separate.
    sdk = folder/'webview2-sdk.nupkg'
    if not sdk.is_file() or hashlib.sha256(sdk.read_bytes()).hexdigest() != WEBVIEW2_SHA256:
        url = ('https://api.nuget.org/v3-flatcontainer/microsoft.web.webview2/'
               +WEBVIEW2_VERSION+'/microsoft.web.webview2.'+WEBVIEW2_VERSION+'.nupkg')
        with urllib.request.urlopen(url, timeout=60) as response:
            raw = response.read(32*1024*1024+1)
        if len(raw) > 32*1024*1024 or hashlib.sha256(raw).hexdigest() != WEBVIEW2_SHA256:
            raise SystemExit('WebView2 SDK checksum mismatch; no loader was installed.')
        sdk.write_bytes(raw)
    with zipfile.ZipFile(io.BytesIO(sdk.read_bytes())) as package:
        name = 'arm64' if arch == 'arm64' else 'x64'
        (folder/'WebView2Loader.dll').write_bytes(package.read('build/native/'+name+'/WebView2Loader.dll'))
        (folder/'WebView2-LICENSE.txt').write_bytes(package.read('LICENSE.txt'))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--target', choices=('windows-amd64','windows-arm64','darwin-amd64','darwin-arm64'))
    parser.add_argument('--output-dir', type=Path, default=APP/'gosx-host')
    args = parser.parse_args()
    if args.target:
        system, arch = args.target.split('-')
    else:
        system = {'win32':'windows','darwin':'darwin'}.get(sys.platform)
        arch = {'arm64':'arm64','aarch64':'arm64','x86_64':'amd64','amd64':'amd64'}.get(platform.machine().lower())
        if not system or not arch:
            raise SystemExit('GoSX desktop currently targets Windows and Mac. Use python3 launch.py in your Linux browser.')
    compiler = shutil.which(os.environ.get('GO', 'go'))
    if not compiler:
        raise SystemExit('Install Go 1.26 or newer to build the optional desktop host.')
    folder = args.output_dir.resolve(); folder.mkdir(parents=True, exist_ok=True)
    target = folder/('storyboard-desk-gosx.exe' if system == 'windows' else 'storyboard-desk-gosx')
    env = dict(os.environ, GOOS=system, GOARCH=arch, CGO_ENABLED='0')
    subprocess.run([compiler, 'build', '-trimpath', '-o', str(target), '.'], cwd=APP/'gosx-host', env=env, check=True)
    if system == 'windows':
        install_loader(folder, arch)
    print('Built '+str(target))
    print('Start: python3 launch.py --gosx')


if __name__ == '__main__':
    main()
