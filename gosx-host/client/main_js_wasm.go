//go:build js && wasm

// The editor's browser behavior runs as Go-WASM inside the GoSX engine lifecycle.
package main

import (
	"os"
	"syscall/js"
)

func main() {
	// Match the pinned GoSX loader's registration ABI. Importing
	// engine/wasm for this factory would initialize Scene3D and its parser.
	// The review browser test exercises the loader's mount and dispose contract.
	register := js.Global().Get("__gosx_register_go_wasm_engine_factory")
	token := os.Getenv("GOSX_GO_WASM_REGISTRATION_TOKEN")
	if register.Type() != js.TypeFunction || token == "" {
		panic("GoSX Workspace registration is closed")
	}
	factory := js.FuncOf(mountWorkspace)
	accepted := register.Invoke(token, "Workspace", factory)
	if accepted.Type() != js.TypeBoolean || !accepted.Bool() {
		factory.Release()
		panic("GoSX rejected Workspace registration")
	}
	// GoSX retains the registered factory for the browser document's lifetime.
	select {}
}

func mountWorkspace(_ js.Value, args []js.Value) any {
	if len(args) == 0 || (args[0].Type() != js.TypeObject && args[0].Type() != js.TypeFunction) {
		err := js.Global().Get("Error").New("GoSX Workspace mount context is missing")
		return js.Global().Get("Promise").Call("reject", err)
	}
	ui := &workspaceClient{doc: js.Global().Get("document")}
	ui.mount()
	handle := js.Global().Get("Object").New()
	var dispose js.Func
	dispose = js.FuncOf(func(js.Value, []js.Value) any {
		handle.Set("dispose", js.Undefined())
		defer dispose.Release()
		ui.Dispose()
		return nil
	})
	handle.Set("dispose", dispose)
	return handle
}
