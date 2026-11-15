//go:build js && wasm

package main

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"syscall/js"
	"time"

	"m31labs.dev/gosx/client/browser"
)

type blockingClient struct {
	*eventScope
	root, form, surface                       js.Value
	forms                                     *formState
	previous, mode                            string
	version, commandRevision, captureRevision int
	busy, pending, dragging                   bool
	timer                                     *time.Timer
	rafID                                     int
	rafCallback                               js.Func
}

func mountBlocking(ctx context.Context, root js.Value, forms *formState) *blockingClient {
	ui := &blockingClient{eventScope: newEventScope(ctx), root: root, form: query(root, "form"), surface: query(root, "[data-gosx-scene3d]"), forms: forms, mode: "plan"}
	values := formValues(ui.form)
	values.Set("mode", ui.mode)
	ui.previous = values.Encode()
	ui.on(root, "input", func(e js.Value) {
		if ui.form.Call("contains", e.Get("target")).Bool() {
			ui.previewSoon()
		}
	})
	ui.on(root, "change", func(e js.Value) {
		if ui.form.Call("contains", e.Get("target")).Bool() {
			ui.previewSoon()
		}
	})
	ui.on(root, "click", func(e js.Value) {
		if button := closest(e.Get("target"), "[data-blocking-view]"); button.Truthy() {
			ui.mode = attr(button, "data-blocking-view")
			root.Call("setAttribute", "data-mode", ui.mode)
			each(root, "[data-blocking-view]", func(tab js.Value) {
				tab.Call("setAttribute", "aria-pressed", strconv.FormatBool(attr(tab, "data-blocking-view") == ui.mode))
			})
			ui.previewSoon()
		}
		if closest(e.Get("target"), "[data-blocking-capture]").Truthy() {
			if ui.mode != "camera" {
				ui.status("Switch to Camera to capture its frame.")
				return
			}
			if attr(root, "data-saved") != "true" || forms.protects(root) {
				ui.status("Save blocking before capturing a poster.")
				return
			}
			ui.captureRevision = -1
			ui.previewSoon()
		}
	})
	ui.on(ui.surface, "gosx:scene3d:commands-applied", func(e js.Value) {
		revision := e.Get("detail").Get("revision").Int()
		if revision != ui.commandRevision {
			return
		}
		ui.status("Preview ready")
		if ui.captureRevision == revision {
			ui.capture()
		}
	})
	ui.on(ui.surface, "pointerdown", func(e js.Value) {
		if ui.mode != "plan" || e.Get("button").Int() != 0 || e.Get("target").Get("tagName").String() != "CANVAS" {
			return
		}
		e.Call("preventDefault")
		ui.dragging = true
		e.Get("target").Call("setPointerCapture", e.Get("pointerId"))
		ui.place(e, true)
	})
	ui.on(ui.surface, "pointermove", func(e js.Value) {
		if ui.dragging {
			ui.place(e, false)
		}
	})
	for _, event := range []string{"pointerup", "pointercancel", "lostpointercapture"} {
		ui.on(ui.surface, event, func(js.Value) { ui.dragging = false })
	}
	ui.waitReady(0)
	return ui
}

func (ui *blockingClient) field(name string) js.Value {
	return ui.form.Get("elements").Call("namedItem", name)
}
func (ui *blockingClient) status(message string) {
	setText(query(ui.root, "#blocking-status"), message)
}

func (ui *blockingClient) waitReady(attempt int) {
	if ui.disposed {
		return
	}
	if attr(ui.surface, "data-gosx-scene3d-ready") == "true" {
		ui.status("Preview ready")
		ui.root.Call("setAttribute", "data-blocking-ready", "true")
		if ui.version > 0 {
			ui.preview()
		}
		return
	}
	if attempt >= 100 {
		ui.status("The 3D preview is unavailable. Positions can still be saved.")
		return
	}
	ui.timer = time.AfterFunc(100*time.Millisecond, func() { ui.waitReady(attempt + 1) })
}

func (ui *blockingClient) previewSoon() {
	ui.version++
	ui.status("Updating preview…")
	if ui.captureRevision > 0 {
		ui.captureRevision = 0
	}
	if attr(ui.root, "data-blocking-ready") != "true" {
		return
	}
	if ui.timer != nil {
		ui.timer.Stop()
	}
	ui.timer = time.AfterFunc(80*time.Millisecond, ui.preview)
}

