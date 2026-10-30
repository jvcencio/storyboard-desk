"""Verify the checked-in shot card matches GoSX compiler output when available."""
from pathlib import Path
import shutil
import subprocess
import unittest


@unittest.skipUnless(shutil.which('gosx'), 'GoSX compiler is not installed')
class GoSXCompileChecks(unittest.TestCase):
    maxDiff = None

    def test_shot_card_generated_source(self):
        root = Path(__file__).parent/'gosx-host'
        result = subprocess.run(['gosx', 'compile', 'board.gsx'], cwd=root,
                                capture_output=True, text=True, check=True, timeout=60)
        def formatted(source):
            return subprocess.run(['gofmt'], input=source, capture_output=True,
                                  text=True, check=True, timeout=60).stdout
        # The checked-in source has a generated-code notice; GoSX's stdout does not.
        checked_in = (root/'board_generated.go').read_text().split('\n', 1)[1]
        self.assertEqual(formatted(checked_in), formatted(result.stdout))
