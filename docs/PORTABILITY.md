# Windows, Mac, Linux and GoSX

GoSX/Go is the default, with a native Windows WebView2 window and a Mac/Linux browser workspace. The built app needs no Python or Node. Both updated runtimes retain the existing project format and enforce exclusive storage ownership. Python remains an explicit compatibility fallback through `--legacy` or **Start Storyboard Desk Legacy**.

Build with `go -C gosx-host run ./cmd/package`; output goes to `build/gosx`. Cross-targets include `windows-amd64`, `windows-arm64`, `darwin-amd64`, `darwin-arm64` and `linux-amd64`. Run the executable or **Start Storyboard Desk** directly. `-browser` selects browser mode on Windows. Mac native hosting requires `-native` or the packaged app bundle and remains pending real Mac execution. See the [workspace guide](GOSX_PORT.md) and [Windows/default-launch evidence](gosx-windows-verification.json).

| Capability | Windows | Mac | Linux |
| --- | --- | --- | --- |
| Browser workspace, media imports, planning and cloud generation | Yes | Yes | Yes |
| Silent ranges and selects reels | FFmpeg | FFmpeg or native Swift helpers | FFmpeg |
| GoSX native window | WebView2 | WKWebView | Use the browser; upstream native host is unsupported |
| Draw Things + Mini Meter sketch route | Import/cloud alternatives | Optional existing integration | Import/cloud alternatives |

## Established Python launch on Windows

Install Python 3.11 or newer, then clone/download the repository. Double-click **Start Storyboard Desk Legacy.bat**, or run in PowerShell from the app folder:

```powershell
py -3 launch.py --legacy
```

No API key or Go compiler is needed to try the demo. Full video processing needs **both `ffmpeg` and `ffprobe`** on PATH, with H.264/libx264 support (FFmpeg 6.1 or newer). Use a build linked from [FFmpeg's download page](https://ffmpeg.org/download.html). Open a new Terminal after changing PATH. Alternatively set `STORYBOARD_FFMPEG` and `STORYBOARD_FFPROBE` to the executable paths, including `.exe`. Paths with spaces work; no shell command is constructed from filenames.

Use `py -3 launch.py --legacy --ask-openrouter-key` for an optional credentialed session. Stop a previous instance first. API keys stay in the server environment; ordinary planning, imports and export do not make model requests.

## Legacy GoSX host for Python

The host pins **m31labs.dev/gosx v0.57.24**. It loads only the explicit `http://127.0.0.1:PORT/` SBD address and checks backend identity before opening. Native shell/filesystem IPC is disabled. Closing a window stops a backend started by that foreground launcher; a pre-existing backend remains running.

Install Go 1.26 or newer, then:

```sh
python build_desktop.py
python launch.py --legacy --gosx
# Optionally:
python launch.py --legacy --gosx --ask-openrouter-key
```

The Mac build also creates `gosx-host/Storyboard Desk.app`; the launcher uses that bundle.

On a Mac use `python3` if `python` is unavailable. The Go compiler may download its pinned module dependencies. No TinyGo, npm, CGo or frontend build step is needed for this desktop-host target.

On Windows the build also downloads a checksum-pinned Microsoft WebView2 SDK, extracts the matching **WebView2Loader.dll**, and saves its license next to the executable. Keep that DLL beside `storyboard-desk-gosx.exe`. The **Evergreen WebView2 Runtime** is separate; install/update it from [Microsoft](https://developer.microsoft.com/microsoft-edge/webview2/) if GoSX reports that the runtime is unavailable. Native host initialization depends on a desktop session. If it cannot open, `python launch.py --legacy` still opens the full app in a browser.

`STORYBOARD_GOSX_HOST` can point to a separately built host executable. To embed a running SBD service directly, run `storyboard-desk-gosx --url http://127.0.0.1:4492/`. The host's `--check` flag validates backend identity without opening a window. GoSX native file dialogs and download behavior follow the platform webview; browser mode is the reference route for external-editor handoffs.

## Storage and portability

Defaults are `%LOCALAPPDATA%\Storyboard Desk` on Windows, `~/Library/Application Support/Storyboard Desk` on Mac, and `$XDG_DATA_HOME/storyboard-desk` (or `~/.local/share/storyboard-desk`) on Linux. Use `settings.local.json` or `STORYBOARD_DATA_DIR` to override. This file is ignored by Git. JSON, lyrics, manifests and prompts are read/written as UTF-8; saved media links use forward slashes.

To move a workspace, stop its server and back up/copy the whole data directory, including root-level `cloud/` job reservations and each project's `project.json`, `media/`, `jobs/` and exports. Moving data does not transfer credentials or authorize paid resubmission. Restoring data does not undo provider charges. Do not run two servers against one data folder. Updated Go/Python runtimes use the same OS ownership lock; crashes release it. Stop older builds before upgrading because they do not honor this root lock.

`STORYBOARD_MEDIA_BACKEND=ffmpeg` selects the portable exporter explicitly. `auto` keeps existing native helpers on Mac when installed and uses FFmpeg otherwise. `native` requires built Mac helpers. FFmpeg output is 24fps H.264/yuv420p MP4, letterboxed to the project aspect for reels, with audio removed. Fractional ranges share a cumulative frame budget so long reels stay within one frame of their requested total. Source files remain unchanged. Retain the original song for your editor's audio track.

## Verification and boundaries

CI runs backend tests and real FFmpeg inspection/range/reel tests on Windows, Linux and Mac, plus GoSX host tests and native Mac/Windows builds. Windows ARM64 is cross-built. These checks verify processing and compilation; they do not certify an interactive WebView2 session, iMovie/Final Cut imports or native download dialogs. The test media includes audio, mixed aspect ratios, fractional timings and filenames with Unicode, spaces and apostrophes.

The current [Windows/default-launch report](gosx-windows-verification.json) records actual Windows 11 x64/WebView2 and NTFS execution, FFmpeg processing, persistence/lock/crash checks and downloaded reel/editor packages, plus Linux/default-launch regressions. Privileged Windows symlink tests and interactive picker/save dialogs remain unverified. Mac native execution, Windows ARM64 execution, Swift/Draw Things, controlled live-provider behavior and real editor imports remain open. Standard launchers now select GoSX, while Python retirement remains pending.
