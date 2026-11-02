# SBD full GoSX and Go migration specification

## Outcome and release gate

Storyboard Desk is a GoSX product: GoSX owns the interactive workspace and native host, and Go owns the local service, project storage and integrations. Python and the legacy browser UI are not required by the default launch path. FFmpeg/ffprobe, OS WebViews and provider accounts remain external dependencies where needed. Default launch follows verified Windows native and Linux browser workflows; Mac uses the browser until native execution is verified. Retain the explicit Python fallback until the remaining platform/integration retirement gates pass, and keep support claims scoped to executed checks.

## Existing contract and constraints

- Projects live outside the checkout in a configurable data directory. Each project has `<id>/project.json` and associated media, jobs and exports. Existing JSON contains shot fields, IDs, media selections, approvals, music/section data, cloud reservations and possibly fields unknown to a newer or older client.
- The Python store validates IDs, writes JSON via a synced temporary file and rename, and increments project revisions. Updates reject stale revisions and retain media/approval state. Loads also derive coverage, music timing and approval data; these derived values are not all persistent fields. Media and paid generation operations add further invariants.
- The service binds to loopback and protects mutations with a per-run token and origin checks. Imported files are copied; source files remain untouched. A missing configured `/Volumes` drive must not be silently recreated.
- Generation requires explicit review, credentials and spending/reservation safeguards; handoffs do not themselves make paid requests. Failed/uncertain requests must not release reservations incorrectly. Exports must preserve current media and music semantics.
- Only one writer may own a data directory. The updated Python and Go stores share an OS ownership lock in addition to their process-local mutation locks. Stop older builds before upgrading; they do not implement this protocol.

## Architecture

GoSX components provide project navigation, direction and shot editing, media library, generation/review, music and sections, settings, and handoffs. A Go application service exposes typed commands and queries to these components; a Go repository owns durable project/media/job data and concurrency. Provider and media adapters isolate HTTP, FFmpeg, platform helpers and filesystem access. Compatibility fixtures and tests use the current Python service as the behavioral oracle during migration, not as a production dependency.

## Milestones and acceptance criteria

1. **Read-only storage compatibility (initial implementation).** Inspect existing project JSON without starting Python, creating directories, modifying data or discarding unknown JSON fields. Reject invalid IDs, malformed documents and unavailable paths. Exercise real Python-created projects. This is not the Python store's derived view and must not be presented as a replacement writer.
2. **Safe Go ownership and core store.** Implement exclusive data-directory ownership across processes, guarded volume availability, atomic durable writes, revisions, validation, project creation/edit/insertion, and legacy JSON/media preservation. Prove stale/concurrent writes and crash recovery with Go and Python fixture tests. Never run Go and Python writers on the same data directory without an interoperable ownership mechanism.
3. **GoSX core workspace.** Replace project navigation, all direction and shot fields, reorder/insert, references, project settings, imports and feedback; test usability and parity, including escaping and unsaved edits.
4. **Media and exports.** Port copied imports, selection, hashes, probes, clips, review packages and exports. Test source preservation, traversal rejection, invalid media, FFmpeg errors and actual representative exports on supported platforms.
5. **Generation and safety.** Port local drawing and cloud provider adapters, credentials, prompts, review and approval state, reservation accounting, job persistence and failure recovery. Use fake providers for deterministic tests; prohibit unconfirmed paid calls.
6. **Music and sections.** Port soundtrack, timing, coverage, portions, selection, handoffs and reels; compare behavior and output with representative Python fixtures and media.
7. **Product cutover.** GoSX is the default, with a native Windows window and a Mac/Linux browser experience; no Python runtime or legacy UI is needed. Mac native hosting is optional until target execution passes. Provide migration/backup and explicit fallback instructions. Pass real supported-platform launch/persistence/export checks and the remaining integration gates before retiring Python.

## Risks and sequencing

Critical path is safe storage ownership → core domain → dependent media/generation/music workflows → platform cutover. Highest risks are project/media loss and accidental paid calls: mitigate with backups, no mixed writers, atomic writes, differential fixtures and explicit approval/reservation checks. Real Windows native/NTFS checks now support making GoSX the normal launch path. Unverified Mac native/local-render/editor integrations remain explicit verification work; Python remains available through the Legacy launchers.

## Current status

| Milestone | Implementation and evidence | Remaining acceptance work |
| --- | --- | --- |
| 1–2: Reader/core storage | Implemented; opaque JSON/numbers, ownership, revisions, stale/concurrent writes, staged creation and killed-writer recovery tested on Linux and Windows NTFS; Windows junction guards exercised. | Mac filesystem/locks; privileged Windows symlink cases. |
| 3: Core workspace | Implemented in GoSX; desktop/mobile Chromium and actual Windows WebView2 editing/reopen/navigation checks retained. | Interactive native file dialogs; Mac native usability. |
| 4: Media/exports | Implemented; source hashes and real FFmpeg processing on Linux/Windows, plus Windows WebView2 reel/editor-package downloads to an isolated directory. | Interactive save dialogs, Swift helpers and actual editor imports. |
| 5: Generation | Implemented; credentials, manual review, quote/brief/allowance gates, reservations, recoverable jobs, original-starter binding, optional offline drawing. Fake providers/child renderers test failure, recovery and cancellation. | Controlled live-provider and actual Mac Draw Things/memory checks. |
| 6: Music/sections | Implemented; PCM WAV, cues, timing, portions, coverage/reservations, assembly/undo, previews and packages; complete Go fixtures reopen in Python. | Actual iMovie/Final Cut soundtrack/timeline review. |
| 7: Cutover | Standard launchers and executable now select GoSX. Actual Windows standard-launcher/native-window and Linux launcher checks pass. Python is explicit through `--legacy`/Legacy launchers. | Mac native and Windows ARM64 execution, remaining integrations, signing/distribution and retirement sign-off. |

See the [workspace guide](GOSX_PORT.md) and [Windows/default-launch report](gosx-windows-verification.json) for current commands, source and limits. The [full-port report](gosx-port-verification.json) and [storage report](gosx-storage-verification.json) retain their earlier source snapshots.
