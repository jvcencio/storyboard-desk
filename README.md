# Storyboard Desk

A GoSX creative workspace for turning an idea into shots, takes and editable footage. Plan the story, keep reference images and alternative clips, generate selected takes, then export extra footage to shape in an editor.

GoSX and Go own the application, including browser interaction compiled from Go to WebAssembly. Windows opens a native WebView2 window; Mac and Linux open the GoSX browser workspace. Existing projects retain their format, media and approval/history records. Python and application JavaScript have been retired. Building and testing use Go; Node and npm are unnecessary.

## Start Storyboard Desk

For a source checkout, install Go 1.26+ and build:

```sh
go -C gosx-host run ./cmd/package
```

Double-click **Start Storyboard Desk.bat** on Windows or **Start Storyboard Desk.command** on Mac. Linux can run `./build/gosx/storyboard-desk-go` or `sh 'Start Storyboard Desk.command'`. The launchers prefer the packaged executable, then a module-local binary, then `go run`. `STORYBOARD_GO_EXECUTABLE` selects an explicit built workspace. The older **Start Storyboard Desk Go** names are aliases.

Windows needs the installed Evergreen WebView2 Runtime and the packaged **WebView2Loader.dll** beside the executable. `-browser` selects browser mode on Windows. Mac native hosting remains available with `-native` or the built app bundle, but still needs real Mac verification; its standard source launcher uses the browser.

To open existing work explicitly:

```sh
./build/gosx/storyboard-desk-go -data-dir /absolute/path/to/projects
# A new custom directory also needs -init-data-dir.
# -no-open prints the local URL without opening a window or browser.
```

Stop the existing app and back up the complete data directory before switching implementations. Local settings/environment overrides still work. The default port is 4492; `-port 0` selects a random loopback port. See the [workspace guide](docs/GOSX_PORT.md) and [Windows/default-launch verification](docs/gosx-windows-verification.json).