func (ui *blockingClient) preview() {
	if ui.disposed {
		return
	}
	if ui.busy {
		ui.pending = true
		return
	}
	if !ui.form.Call("checkValidity").Bool() {
		ui.status("Check the highlighted fields to update the preview.")
		return
	}
	ui.busy = true
	version := ui.version
	values := formValues(ui.form)
	values.Set("mode", ui.mode)
	current := values.Encode()
	values.Set("previous", ui.previous)
	ui.status("Updating preview…")
	go func() {
		defer func() {
			ui.busy = false
			if ui.pending && !ui.disposed {
				ui.pending = false
				ui.preview()
			}
		}()
		response, err := browser.Fetch(ui.ctx, browser.FetchRequest{URL: "/blocking/preview", Method: "POST", ContentType: "application/x-www-form-urlencoded", Body: []byte(values.Encode())})
		if ui.disposed || version != ui.version {
			ui.pending = true
			return
		}
		if err != nil || response.Status != 200 {
			ui.status("Preview could not update. Check the fields and try again.")
			ui.captureRevision = 0
			return
		}
		data := js.Global().Get("JSON").Call("parse", string(response.Body))
		ui.previous = current
		ui.commandRevision++
		if ui.captureRevision == -1 {
			ui.captureRevision = ui.commandRevision
		}
		detail := map[string]any{"revision": ui.commandRevision, "commands": data.Get("commands")}
		event := js.Global().Get("CustomEvent").New("gosx:scene3d:commands", map[string]any{"detail": detail})
		ui.surface.Call("dispatchEvent", event)
	}()
}

func (ui *blockingClient) place(e js.Value, selectNearest bool) {
	canvas := query(ui.surface, "canvas")
	box := canvas.Call("getBoundingClientRect")
	x := (e.Get("clientX").Float()-box.Get("left").Float())/box.Get("width").Float()*16 - 8
	z := (e.Get("clientY").Float()-box.Get("top").Float())/box.Get("height").Float()*10 - 5
	index, _ := strconv.Atoi(query(ui.root, `input[name="selected_actor"]:checked`).Get("value").String())
	if selectNearest {
		count, _ := strconv.Atoi(ui.field("actor_count").Get("value").String())
		for i := 0; i < count; i++ {
			ax, _ := strconv.ParseFloat(ui.field(fmt.Sprintf("actor.%d.x", i)).Get("value").String(), 64)
			az, _ := strconv.ParseFloat(ui.field(fmt.Sprintf("actor.%d.z", i)).Get("value").String(), 64)
			if math.Hypot(ax-x, az-z) < .6 {
				query(ui.root, fmt.Sprintf(`input[name="selected_actor"][value="%d"]`, i)).Set("checked", true)
				return
			}
		}
	}
	for name, n := range map[string]float64{"x": math.Max(-8, math.Min(8, x)), "z": math.Max(-5, math.Min(5, z))} {
		field := ui.field(fmt.Sprintf("actor.%d.%s", index, name))
		field.Set("value", strconv.FormatFloat(math.Round(n*100)/100, 'f', 2, 64))
		field.Call("dispatchEvent", js.Global().Get("Event").New("input", map[string]any{"bubbles": true}))
	}
}

// GoSX schedules its draw before commands-applied. Capturing in the next
// animation callback reads that draw before WebGL's buffer is discarded.
func (ui *blockingClient) capture() {
	if ui.rafID != 0 {
		js.Global().Call("cancelAnimationFrame", ui.rafID)
		ui.rafCallback.Release()
	}
	version := ui.version
	ui.captureRevision = 0
	ui.rafCallback = js.FuncOf(func(js.Value, []js.Value) any {
		ui.rafID = 0
		defer ui.rafCallback.Release()
		if ui.disposed || ui.version != version || ui.forms.protects(ui.root) {
			return nil
		}
		image := query(ui.surface, "canvas").Call("toDataURL", "image/png").String()
		values := formValues(ui.form)
		values.Set("image", image)
		ui.status("Saving camera poster…")
		go ui.savePoster(values)
		return nil
	})
	ui.rafID = js.Global().Call("requestAnimationFrame", ui.rafCallback).Int()
}

func (ui *blockingClient) savePoster(values url.Values) {
	response, err := browser.Fetch(ui.ctx, browser.FetchRequest{URL: "/blocking/poster", Method: "POST", ContentType: "application/x-www-form-urlencoded", Body: []byte(values.Encode())})
	if ui.disposed {
		return
	}
	if err != nil {
		ui.status("Poster save could not be confirmed. Reopen the shot to check its saved poster.")
		return
	}
	if response.Status != 200 {
		ui.status(string(response.Body))
		return
	}
	data := js.Global().Get("JSON").Call("parse", string(response.Body))
	ui.field("revision").Set("value", strconv.Itoa(data.Get("revision").Int()))
	poster := query(ui.root, ".blocking-poster")
	if !poster.Truthy() {
		poster = js.Global().Get("document").Call("createElement", "img")
		poster.Set("className", "blocking-poster")
		poster.Set("alt", "Saved camera blocking study")
		query(ui.root, "#blocking-status").Call("after", poster)
	}
	poster.Set("src", data.Get("url"))
	ui.status("Camera poster saved")
}

func (ui *blockingClient) Dispose() {
	if ui.timer != nil {
		ui.timer.Stop()
	}
	if ui.rafID != 0 {
		js.Global().Call("cancelAnimationFrame", ui.rafID)
		ui.rafCallback.Release()
		ui.rafID = 0
	}
	ui.eventScope.Dispose()
}
