# Storyboard Desk GoSX workspace

GoSX/Go is the default workspace: planning, media, generation, music sections and handoffs run without Python or the legacy UI. Windows uses a native window by default; Mac/Linux use the browser, with Mac native hosting available through `-native`. See the [migration specification](GOSX_MIGRATION_SPEC.md) and [Windows/default-launch verification](gosx-windows-verification.json). The [earlier full-port report](gosx-port-verification.json) identifies the source before this launcher cutover.

## Start the Go workspace

Install Go 1.26 or newer to build from source. Stop any Storyboard Desk process using the chosen data directory first. Back up the complete directory before switching runtimes with existing work.

```sh
cd gosx-host
go run ./cmd/package
../build/gosx/storyboard-desk-go -data-dir /absolute/path/to/existing-projects
# Windows: use ../build/gosx/storyboard-desk-go.exe and its adjacent loader DLL.
# For a new, explicitly chosen directory:
../build/gosx/storyboard-desk-go -data-dir /absolute/path/to/new-projects -init-data-dir
# Source-only browser development, including Windows without a packaged loader:
go run . -browser -data-dir /absolute/path/to/existing-projects
```

The standard **Start Storyboard Desk.command** and **Start Storyboard Desk.bat** launch GoSX. They prefer `build/gosx/storyboard-desk-go[.exe]`, then a module-local binary, then Go from source. `STORYBOARD_GO_EXECUTABLE` selects an explicit binary; a bad override fails instead of switching backends. **Start Storyboard Desk Go** remains an alias. Python launch/build scripts, the legacy UI and its service bridge have been removed.

No Python, Node, npm or frontend compiler is required by a built Go app. `-browser` overrides Windows native hosting. `-no-open` prints the address without opening either a window or browser (an explicitly supplied `-native` still selects native hosting). `-port 0` chooses a random loopback port. `-editor` opens the same workspace on a random port without a browser. Existing `-workspace` invocations remain supported. `-check -url http://127.0.0.1:PORT/` verifies an existing Go workspace. The retired Python service bridge is no longer a host mode.

The workspace reads existing `settings.local.json` and documented `STORYBOARD_*` environment variables. Use `-settings /path/to/settings.local.json` for an explicit file. Precedence is command flags, environment, local settings, then OS defaults. A fresh OS-default data directory can be initialized automatically; a custom missing directory requires `-init-data-dir`. An unavailable `/Volumes` mount is never recreated. A new Go workspace starts empty; use **New project** or import the reusable template.

For credentialed generation, add `-ask-openrouter-key` in a terminal, or supply `OPENROUTER_API_KEY` in the server environment. Hidden entry stays in server memory and is not written to settings/projects or passed to media tools/browser children. On Mac, an explicitly configured `STORYBOARD_OPENROUTER_KEYCHAIN_SERVICE` can read an existing Keychain item. Planning, imports, prompts and exports work without a key.

## Implemented slices

| Slice | Go behavior |
| --- | --- |
| Storage | Exclusive process ownership, revision checks, validated paths, atomic writes, opaque JSON/number preservation, staged creation and crash recovery. |
| Planning | Project navigation, episode/scene timing, every shot field, insertion/reordering, appearance, URL notes, copied image references, templates and reviewed plans. Rejected edits retain drafts; navigation checks for unsaved edits. |
| Media | Copied PNG/JPEG/WebP and MP4/MOV/WebM imports, hashes, optional probes/posters, all takes, selection, starter approval/staleness, bin/restore, source ranges, editorial notes, independent placements and undo. HTTP range requests support playback. |
| Review | Selected-sequence and section-assembly previews with muted clips and original-song audio. Playback stops at missing coverage without stretching sources. |
| Generation | Manual prompt destinations, direction packets/import, catalog connection, approved briefs, allowances, exact quotes/references, explicit confirmation, durable reservations/jobs, polling and saved-result recovery. Unknown charges retain reservations. Results arrive unselected, tied to the original starter. |
| Local drawing | Optional Draw Things adapter with offline arguments, input/settings snapshots, one worker, memory-pressure preflight, cancellation, receipts and idempotent recovery. Requires the existing Mac tools/model. |
| Music | Original PCM WAV/history, exact lyrics, derived timing, cues, sections, fixed portions, coverage bins/reservations, independent assembly edits and undo. |
| Exports | Review ZIP with selected sources and own generation receipts; manual image/direction packet; silent reels with JSON/CSV maps; editor ZIP with ranges, full source handles, coverage options, original/excerpt WAV, cues and Final Cut XML. |
| Packaging | Go-only builder, Windows loader/license packaging, optional Mac bundle, Linux executable, native/browser startup and compatible local settings. |

