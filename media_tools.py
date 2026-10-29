"""Silent picture inspection and export through native Mac tools or FFmpeg."""
import json
import math
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import time

from settings import CONFIG

APP = Path(__file__).resolve().parent
SIZES = {'16:9': (1280, 720), '9:16': (720, 1280), '1:1': (1080, 1080)}


def _native(name):
    choice = CONFIG['media_backend']
    path = APP/name
    if choice != 'ffmpeg' and sys.platform == 'darwin' and path.is_file():
        return path
    if choice == 'native':
        raise ValueError('Native video helpers require macOS and python3 build_native.py. Choose the ffmpeg backend on other systems.')
    return None


def _tool(name):
    configured = str(CONFIG[name])
    path = shutil.which(str(Path(configured).expanduser()))
    if not path:
        raise ValueError('Install FFmpeg including ffprobe and add both to PATH, or configure STORYBOARD_FFMPEG and STORYBOARD_FFPROBE. See README.md.')
    return path


def _run(command, timeout=120):
    try:
        result = subprocess.run([str(v) for v in command], capture_output=True, timeout=max(.1, timeout))
    except (OSError, subprocess.TimeoutExpired):
        raise ValueError('Local video processing failed or timed out. Original media is retained.') from None
    if result.returncode:
        raise ValueError('Local video processing failed. Check the clip and the configured video tools; original media is retained.')
    return result.stdout


def _metadata(path):
    data = json.loads(_run([_tool('ffprobe'), '-v', 'error', '-show_format', '-show_streams', '-of', 'json', path], 30))
    streams = data.get('streams', [])
    video = next((s for s in streams if s.get('codec_type') == 'video'), None)
    if not video:
        raise ValueError('The source has no readable video track.')
    duration = float(video.get('duration') or data.get('format', {}).get('duration', 0))
    width, height = int(video.get('width', 0)), int(video.get('height', 0))
    if not math.isfinite(duration) or duration <= 0 or min(width, height) <= 0:
        raise ValueError('The source video duration or dimensions are unavailable.')
    rotation = next((s.get('rotation', 0) for s in video.get('side_data_list', []) if 'rotation' in s), video.get('tags', {}).get('rotate', 0))
    if abs(float(rotation)) % 180 == 90:
        width, height = height, width
    return {'duration_seconds': duration, 'width': width, 'height': height,
            'audio_tracks': sum(s.get('codec_type') == 'audio' for s in streams)}


def require_video_tools():
    if not _native('media_probe'):
        _tool('ffmpeg'); _tool('ffprobe')


def probe_video(path, folder):
    path, folder = Path(path), Path(folder)
    helper = _native('media_probe')
    if helper:
        return json.loads(_run([helper, path, folder], 30))
    info = _metadata(path)
    poster = folder/'poster.jpg'
    _run([_tool('ffmpeg'), '-v', 'error', '-nostdin', '-y', '-ss', min(.1, info['duration_seconds']/2),
          '-i', path, '-map', '0:v:0', '-frames:v', '1', '-vf',
          'scale=960:960:force_original_aspect_ratio=decrease', '-threads', '2', poster], 30)
    if not poster.is_file():
        raise ValueError('Video preview could not be created; the source is retained.')
    return dict(info, poster=str(poster))


def _range(source, start, end, info=None):
    start, end = float(start), float(end)
    info = info or _metadata(source)
    if not all(math.isfinite(v) for v in (start, end)) or not 0 <= start < end <= info['duration_seconds']+.001:
        raise ValueError('Choose an in/out range within the source video.')
    return start, end


