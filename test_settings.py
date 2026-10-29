"""A fresh checkout must not depend on the creator's drives or credentials."""
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import cloud
import settings


class SettingsChecks(unittest.TestCase):
    def test_fresh_mac_uses_user_storage_without_creating_it(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)/'home'
            value = settings.load_settings({}, Path(tmp)/'missing.json', home, 'darwin')
            self.assertEqual(value['data_dir'], (home/'Library/Application Support/Storyboard Desk').resolve())
            self.assertFalse(home.exists())

    def test_environment_overrides_local_machine_settings(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp)/'settings.json'
            path.write_text(json.dumps({'data_dir': tmp+'/first', 'port': 4500}))
            value = settings.load_settings({'STORYBOARD_DATA_DIR': tmp+'/second', 'STORYBOARD_PORT': '4501'}, path)
            self.assertEqual(value['data_dir'], (Path(tmp)/'second').resolve())
            self.assertEqual(value['port'], 4501)

    def test_invalid_config_and_credentials_are_not_accepted_as_settings(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp)/'settings.json'
            path.write_text(json.dumps({'OPENROUTER_API_KEY': 'not-a-real-key'}))
            with self.assertRaisesRegex(ValueError, 'credentials'):
                settings.load_settings({}, path)
            for port in ('0', '65536', '4492.5'):
                with self.assertRaises(ValueError):
                    settings.load_settings({'STORYBOARD_PORT': port}, Path(tmp)/'missing')

    def test_standalone_api_key_is_read_from_environment(self):
        with patch.dict(os.environ, {'OPENROUTER_API_KEY': '  fixture-only-key  '}, clear=True), \
                patch.object(cloud.importlib.util, 'spec_from_file_location') as legacy:
            self.assertEqual(cloud.keychain_key(), 'fixture-only-key')
            legacy.assert_not_called()

    def test_missing_standalone_key_has_actionable_error(self):
        with tempfile.TemporaryDirectory() as tmp, patch.dict(os.environ, {}, clear=True), \
                patch.object(cloud, 'APP', Path(tmp)/'app'):
            with self.assertRaisesRegex(ValueError, 'ask-openrouter-key'):
                cloud.keychain_key()

    def test_existing_workstation_keychain_adapter_still_supported(self):
        with tempfile.TemporaryDirectory() as tmp, patch.dict(os.environ, {}, clear=True), \
                patch.object(cloud, 'APP', Path(tmp)/'app'):
            path = Path(tmp)/'buckley-workstation/scripts/workstation_credentials.py'
            path.parent.mkdir(parents=True)
            path.write_text('def find(provider):\n    return "fixture-workstation-key", {}\n')
            self.assertEqual(cloud.keychain_key(), 'fixture-workstation-key')
