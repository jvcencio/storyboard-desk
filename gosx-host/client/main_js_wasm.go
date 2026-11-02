//go:build js && wasm

// The editor's browser behavior runs as Go-WASM inside the GoSX engine lifecycle.
package main

import (
	"fmt"
	"syscall/js"

	"m31labs.dev/gosx/engine/wasm"
)

func main() {
	if err := wasm.Register("Workspace", func(ctx wasm.Context) (wasm.Handle, error) {
		ui := &workspaceClient{doc: js.Global().Get("document")}
		ui.mount()
		return ui, nil
	}); err != nil {
		panic(fmt.Errorf("register editor: %w", err))
	}
	select {}
}