def _encode(source, target, start, end, frames, size=None, timeout=120):
    # A cumulative frame budget prevents many fractional ranges accumulating drift.
    filters = [f'trim=duration={end-start:.9f}', 'setpts=PTS-STARTPTS', 'fps=24']
    if size:
        width, height = size
        filters += [f'scale={width}:{height}:force_original_aspect_ratio=decrease:force_divisible_by=2',
                    f'pad={width}:{height}:(ow-iw)/2:(oh-ih)/2']
    else:
        filters += ['scale=trunc(iw/2)*2:trunc(ih/2)*2']
    filters += ['setsar=1', 'tpad=stop_mode=clone:stop_duration=1']
    _run([_tool('ffmpeg'), '-v', 'error', '-nostdin', '-n', '-ss', f'{start:.9f}', '-i', source,
          '-map', '0:v:0', '-an', '-sn', '-dn', '-vf', ','.join(filters), '-frames:v', frames,
          '-c:v', 'libx264', '-preset', 'veryfast', '-crf', '18', '-pix_fmt', 'yuv420p',
          '-threads', '2', '-filter_threads', '1', '-movflags', '+faststart', target], timeout)


def _verify(target, expected):
    result = _metadata(target)
    if result['audio_tracks'] != 0 or abs(result['duration_seconds']-expected) > .09:
        raise ValueError('The export failed its silent-picture/duration check. Source clips are retained.')
    return result


def render_range(source, target, start, end):
    source, target = Path(source), Path(target)
    if target.exists():
        raise ValueError('Choose a new export filename; existing media will not be overwritten.')
    helper = _native('export_range')
    if helper:
        result = json.loads(_run([helper, source, target, start, end], 120))
        if result.get('audio_tracks') != 0 or abs(result.get('duration_seconds', 0)-(end-start)) > .09:
            raise ValueError('The MP4 export failed its silent-picture/duration check.')
        return result
    start, end = _range(source, start, end)
    frames = round((end-start)*24)
    if frames < 1:
        raise ValueError('Choose at least one frame of footage.')
    try:
        _encode(source, target, start, end, frames)
        return _verify(target, end-start)
    except Exception:
        target.unlink(missing_ok=True)
        raise


def render_reel(segments, target, aspect):
    target = Path(target)
    if not segments or target.exists():
        raise ValueError('Choose footage and a new filename for the selects reel.')
    size = SIZES[aspect]
    expected = sum(float(s['end'])-float(s['start']) for s in segments)
    helper = _native('export_reel')
    if helper:
        with tempfile.TemporaryDirectory(prefix='sbd-reel-') as tmp:
            request = Path(tmp)/'request.json'
            request.write_text(json.dumps({'segments': segments, 'width': size[0], 'height': size[1]}), encoding='utf-8')
            result = json.loads(_run([helper, request, target], 240))
        if result.get('audio_tracks') != 0 or abs(result.get('duration_seconds', 0)-expected) > .09:
            raise ValueError('The selects reel failed its silent-picture/duration check.')
        return result
    deadline = time.monotonic()+240
    try:
        with tempfile.TemporaryDirectory(prefix='sbd-reel-') as tmp:
            tmp = Path(tmp)
            cumulative = 0; used_frames = 0; files = []; metadata = {}
            for index, segment in enumerate(segments):
                source = Path(segment['path'])
                if source not in metadata:
                    metadata[source] = _metadata(source)
                start, end = _range(source, segment['start'], segment['end'], metadata[source])
                cumulative += end-start
                frames = round(cumulative*24)-used_frames
                if frames < 1:
                    raise ValueError('Each source range must contribute at least one frame.')
                used_frames += frames
                clip = tmp/f'clip-{index:04d}.mp4'
                _encode(source, clip, start, end, frames, size, deadline-time.monotonic())
                files.append("file '"+clip.name+"'")
            manifest = tmp/'concat.txt'
            manifest.write_text('\n'.join(files)+'\n', encoding='utf-8')
            _run([_tool('ffmpeg'), '-v', 'error', '-nostdin', '-n', '-f', 'concat', '-safe', '1',
                  '-i', manifest, '-map', '0:v:0', '-an', '-c:v', 'copy', '-movflags', '+faststart', target], deadline-time.monotonic())
        return _verify(target, expected)
    except Exception:
        target.unlink(missing_ok=True)
        raise
