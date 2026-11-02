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

The standard **Start Storyboard Desk.command** and **Start Storyboard Desk.bat** launch GoSX. They prefer `build/gosx/storyboard-desk-go[.exe]`, then a module-local binary, then Go from source. `STORYBOARD_GO_EXECUTABLE` selects an explicit binary; a bad override fails instead of switching backends. **Start Storyboard Desk Go** remains an alias. `python launch.py` also delegates to Go; `--legacy` or **Start Storyboard Desk Legacy** explicitly selects Python.

No Python, Node, npm or frontend compiler is required by a built Go app. `-browser` overrides Windows native hosting. `-no-open` prints the address without opening either a window or browser (an explicitly supplied `-native` still selects native hosting). `-port 0` chooses a random loopback port. `-editor` opens the same workspace on a random port without a browser. Existing `-workspace` invocations remain supported. Explicit `-url`, `-check`, `-board-preview` and `-editor` modes retain their prior contracts.

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

Updated Python and Go stores hold the same `.storyboard-owner.lock` for their lifetime. A second writer fails before opening. A crash releases the OS lock; do not delete its file while a process runs. Go also holds an existing legacy `cloud/worker.lock`. Stop older builds before upgrading: they do not implement the root ownership protocol.

Go patches existing `project.json` files. IDs, paths, selections, approval/history records, source bytes and unknown JSON survive normal edits. Approval, direction and music timing use current inputs and Python-compatible fingerprints. Tests reopen complete Go projects, media, cues, sections and fake job receipts through Python loaders.

Writes flush a private temporary file, replace the document and sync the directory on Unix. Windows uses replacement/write-through after flushing. Incomplete temporary files/staging directories are ignored after a crash. Unsafe paths, replaced roots and invalid documents are rejected. Device/power-loss behavior depends on the filesystem; retain backups.

To switch back, stop Go and run `python3 launch.py --legacy` against the same configured directory, or use **Start Storyboard Desk Legacy**. No conversion is required. Back up the **whole data directory**, including root-level `cloud/` reservations/jobs. Restoring an old backup does not undo provider charges: reconcile account activity before new generation. For rollback to an older app version, restore a complete backup to a separate directory after stopping every writer.

## Build and verify

```sh
cd gosx-host
go run ./cmd/package -target linux-amd64
# Other targets: windows-amd64, windows-arm64, darwin-amd64, darwin-arm64.
# Output is ../build/gosx; run the executable directly.
go test -race ./...
go vet ./...
```

The Windows packager verifies the pinned Microsoft WebView2 SDK checksum and places its matching loader/license beside the executable and standard batch launcher. The Evergreen runtime is separate. Mac packaging creates **Storyboard Desk.app**, whose bundle launcher explicitly selects native hosting; signing/notarization and real Mac execution remain pending. Builds retain Git revision stamping.

From the repository root, run `python3 -m unittest discover -s . -p 'test_*.py' -v` for compatibility/regression checks. Python is a migration test oracle, not a built-app dependency. `board.gsx` and checked-in `board_generated.go` remain the typed shot-card source/output; the complete workspace uses GoSX Node/component APIs in `workspace*.go`, embedded CSS and small browser playback/form affordances. It does not load `static/app.js`.

For a static read-only board, `go run . -board-preview -data-dir /path/to/projects` briefly acquires ownership and prints HTML containing project data. The legacy `-url` bridge remains available for an already-running Python service.

## Remaining release gates

Linux browser workflows, real FFmpeg processing, fake-provider/local-render recovery, race tests and bidirectional compatibility have been exercised. Real Windows 11 x64/WebView2 and NTFS checks now cover startup, Unicode project editing/reopen, process locks/crash recovery, directory-junction guards, muted playback and silent-reel/editor-package downloads. The standard Windows launcher is exercised against the actual native window. Mac and Windows ARM64 execution remain separate from cross-compilation.

Windows UI checks use WebView2's temporary debugging connection because the desktop computer-use connector cannot attach to this WSL checkout. File inputs use Playwright payloads, and downloads use an isolated test directory; native picker/save-dialog interaction is not certified. No debugging flags are added to normal launchers.

Real Mac native execution, interactive Windows file dialogs, actual Swift/Draw Things, controlled live-provider checks and Final Cut/iMovie imports remain open. GoSX is now the normal launch path; Python remains an explicit fallback until those broader retirement gates pass.
