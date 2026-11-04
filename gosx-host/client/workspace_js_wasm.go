//go:build js && wasm

package main

import (
	"context"
	"syscall/js"

	"m31labs.dev/gosx/engine/wasm"
)

// The engine coordinates independent controllers. Each controller owns its
// listeners and pending work; form drafts never share playback state.
type workspaceClient struct {
	doc         js.Value
	cancel      context.CancelFunc
	controllers []wasm.Handle
	disposed    bool
}

func (ui *workspaceClient) mount() {
	ctx, cancel := context.WithCancel(context.Background())
	ui.cancel = cancel
	ui.controllers = append(ui.controllers, mountFormState(ctx, ui.doc), mountCapabilities(ctx, ui.doc))
	if room := query(ui.doc, "[data-room-events]"); room.Truthy() {
		ui.controllers = append(ui.controllers, mountRoom(ctx, room))
	}
	each(ui.doc, "[data-cloud-form]", func(form js.Value) {
		ui.controllers = append(ui.controllers, mountCloudControls(ctx, form))
	})
	each(ui.doc, "[data-review]", func(root js.Value) {
		ui.controllers = append(ui.controllers, mountReview(ctx, root))
	})
	ui.doc.Get("body").Call("setAttribute", "data-editor-ready", "true")
}

func (ui *workspaceClient) Dispose() {
	if ui.disposed {
		return
	}
	ui.disposed = true
	ui.cancel()
	for i := len(ui.controllers) - 1; i >= 0; i-- {
		ui.controllers[i].Dispose()
	}
	ui.controllers = nil
}