FFmpeg/ffprobe with libx264 are needed for portable video inspection, silent ranges and reels. Existing Swift helpers remain optional on Mac (`media_backend: native` or `auto`). Planning and copied imports remain available if probing is unavailable. PCM WAV inspection runs in Go. Missing tools and invalid sources return errors while retaining original media.

To generate, connect deliberately, review/approve a production brief, and set an allowance. Prepare a request, inspect its exact prompt, inputs and reservation, then confirm it. Recheck a saved video job to advance polling. An uncertain request is never automatically submitted again. Local drawing status has a refresh control. The allowance is accounting protection, not a provider-enforced maximum charge.

For manual review, download the image/brief ZIP from **Plans & handoffs**, attach its actual images in the chosen review app, inspect the returned JSON and import it. Matching legacy cached Archive previews can be included from the configured reference cache; URL notes are never automatically fetched. A direction proposal never approves a starter by itself.

## Storage compatibility and rollback

Go and the last migration-era Python store hold the same `.storyboard-owner.lock` for their lifetime. A second writer fails before opening. A crash releases the OS lock; do not delete its file while a process runs. Go also holds an existing legacy `cloud/worker.lock`. Stop older builds before upgrading: they do not implement the root ownership protocol.

Go patches existing `project.json` files. IDs, paths, selections, approval/history records, source bytes and unknown JSON survive normal edits. Approval, direction and music timing use current inputs and Python-compatible fingerprints. Always-on Go tests read captured Python-created project/oracle fixtures and reopen complete Go projects, media, cues, sections and fake job receipts. The original bidirectional Python-loader checks are retained as historical verification evidence.

Writes flush a private temporary file, replace the document and sync the directory on Unix. Windows uses replacement/write-through after flushing. Incomplete temporary files/staging directories are ignored after a crash. Unsafe paths, replaced roots and invalid documents are rejected. Device/power-loss behavior depends on the filesystem; retain backups.

Python is available in repository history rather than as an installed fallback. Back up the **whole data directory**, including root-level `cloud/` reservations/jobs. For rollback to an older app version, stop every writer and restore a complete backup to a separate directory. Restoring a backup does not undo provider charges: reconcile account activity before new generation.

## Build and verify

```sh
cd gosx-host
go run ./cmd/package -target linux-amd64
# Other targets: windows-amd64, windows-arm64, darwin-amd64, darwin-arm64.
# Output is ../build/gosx; run the executable directly.
go test -race ./...
go vet ./...
```

The Windows packager verifies the pinned Microsoft WebView2 SDK checksum and places its matching loader/license beside the executable and standard batch launcher. The Evergreen runtime is separate. Mac packaging creates **Storyboard Desk.app**, whose bundle launcher explicitly selects native hosting. Apple Silicon execution has been checked; signing/notarization remains separate. Builds retain Git revision stamping.

Compatibility tests are Go-only. The frozen synthetic fixture in `gosx-host/testdata/legacy` records its original source hashes, file hashes and expected approval, direction, prompt and timing outputs. Never regenerate its oracle with Go. The suite exercises opaque fields/large integers, revision/CSRF rejection, source preservation, editing/undo, reservations, exports, fake provider/local jobs, and actual built-app launch with an empty PATH.

Rejected forms start unsaved after the page reload, retaining the submitted revision and navigation guard. The Go browser regression covers stale and invalid shot saves followed immediately by navigation and Cancel, with no extra input event and no change to project bytes; see `rejected_draft_regression` in the [Go client report](gosx-go-client-verification.json).

Shot editing uses a typed `ShotEditCommand`, a GoSX server action and the `ShotEditor` component with explicit form values and action state. The `/save` endpoint retains HTML redirects and partial-field clients; `Accept: application/json` returns GoSX action results with field errors or a revision receipt. Both paths keep the existing loopback, origin, token and store revision guards. Rejected drafts retain their original target and revision, and invalid fields have accessible [inline errors](gosx-shot-validation.png).

