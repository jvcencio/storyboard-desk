# Storyboard Desk

A GoSX creative workspace for turning an idea into shots, takes and editable footage. Plan the story, keep reference images and alternative clips, generate selected takes, then export extra footage to shape in an editor.

The current app is a local Python server with a buildless browser UI. Full video inspection and MP4 export use small macOS helpers. It is a working development version.

## Start on a Mac

The full workflow targets macOS 13 or newer, Python 3.9 or newer and a modern browser. No pip packages, Node build or API key are needed to open the app and organize imported media.

```sh
git clone https://github.com/jvcencio/storyboard-desk.git
cd storyboard-desk
python3 launch.py
```

The launcher opens **http://127.0.0.1:4492/**. Keep its Terminal window open; **Control-C** stops it. Later, you can also double-click **Start Storyboard Desk.command**.

To enable video inspection, silent MP4 ranges and selects-reel export, build the native helpers once:

```sh
# If Apple's Command Line Tools are not installed:
xcode-select --install

# After that installation has finished:
python3 build_native.py
```

Build on the Mac that will run the app; no architecture-specific binaries are committed. This release is supported on macOS.

## First useful session

The first launch creates a small three-shot demo. Open a shot, edit its direction, import an image or a clip, and compare takes. You can start your own project at any time. For a blank three-shot episode, import [the reusable project template](templates/storyboard-project-template.json).

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
python3 launch.py --ask-openrouter-key
```

The prompt hides your input and passes it only through the server's process environment. It does not save the key to the repository or project files. Existing `OPENROUTER_API_KEY` environment setups also work. The creator's optional workstation Keychain adapter remains supported when present; that adapter and its credentials are not included here.

In the app, check the connection, review/approve a production brief, and set a project allowance. Inspect the exact prompt, reference images and reservation before confirming each request. Review and approve a finished starter image before creating video. New media returns unselected. Startup, exports and copying prompts do not create paid requests. There is no automatic retry.

The allowance includes confirmed charges and reservations. A failed request with unknown cost keeps its reservation. The image reservation is a conservative estimate, not a provider-enforced maximum charge. Price and availability are checked against the provider catalog on connection.

You can also copy prompts and manually use Flow, Grok, Claude or another tool, then import the resulting files. A handoff does not log into those services or send automatically.

## Storage and configuration

On a fresh Mac, projects live in `~/Library/Application Support/Storyboard Desk`, outside the checkout. Each project retains its JSON, media, hashes, prompts and exports. Imports copy files; original source files stay in place. Keep your own backup of that data folder.

To use another folder, copy `settings.example.json` to **settings.local.json** and edit it. That local file is ignored by Git. Environment variables override local settings:

| Setting | Environment variable |
| --- | --- |
| Project folder | `STORYBOARD_DATA_DIR` |
| Local port (default 4492) | `STORYBOARD_PORT` |
| Optional Draw Things CLI | `STORYBOARD_DRAW_CLI` |
| Optional model folder / filename | `STORYBOARD_MODELS_DIR` / `STORYBOARD_DRAW_MODEL` |
| Optional Mini Meter executable | `STORYBOARD_METER` |
| Optional cached reference previews | `STORYBOARD_REFERENCE_CACHE` |

An unavailable configured `/Volumes/...` drive is never replaced with a newly created empty drive folder. The server binds to loopback and requires same-origin checks plus a per-run token for mutations. It is a local workspace, not an Internet-facing multi-user server.

**Draw locally** is an optional advanced layout-sketch route. It requires Draw Things CLI, the configured local model and Mini Meter memory checks. It is not required for cloud generation or the imported-footage workflow. A local rough sketch is not a qualified character-identity render.

## Development

Python standard library backend; plain HTML/CSS/JavaScript frontend. Swift sources implement local media inspection and silent exports.

```sh
python3 -m unittest discover -s . -p 'test_*.py' -v
# Optional JavaScript syntax check if Node is installed:
for file in static/*.js; do node --check "$file"; done

# Use a separate data folder and port for development:
python3 server.py --data-dir /tmp/storyboard-desk-dev --port 4493
```

Tests use temporary stores and fake cloud clients; they do not spend model credits. GitHub CI runs the tests and builds native helpers on macOS.

This repository contains application source and a generic demo. Personal projects, lyrics, reference/media libraries, cloud-job receipts, handoff packets, logs, local configuration and built executables are excluded by an explicit source allowlist. No license for public redistribution has been selected.
