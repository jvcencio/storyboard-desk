# Platform setup and verification

Storyboard Desk runs in GoSX and Go. Windows defaults to a native WebView2 window; Mac and Linux default to a browser. Python source, launchers and build tooling have been retired. Existing project files require no conversion.

| Capability | Windows | macOS | Linux |
| --- | --- | --- | --- |
| Planning, copied imports, music, review packages | Go workspace | Go workspace | Go workspace |
| Default window | WebView2 | Browser | Browser |
| Optional native window | Default | `-native` / app bundle | Browser supported |
| Video probes, silent ranges and reels | FFmpeg/ffprobe | FFmpeg/ffprobe or Swift helpers | FFmpeg/ffprobe |
| Local Draw Things / Mini Meter | Import/cloud alternatives | Optional configured integration | Import/cloud alternatives |

## Build and launch

Install Go 1.26+ for a source build, then run:

```sh
go -C gosx-host run ./cmd/package
```

Use **Start Storyboard Desk.bat** on Windows, **Start Storyboard Desk.command** on Mac, or the built executable on Linux. The built app needs no Go compiler, Python, Node, npm or CGo. Add `-browser` to select the browser on Windows. Configuration and project locations are described in the [README](../README.md).

The Windows package includes a checksum-pinned **WebView2Loader.dll** and its license. Keep both beside **storyboard-desk-go.exe**. Install the [Evergreen WebView2 Runtime](https://developer.microsoft.com/microsoft-edge/webview2/) separately if the native host reports it missing. A desktop session is required for a native window. Browser and `-no-open` modes remain available.

The Mac package includes **Storyboard Desk.app**, whose launcher selects `-native`. Build optional AVFoundation media helpers on the target Mac first:

```sh
go -C gosx-host run ./cmd/native
go -C gosx-host run ./cmd/package
```

Apple Command Line Tools are required for those Swift helpers. Matching host-architecture helpers are included in the app bundle when present. Cross-compilation alone does not certify target execution. Signing/notarization remain separate distribution work.

Install FFmpeg and ffprobe with libx264 support on PATH, or configure `STORYBOARD_FFMPEG` and `STORYBOARD_FFPROBE`. Planning, copied imports and Go PCM WAV handling do not require these tools. Video export reports unavailable or invalid tools without changing source media.

## Verification scope

[Retirement verification](gosx-retirement-verification.json) records current Go-only checks and source identity. [Windows verification](gosx-windows-verification.json) and the earlier [full-port report](gosx-port-verification.json) retain the source snapshots used for their Windows 11 x64/WebView2, NTFS, FFmpeg, browser, compatibility and crash-recovery checks.

CI runs Go tests on Windows, Linux and Intel Mac, real FFmpeg processing, Mac helper compilation/execution, Linux race checks, Go-driven headless browser checks, and package builds. The editor engine is compiled from Go to WebAssembly with the pinned GoSX compiler; building and testing require no Node/npm toolchain. Captured synthetic legacy fixtures check JSON preservation and expected hashes/prompts/timing. No live provider or actual Draw Things model is called by tests.

The [physical Apple Silicon Mac report](gosx-mac-verification.json) records real native media helpers, ARM64 packaging, an installed native app, screenshots and validated exports using synthetic project data. Peekaboo's authorized Bridge verified native file-picker cancellation, reopening and PNG import with matching bytes on source `2a08440`. The updated Go-WASM build (`cc21af1`) passes actual WKWebView rendering and native Save cancellation, reopening and saving. The saved ZIP matches its export, passes CRC checks and preserves original project/media bytes. Foreground interaction ran only during idle periods and stopped when user activity resumed.

Native file/save dialog verification is recorded separately from programmatic file-input/download checks. The Mac Save check used the displayed default filename and synthetic input directory; custom name/folder selection and native dirty-draft behavior remain unverified. Windows native dialogs, Windows ARM64 execution, privileged Windows symlink cases, actual Draw Things, controlled live-provider behavior and real iMovie/Final Cut imports require their own execution evidence. Keep these limits separate from successful compilation or synthetic-provider tests.
