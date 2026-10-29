"""Real FFmpeg exports, silent output and cumulative section timing."""
import hashlib
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import media_tools as media


class PortableMediaChecks(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.ffmpeg = shutil.which(os.environ.get('STORYBOARD_FFMPEG', 'ffmpeg'))
        cls.ffprobe = shutil.which(os.environ.get('STORYBOARD_FFPROBE', 'ffprobe'))
        if not cls.ffmpeg or not cls.ffprobe:
            if os.environ.get('STORYBOARD_REQUIRE_FFMPEG') == '1':
                raise AssertionError('CI requires real ffmpeg and ffprobe; install both before running this suite.')
            raise unittest.SkipTest('Optional FFmpeg tools are not installed')
        cls.temp = tempfile.TemporaryDirectory(); cls.root = Path(cls.temp.name)
        cls.sources = []
        for color, size, fps in [('red', '320x180', '24'), ('blue', '180x320', '30')]:
            path = cls.root/(color+" 'café'.mp4")
            subprocess.run([cls.ffmpeg, '-v', 'error', '-nostdin', '-f', 'lavfi', '-i', f'color={color}:s={size}:r={fps}:d=2',
                            '-f', 'lavfi', '-i', 'sine=frequency=440:duration=2', '-map', '0:v:0', '-map', '1:a:0',
                            '-c:v', 'libx264', '-pix_fmt', 'yuv420p', '-threads', '2', '-c:a', 'aac', '-shortest', str(path)], check=True)
            cls.sources.append(path)
        cls.hashes = [hashlib.sha256(p.read_bytes()).hexdigest() for p in cls.sources]

    @classmethod
    def tearDownClass(cls):
        cls.temp.cleanup()

    def setUp(self):
        self.config = patch.dict(media.CONFIG, {'media_backend': 'ffmpeg', 'ffmpeg': self.ffmpeg, 'ffprobe': self.ffprobe})
        self.config.start(); self.addCleanup(self.config.stop)
        self.work = tempfile.TemporaryDirectory(); self.addCleanup(self.work.cleanup); self.folder = Path(self.work.name)

    def test_probe_and_silent_range_preserve_original(self):
        info = media.probe_video(self.sources[0], self.folder)
        self.assertEqual((info['width'], info['height'], info['audio_tracks']), (320, 180, 1))
        self.assertTrue(Path(info['poster']).read_bytes().startswith(b'\xff\xd8\xff'))
        target = self.folder/'range.mp4'
        exported = media.render_range(self.sources[0], target, .25, 1.25)
        self.assertEqual(exported['audio_tracks'], 0)
        self.assertAlmostEqual(exported['duration_seconds'], 1, delta=.05)
        self.assertEqual(hashlib.sha256(self.sources[0].read_bytes()).hexdigest(), self.hashes[0])
        with self.assertRaises(ValueError): media.render_range(self.sources[0], target, 0, 1)
        with self.assertRaises(ValueError): media.render_range(self.sources[0], self.folder/'bad.mp4', 0, 20)

    def test_mixed_geometry_reel_preserves_order_and_fractional_total(self):
        # Repeated fractional ranges used to accumulate rounding drift.
        segments = [{'path':str(self.sources[i % 2]), 'start':.1, 'end':.41} for i in range(10)]
        target = self.folder/'reel.mp4'
        result = media.render_reel(segments, target, '16:9')
        self.assertEqual((result['width'], result['height'], result['audio_tracks']), (1280,720,0))
        self.assertAlmostEqual(result['duration_seconds'], 3.1, delta=.05)
        for seconds, channel in [(0.1, 0), (.4, 2)]:
            rgb = subprocess.check_output([self.ffmpeg, '-v', 'error', '-ss', str(seconds), '-i', str(target),
                                           '-frames:v', '1', '-vf', 'crop=2:2:iw/2:ih/2,scale=1:1', '-pix_fmt', 'rgb24', '-f', 'rawvideo', '-'])
            self.assertGreater(rgb[channel], 180)
        self.assertEqual([hashlib.sha256(p.read_bytes()).hexdigest() for p in self.sources], self.hashes)

    def test_missing_tools_fail_with_setup_guidance(self):
        with patch.dict(media.CONFIG, {'ffprobe':'nonexistent-sbd-ffprobe'}):
            with self.assertRaisesRegex(ValueError, 'Install FFmpeg'):
                media.probe_video(self.sources[0], self.folder)
