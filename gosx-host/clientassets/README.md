The editor's browser behavior is authored in Go in `../client`.

Run `go run ./cmd/client` from `gosx-host` to build it with the pinned GoSX
compiler and the standard Go WebAssembly toolchain. The packager also runs this
step. Generated WebAssembly and unmodified GoSX browser runtime assets are
ignored by Git and embedded in the application executable.

No Node, npm, application JavaScript, or JavaScript build tools are required.