Project settings, insertion and reordering share that adapter through `ProjectEditCommand`, `InsertShotCommand` and `MoveShotCommand`. `ProjectEditor` receives explicit values and action state. A rejected unchecked Music video field stays off; each shot card retains its own movement target. Legacy form redirects, atomic store writes and stale-revision rejection remain in force.

`board.gsx` and checked-in `board_generated.go` remain the typed shot-card source/output. The workspace uses GoSX Node/component APIs in `workspace*.go`, embedded CSS and a Go-WASM engine in `client/*.go` for forms, file previews, clipboard and playback. `go run ./cmd/client` uses the pinned GoSX compiler's Go-WASM build and copies its supplied browser loaders. The packager and source launchers run this step automatically. Generated assets are ignored by Git and embedded in the executable; Python, Node and npm are unnecessary for building, testing or running the app. CI also checks generated shot-card parity and runs Go-driven headless browser tests on Linux. On Mac, `go -C gosx-host run ./cmd/native` builds the optional Swift helpers; native tests run on Mac CI and matching-architecture helpers are copied into the app bundle.

The engine now coordinates separate controllers for dirty forms, file/clipboard/native-save capabilities, cloud selectors and each review player. Controllers own their listeners and cancellation contexts; disposing playback pauses media and cancels its frame loop. This keeps ordinary form state independent of media playback while preserving the existing Go-only build.

Native exports call GoSX's Save dialog API with the correct extension and overwrite confirmation. Cancellation preserves the editor and its unsaved forms. Saving streams to a private temporary file before atomic replacement; interrupted copies preserve existing destination files. Destinations within project storage and destination symlinks are rejected. Browser mode keeps normal attachment downloads.

For on-demand background launch, run the built executable with `-background -no-open` and a fixed port. Startup returns JSON. It reuses only the matching Go backend and data directory, serializes concurrent starts, and keeps `runtime/server.log` in that directory. Update Mini Meter or other integrations that previously called `launch.py` to invoke this command.


## Remaining release gates

Linux browser workflows, real FFmpeg processing, fake-provider/local-render recovery, race tests and bidirectional compatibility have been exercised. Real Windows 11 x64/WebView2 and NTFS checks now cover startup, Unicode project editing/reopen, process locks/crash recovery, directory-junction guards, muted playback and silent-reel/editor-package downloads. The standard Windows launcher is exercised against the actual native window. A physical M1 Max Mac passes Go race tests, native media-helper execution, ARM64 packaging, installed-app exports and actual native Go-WASM window/Save checks. See the [Mac report](gosx-mac-verification.json). Windows ARM64 execution remains separate from cross-compilation.

Windows UI checks use WebView2's temporary debugging connection because the desktop computer-use connector cannot attach to this WSL checkout. File inputs use Playwright payloads, and downloads use an isolated test directory; native picker/save-dialog interaction is not certified. No debugging flags are added to normal launchers.

Peekaboo's existing authorized Bridge supports Mac screenshots and native interaction over SSH. Live ScreenCaptureKit captures and fresh Accessibility state verified the keyframe picker: cancel, reopen, choose the synthetic PNG and copy its exact bytes on source `2a08440`. The updated Go-WASM build (`cc21af1`) renders in WKWebView, opens the real Save panel, handles cancellation, reopens and saves a valid review ZIP. The Go client shows the actual cancellation and saved-filename results. The external copy matches its generated export; revision 16, opaque integers and all three original media hashes remain unchanged. Checks ran during idle periods and stopped when user activity resumed. Subsequent [custom Save checks](gosx-mac-custom-folder.png) pass with spaces and Unicode in both the filename and selected folder, using native folder navigation and the Save button. A native dirty draft remains unverified. See the [native window](gosx-mac-native-go.png), [Save panel](gosx-mac-save-dialog.png) and separate [headless browser evidence](gosx-go-client-verification.json).

Interactive Windows file dialogs, actual Draw Things, controlled live-provider checks and Final Cut/iMovie imports also remain open. Python has been retired at the requested cutover. These platform/integration limits remain visible release verification work; retirement does not certify unexecuted integrations. See the [retirement report](gosx-retirement-verification.json) for its source-scoped checks; earlier reports identify their historical source snapshots.
