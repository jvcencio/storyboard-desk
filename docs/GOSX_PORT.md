# Storyboard Desk GoSX port

The destination is a GoSX UI backed by Go services, with compatibility for existing Storyboard Desk projects. This is an incremental port: the current Python service and browser UI remain the production implementation while functionality is moved and verified.

## Experimental shot editor

Start the current Storyboard Desk service first, preferably with a **backup or separate development data folder**. In a second terminal:

```sh
cd gosx-host
go run . -editor -url http://127.0.0.1:4492/
```

Open the printed loopback address in a browser and keep both processes running. The editor shows all projects and shots, and can change a shot's title, purpose, composition, action and camera. It saves through the existing Python service and preserves other shot fields. Each form is tied to the project revision displayed on the page: if another edit changes that revision, the save is rejected; reload and reapply your changes. Do not use this experimental UI for work you cannot afford to lose.

The editor runs on a random loopback port with a per-process form token and origin checks; it is not intended for a network or shared computer. The existing service remains responsible for validation and storage. Generation, media management, music, sections, project creation, and the rest of the existing interface have **not** been ported. The native host and standard launcher still open the existing UI by default. The editor does not yet provide feature parity, and no Python dependency has been removed.

For a static, read-only rendering of current projects:

```sh
cd gosx-host
go run . -board-preview -url http://127.0.0.1:4492/ > board-preview.html
```

The output contains project data; treat the file as private. It does not contain the service token.

## Developing the port

`board.gsx` is the GoSX source for the shot card; `board_generated.go` is checked-in Go compiler output. With the pinned GoSX compiler installed, regenerate it with `gosx compile board.gsx` and retain the generated-code notice at the top. `test_gosx_compile.py` checks that it matches when the compiler is available. GoSX depends on gotreesitter through its own compiler; no separate Canopy or Hyphae integration is required for this slice.

The Go client retains unknown shot fields and submits the service revision and token. Tests exercise the editor's local request protections and an actual Python service round trip, including a stale revision. Run `go test ./...` in `gosx-host` and `python3 -m unittest discover -s . -p 'test_*.py'` from the repository root.

Next work: port navigation and remaining shot fields into GoSX; migrate project storage and revision handling into a Go service behind compatibility tests; then port media, cloud safeguards, music/sections and handoffs. Preserve existing project formats and verify each workflow before replacing the corresponding Python route. Only switch the default desktop experience and retire Python after parity and platform verification.