For inspection and silent video export, install FFmpeg/ffprobe with libx264 support and put them on PATH, or configure their paths. [FFmpeg's download page](https://ffmpeg.org/download.html) links platform builds. Existing Swift helpers remain an optional Mac route.

## Background startup

For an on-demand launcher such as Mini Meter, run `./build/gosx/storyboard-desk-go -background -no-open`. It returns JSON with `app`, `url`, `status` and, for a new server, `pid`. Repeated launches reuse only a Go workspace with the same data directory and port. Logs live in `runtime/server.log` inside that directory. Existing integrations that called `launch.py --background --no-open` should call the Go executable with `-background -no-open` instead. Foreground startup accepts `-ask-openrouter-key`; background sessions can inherit the server's environment key.

## First useful session

The Go workspace starts empty. Create a project, edit its direction, import an image or clip, and compare takes. For a blank three-shot episode, import [the reusable project template](templates/storyboard-project-template.json).

For a music video:

1. Create a project, enable **Music video**, and attach the original PCM WAV in **Music & lyrics**.
2. Import footage in the shot editor. **Media Library** lets you review it without choosing a final take.
3. In **Song sections**, create a verse/chorus or split a time range into working portions.
4. Collect alternative clips in a section's **Coverage bin**. Choose preferred source ranges and their order.
5. Choose **Preferred source ranges**, then **Export selects reel MP4**. Import that silent overlength reel into iMovie or another editor; split, trim and rearrange it against the original song.

The footage reel may be longer than its song section. That extra material is deliberate: SBD supplies options; you decide what survives the edit. It does not automatically synchronize, stretch or finish the music video.

See [the first-session walkthrough](docs/GETTING_STARTED.md) for image/video generation and common questions.

## Optional Google image and Veo generation

The **Generate** workspace calls supported Google image models and silent Veo video through **OpenRouter**. Your own OpenRouter API credits and account access are required; a Google/ChatGPT/Claude consumer subscription does not configure this route.

Stop an existing server before starting a credentialed session:

```sh
./build/gosx/storyboard-desk-go -ask-openrouter-key
```

The prompt hides your input and keeps it in server memory. It does not save the key to settings/projects or pass it to media/browser children. Existing `OPENROUTER_API_KEY` environment setups also work. On Mac, `STORYBOARD_OPENROUTER_KEYCHAIN_SERVICE` can select an existing Keychain item.

In the app, check the connection, review/approve a production brief, and set a project allowance. Inspect the exact prompt, reference images and reservation before confirming each request. Review and approve a finished starter image before creating video. New media returns unselected. Startup, exports and copying prompts do not create paid requests. There is no automatic retry.

The allowance includes confirmed charges and reservations. A failed request with unknown cost keeps its reservation. The image reservation is a conservative estimate, not a provider-enforced maximum charge. Price and availability are checked against the provider catalog on connection.

You can also copy prompts and manually use Flow, Grok, Claude or another tool, then import the resulting files. A handoff does not log into those services or send automatically.

## Storage and configuration

Projects live outside the checkout: `%LOCALAPPDATA%\Storyboard Desk` on Windows, `~/Library/Application Support/Storyboard Desk` on Mac, and `~/.local/share/storyboard-desk` (or XDG data home) on Linux. Each project retains its JSON, media, hashes, prompts and exports. Imports copy files; original source files stay in place. Keep your own backup of that data folder.

To use another folder, copy `settings.example.json` to **settings.local.json** and edit it. That local file is ignored by Git. Environment variables override local settings:

| Setting | Environment variable |
| --- | --- |
| Project folder | `STORYBOARD_DATA_DIR` |
| Local port (default 4492) | `STORYBOARD_PORT` |
| Optional Draw Things CLI | `STORYBOARD_DRAW_CLI` |
| Optional model folder / filename | `STORYBOARD_MODELS_DIR` / `STORYBOARD_DRAW_MODEL` |
| Optional Mini Meter executable | `STORYBOARD_METER` |
| Optional cached reference previews | `STORYBOARD_REFERENCE_CACHE` |
| FFmpeg / ffprobe executables | `STORYBOARD_FFMPEG` / `STORYBOARD_FFPROBE` |
| Media backend (`auto`, `ffmpeg`, `native`) | `STORYBOARD_MEDIA_BACKEND` |

An unavailable configured `/Volumes/...` drive is never replaced with a newly created empty drive folder. The server binds to loopback and requires same-origin checks plus a per-run token for mutations. It is a local workspace, not an Internet-facing multi-user server.

**Draw locally** is an optional advanced layout-sketch route. It is a Mac integration that requires Draw Things CLI, the configured local model and Mini Meter memory checks. It is not required for cloud generation or the imported-footage workflow. A local rough sketch is not a qualified character-identity render.

## Development

The workspace uses Go services, GoSX Node components, CSS and a Go-WASM editor engine. GoSX supplies the browser loader; application behavior lives in `gosx-host/client/*.go`. The packager builds and embeds the runtime automatically. Swift helpers are optional media adapters. Captured synthetic legacy projects and expected outputs keep compatibility checks independent of the retired runtime.

```sh
go -C gosx-host run ./cmd/client
go -C gosx-host test -race ./...
go -C gosx-host vet ./...
go -C gosx-host run ./cmd/package -target linux-amd64
# Optional browser checks: installed headless Chrome, driven entirely from Go.
go -C gosx-host test -tags browser -run '^TestWorkspaceGoClient' .
\n+# Optional macOS AVFoundation helpers (Apple Command Line Tools required):
go -C gosx-host run ./cmd/native

# Use a separate data folder and port for development:
go -C gosx-host run . -browser -data-dir /path/to/development-projects -init-data-dir -port 4493
```

Tests use temporary stores and fake cloud clients; they do not spend model credits. GitHub CI runs Go tests on Windows, Linux and Mac, exercises actual FFmpeg exports, and builds the GoSX packages and optional Mac helpers. See the [retirement verification](docs/gosx-retirement-verification.json) for executed checks and remaining target-specific limits.

The [physical Apple Silicon Mac verification](docs/gosx-mac-verification.json) records native builds, media helpers, exports and a real file-picker import through Peekaboo. The updated native Go-WASM window and macOS Save cancellation, reopening and saving pass; the saved ZIP has valid CRCs and preserves the project and source media. Custom filenames and folders containing spaces and Unicode also pass. Foreground checks ran only during idle periods. The [Go editor verification](docs/gosx-go-client-verification.json) records browser behavior and generated runtime checks.

This repository contains application source and a generic demo. Personal projects, lyrics, reference/media libraries, cloud-job receipts, handoff packets, logs, local configuration and built executables are excluded by an explicit source allowlist. No license for public redistribution has been selected.
