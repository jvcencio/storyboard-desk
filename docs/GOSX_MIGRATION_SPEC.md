# SBD full GoSX and Go migration specification

## Outcome and release gate

Storyboard Desk is a GoSX product: GoSX owns the interactive workspace and native host, and Go owns the local service, project storage and integrations. Python and the legacy browser UI are not required by the default launch path. FFmpeg/ffprobe, OS WebViews and provider accounts remain external dependencies where needed. Windows defaults to native hosting; Mac/Linux default to the browser, with optional Mac native hosting verified on a physical M1 Max. At the requested cutover, retire Python source/tooling and keep support claims scoped to executed checks. Historical Python verification and captured synthetic compatibility fixtures preserve the legacy format contract.

## Existing contract and constraints

- Projects live outside the checkout in a configurable data directory. Each project has `<id>/project.json` and associated media, jobs and exports. Existing JSON contains shot fields, IDs, media selections, approvals, music/section data, cloud reservations and possibly fields unknown to a newer or older client.
- The Python store validates IDs, writes JSON via a synced temporary file and rename, and increments project revisions. Updates reject stale revisions and retain media/approval state. Loads also derive coverage, music timing and approval data; these derived values are not all persistent fields. Media and paid generation operations add further invariants.
- The service binds to loopback and protects mutations with a per-run token and origin checks. Imported files are copied; source files remain untouched. A missing configured `/Volumes` drive must not be silently recreated.
- Generation requires explicit review, credentials and spending/reservation safeguards; handoffs do not themselves make paid requests. Failed/uncertain requests must not release reservations incorrectly. Exports must preserve current media and music semantics.
- Only one writer may own a data directory. The updated Python and Go stores share an OS ownership lock in addition to their process-local mutation locks. Stop older builds before upgrading; they do not implement this protocol.

## Architecture

GoSX components provide project navigation, direction and shot editing, media library, generation/review, music and sections, settings, and handoffs. Browser behavior is authored in Go and compiled to a GoSX Go-WASM engine. The build uses Go and supplied framework loaders, without application JavaScript or a Node toolchain. A Go application service exposes typed commands and queries to these components; a Go repository owns durable project/media/job data and concurrency. Provider and media adapters isolate HTTP, FFmpeg, platform helpers and filesystem access. Compatibility fixtures capture the retiring Python service as a fixed behavioral oracle; ongoing tests run in Go without that runtime.

## Milestones and acceptance criteria

1. **Read-only storage compatibility (initial implementation).** Inspect existing project JSON without starting Python, creating directories, modifying data or discarding unknown JSON fields. Reject invalid IDs, malformed documents and unavailable paths. Exercise real Python-created projects. This is not the Python store's derived view and must not be presented as a replacement writer.
2. **Safe Go ownership and core store.** Implement exclusive data-directory ownership across processes, guarded volume availability, atomic durable writes, revisions, validation, project creation/edit/insertion, and legacy JSON/media preservation. Prove stale/concurrent writes and crash recovery with Go and Python fixture tests. Never run Go and Python writers on the same data directory without an interoperable ownership mechanism.
3. **GoSX core workspace.** Replace project navigation, all direction and shot fields, reorder/insert, references, project settings, imports and feedback; test usability and parity, including escaping and unsaved edits.
4. **Media and exports.** Port copied imports, selection, hashes, probes, clips, review packages and exports. Test source preservation, traversal rejection, invalid media, FFmpeg errors and actual representative exports on supported platforms.
5. **Generation and safety.** Port local drawing and cloud provider adapters, credentials, prompts, review and approval state, reservation accounting, job persistence and failure recovery. Use fake providers for deterministic tests; prohibit unconfirmed paid calls.
6. **Music and sections.** Port soundtrack, timing, coverage, portions, selection, handoffs and reels; compare behavior and output with representative Python fixtures and media.
7. **Product cutover.** GoSX is the default, with a native Windows window and a Mac/Linux browser experience; no Python runtime or legacy UI is needed. Mac native hosting is available through the app bundle or `-native`, with execution evidence recorded separately. Provide migration/backup and historical rollback instructions. Preserve supported-platform launch/persistence/export evidence, replace Python-driven checks with Go regressions, and list unexecuted integrations explicitly.

## Risks and sequencing

Critical path is safe storage ownership → core domain → dependent media/generation/music workflows → platform cutover. Highest risks are project/media loss and accidental paid calls: mitigate with backups, no mixed writers, atomic writes, differential fixtures and explicit approval/reservation checks. Real Windows native/NTFS checks now support making GoSX the normal launch path. Unverified Mac native/local-render/editor integrations remain explicit verification work; Python and Legacy launchers have now been retired; the migration commit preserves their final source in history.

## Current status

| Milestone | Implementation and evidence | Remaining acceptance work |
| --- | --- | --- |
| 1–2: Reader/core storage | Implemented; opaque JSON/numbers, ownership, revisions, stale/concurrent writes, staged creation and killed-writer recovery tested on Linux, Windows NTFS and physical Apple Silicon Mac; Windows junction/symlink guards exercised in CI. | Process-crash/storage checks complete on the tested hosts. |
| 3: Core workspace | Implemented in GoSX; desktop/mobile Chromium and actual Windows WebView2 editing/reopen/navigation checks retained. Actual Mac WKWebView Go-WASM rendering and native picker/import pass on their recorded sources. | Windows native file dialogs; broader Mac native usability. |
| 4: Media/exports | Implemented; source hashes and real FFmpeg processing on Linux/Windows/Mac, plus installed Mac bundle exports using its Swift AVFoundation helpers. Actual Mac Save cancel/reopen/save passes with the default filename/folder, valid ZIP CRCs and unchanged source bytes. | Windows native Save dialogs, Mac custom Save name/folder and native dirty drafts, actual editor imports. |
| 5: Generation | Implemented; credentials, manual review, quote/brief/allowance gates, reservations, recoverable jobs, original-starter binding, optional offline drawing. Fake providers/child renderers test failure, recovery and cancellation. | Controlled live-provider and actual Mac Draw Things/memory checks. |
| 6: Music/sections | Implemented; PCM WAV, cues, timing, portions, coverage/reservations, assembly/undo, previews and packages; complete Go fixtures reopen in Python. | Actual iMovie/Final Cut soundtrack/timeline review. |
| 7: Cutover | Standard launchers and executable select GoSX. Actual Windows native-window and Linux launcher checks pass. A physical Mac has updated native Go-WASM window, Save cancel/reopen/save and historical picker/import evidence. Python and application JavaScript are removed; the editor engine, tests and packaging use Go. | Windows native dialogs and ARM64 execution, scoped Mac Save follow-ups, remaining integrations and signing/distribution. |

See the [retirement verification](gosx-retirement-verification.json), [workspace guide](GOSX_PORT.md) and [Windows/default-launch report](gosx-windows-verification.json) for current commands, source and limits. The [full-port report](gosx-port-verification.json) and [storage report](gosx-storage-verification.json) retain their earlier source snapshots.
