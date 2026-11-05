//go:build js && wasm

package main

import (
	"context"
	"syscall/js"
)

// The engine coordinates independent controllers. Each controller owns its
// listeners and pending work; form drafts never share playback state.
type workspaceClient struct {
	doc       js.Value
	cancel    context.CancelFunc
	disposers []func()
	disposed  bool
}

func (ui *workspaceClient) mount() {
	ctx, cancel := context.WithCancel(context.Background())
	ui.cancel = cancel
	forms := mountFormState(ctx, ui.doc)
	ui.disposers = append(ui.disposers, forms.Dispose, mountCapabilities(ctx, ui.doc).Dispose)
	if room := query(ui.doc, "[data-room-events]"); room.Truthy() {
		ui.disposers = append(ui.disposers, mountRoom(ctx, room, forms))
	}
	each(ui.doc, "[data-cloud-form]", func(form js.Value) {
		ui.disposers = append(ui.disposers, mountCloudControls(ctx, form).Dispose)
	})
	each(ui.doc, "[data-review]", func(root js.Value) {
		ui.disposers = append(ui.disposers, mountReview(ctx, root).Dispose)
	})
	ui.doc.Get("body").Call("setAttribute", "data-editor-ready", "true")
}

func (ui *workspaceClient) Dispose() {
	if ui.disposed {
		return
	}
	ui.disposed = true
	ui.cancel()
	for i := len(ui.disposers) - 1; i >= 0; i-- {
		ui.disposers[i]()
	}
	ui.disposers = nil
}
