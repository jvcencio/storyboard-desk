"""Portable paths, with optional machine-local overrides. No credentials here."""
from pathlib import Path
import json
import os
import shutil
import sys

APP = Path(__file__).resolve().parent
ENV = {
    'data_dir': 'STORYBOARD_DATA_DIR', 'port': 'STORYBOARD_PORT',
    'draw_cli': 'STORYBOARD_DRAW_CLI', 'models_dir': 'STORYBOARD_MODELS_DIR',
    'model': 'STORYBOARD_DRAW_MODEL', 'meter': 'STORYBOARD_METER',
    'reference_cache': 'STORYBOARD_REFERENCE_CACHE',
}


def load_settings(environ=None, local_path=None, home=None, platform=None):
    environ = os.environ if environ is None else environ
    home = Path.home() if home is None else Path(home)
    platform = sys.platform if platform is None else platform
    base = home/'Library/Application Support/Storyboard Desk' if platform == 'darwin' else home/'.local/share/storyboard-desk'
    values = {'data_dir': str(base), 'port': 4492,
              'draw_cli': shutil.which('draw-things-cli') or '/opt/homebrew/bin/draw-things-cli',
              'models_dir': str(base/'models'), 'model': 'juggernaut_xl_v9_lightning_q6p_q8p.ckpt',
              'meter': str(home/'Applications/Mini Meter.app/Contents/MacOS/MiniMeter'),
              'reference_cache': str(APP/'references')}
    path = Path(local_path) if local_path is not None else APP/'settings.local.json'
    if path.is_file():
        local = json.loads(path.read_text())
        if not isinstance(local, dict) or set(local)-set(ENV):
            raise ValueError('Use only the documented settings in settings.local.json; credentials belong in the server environment.')
        values.update(local)
    values.update({key: environ[name] for key, name in ENV.items() if name in environ})
    if isinstance(values['port'], bool):
        raise ValueError('STORYBOARD_PORT must be an integer between 1 and 65535.')
    try:
        port = int(values['port'])
    except (TypeError, ValueError):
        raise ValueError('STORYBOARD_PORT must be an integer between 1 and 65535.') from None
    if str(port) != str(values['port']) or not 1 <= port <= 65535:
        raise ValueError('STORYBOARD_PORT must be an integer between 1 and 65535.')
    values['port'] = port
    for key in ('data_dir', 'draw_cli', 'models_dir', 'meter', 'reference_cache'):
        if not isinstance(values[key], str) or not values[key].strip():
            raise ValueError('Choose a nonempty path for '+key+'.')
        values[key] = Path(values[key]).expanduser().resolve()
    if not isinstance(values['model'], str) or not values['model'] or Path(values['model']).name != values['model']:
        raise ValueError('Choose a model filename inside the configured models directory.')
    return values


CONFIG = load_settings()
