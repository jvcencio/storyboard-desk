//go:build js && wasm

package main

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"syscall/js"
	"time"

	"github.com/jvcencio/storyboard-desk/gosx-host/internal/blockingflow"
	"m31labs.dev/gosx/client/browser"
)

// blockingClient connects the blocking page to blockingflow.Flow. It reports
// DOM events to the flow and performs the effects the flow returns; the preview
// and capture lifecycle lives in the flow, not in flags here.
type blockingClient struct {
	*eventScope
	root, form, surface js.Value
	forms               *formState
	flow                *blockingflow.Flow
	previous            string // encoded form values the scene shows now
	dragging            bool
	debounce, readiness *time.Timer
	frameID             int
	frame               js.Func
}

func mountBlocking(ctx context.Context, root js.Value, forms *formState) *blockingClient {
	ui := &blockingClient{eventScope: newEventScope(ctx), root: root, form: query(root, `form[action="/blocking/save"]`),
		surface: query(root, "[data-gosx-scene3d]"), forms: forms, flow: blockingflow.New(blockingflow.Plan)}
	ui.previous = ui.values().Encode()
	changed := func(e js.Value) {
		if ui.form.Call("contains", e.Get("target")).Bool() {
			ui.apply(ui.flow.Changed())
		}
	}
	ui.on(root, "input", changed)
	ui.on(root, "change", changed)
	ui.on(root, "click", func(e js.Value) {
		if button := closest(e.Get("target"), "[data-blocking-view]"); button.Truthy() {
			mode := attr(button, "data-blocking-view")
			root.Call("setAttribute", "data-mode", mode)
			each(root, "[data-blocking-view]", func(tab js.Value) {
				tab.Call("setAttribute", "aria-pressed", strconv.FormatBool(attr(tab, "data-blocking-view") == mode))
			})
			ui.apply(ui.flow.ModeChanged(blockingflow.Mode(mode)))
		}
		if closest(e.Get("target"), "[data-blocking-capture]").Truthy() {
			ui.apply(ui.flow.CaptureClicked(attr(root, "data-saved") == "true", forms.protects(root)))
		}
	})
	ui.on(ui.surface, "gosx:scene3d:commands-applied", func(e js.Value) {
		ui.apply(ui.flow.Applied(e.Get("detail").Get("revision").Int()))
	})
	ui.on(ui.surface, "pointerdown", func(e js.Value) {
		if ui.flow.Mode() != blockingflow.Plan || e.Get("button").Int() != 0 || e.Get("target").Get("tagName").String() != "CANVAS" {
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

// values returns the blocking form together with the stage view.
func (ui *blockingClient) values() url.Values {
	values := formValues(ui.form)
	values.Set("mode", string(ui.flow.Mode()))
	return values
}

// apply performs what the flow asked for. Effects that read the canvas run
// before this function returns, inside the animation frame that requested them.
func (ui *blockingClient) apply(effect blockingflow.Effect) {
	if ui.disposed {
		return
	}
	if effect.CancelFrame {
		ui.cancelFrame()
	}
	if effect.Status != "" {
		setText(query(ui.root, "#blocking-status"), effect.Status)
	}
	if effect.Debounce {
		if ui.debounce != nil {
			ui.debounce.Stop()
		}
		ui.debounce = time.AfterFunc(80*time.Millisecond, ui.previewDue)
	}
	if effect.Fetch {
		ui.fetch()
	}
	if effect.Frame {
		ui.requestFrame()
	}
	if effect.Save {
		ui.save()
	}
	if effect.Redo {
		ui.previewDue()
	}
}

func (ui *blockingClient) previewDue() {
	if !ui.disposed {
		ui.apply(ui.flow.PreviewDue(ui.form.Call("checkValidity").Bool()))
	}
}

// waitReady polls for the 3D surface. After ten seconds it reports the scene
// unavailable and keeps looking once a second, so a slow device still recovers.
func (ui *blockingClient) waitReady(attempt int) {
	if ui.disposed {
		return
	}
	if attr(ui.surface, "data-gosx-scene3d-ready") == "true" {
		ui.root.Call("setAttribute", "data-blocking-ready", "true")
		ui.apply(ui.flow.SceneReady())
		return
	}
	delay := 100 * time.Millisecond
	if attempt >= 100 {
		if attempt == 100 {
			ui.apply(ui.flow.SceneUnavailable())
		}
		delay = time.Second
	}
	ui.readiness = time.AfterFunc(delay, func() { ui.waitReady(attempt + 1) })
}

func (ui *blockingClient) fetch() {
	values := ui.values()
	current := values.Encode()
	values.Set("previous", ui.previous)
	body := []byte(values.Encode())
	go func() {
		response, err := browser.Fetch(ui.ctx, browser.FetchRequest{URL: "/blocking/preview", Method: "POST", ContentType: "application/x-www-form-urlencoded", Body: body})
		if ui.disposed {
			return
		}
		effect := ui.flow.PreviewFetched(err == nil && response.Status == 200)
		if effect.Send > 0 {
			ui.previous = current
			commands := js.Global().Get("JSON").Call("parse", string(response.Body)).Get("commands")
			detail := map[string]any{"revision": effect.Send, "commands": commands}
			ui.surface.Call("dispatchEvent", js.Global().Get("CustomEvent").New("gosx:scene3d:commands", map[string]any{"detail": detail}))
		}
		ui.apply(effect)
	}()
}

func (ui *blockingClient) place(e js.Value, selectNearest bool) {
	canvas := query(ui.surface, "canvas")
	box := canvas.Call("getBoundingClientRect")
	x := (e.Get("clientX").Float()-box.Get("left").Float())/box.Get("width").Float()*16 - 8
	z := (e.Get("clientY").Float()-box.Get("top").Float())/box.Get("height").Float()*10 - 5
	if !finite(x) || !finite(z) {
		return // the canvas has no size yet
	}
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

// GoSX schedules its draw before commands-applied. The next animation frame
// reads that draw before WebGL discards its buffer.
func (ui *blockingClient) requestFrame() {
	ui.cancelFrame()
	var callback js.Func
	callback = js.FuncOf(func(js.Value, []js.Value) any {
		ui.frameID = 0
		callback.Release()
		ui.apply(ui.flow.Framed(ui.forms.protects(ui.root)))
		return nil
	})
	ui.frame = callback
	ui.frameID = js.Global().Call("requestAnimationFrame", callback).Int()
}

func (ui *blockingClient) cancelFrame() {
	if ui.frameID != 0 {
		js.Global().Call("cancelAnimationFrame", ui.frameID)
		ui.frame.Release()
		ui.frameID = 0
	}
}

// save reads the canvas now, while the frame the flow asked for is current,
// then posts it.
func (ui *blockingClient) save() {
	values := formValues(ui.form)
	values.Set("image", query(ui.surface, "canvas").Call("toDataURL", "image/png").String())
	body := []byte(values.Encode())
	go func() {
		response, err := browser.Fetch(ui.ctx, browser.FetchRequest{URL: "/blocking/poster", Method: "POST", ContentType: "application/x-www-form-urlencoded", Body: body})
		if ui.disposed {
			return
		}
		if err != nil {
			ui.apply(ui.flow.PosterDone(false, ""))
			return
		}
		if response.Status != 200 {
			ui.apply(ui.flow.PosterDone(false, strings.TrimSpace(string(response.Body))))
			return
		}
		data := js.Global().Get("JSON").Call("parse", string(response.Body))
		ui.field("revision").Set("value", strconv.Itoa(data.Get("revision").Int()))
		ui.showPoster(data.Get("url"))
		ui.apply(ui.flow.PosterDone(true, ""))
	}()
}

// showPoster displays the new poster and offers it as a composition sketch.
// A note about a sketch made from the previous poster no longer applies.
func (ui *blockingClient) showPoster(source js.Value) {
	poster := query(ui.root, ".blocking-poster")
	if !poster.Truthy() {
		poster = js.Global().Get("document").Call("createElement", "img")
		poster.Set("className", "blocking-poster")
		poster.Set("alt", "Saved camera blocking study")
		query(ui.root, "#blocking-status").Call("after", poster)
	}
	poster.Set("src", source)
	if note := query(ui.root, "[data-blocking-study-note]"); note.Truthy() {
		note.Call("remove")
	}
	if offer := query(ui.root, "[data-blocking-study-offer]"); offer.Truthy() {
		offer.Call("removeAttribute", "hidden")
		// Every form on the page carries the project revision, and the poster
		// request just advanced it.
		if revision := query(offer, `input[name="revision"]`); revision.Truthy() {
			revision.Set("value", ui.field("revision").Get("value"))
		}
	}
}

func (ui *blockingClient) Dispose() {
	for _, timer := range []*time.Timer{ui.debounce, ui.readiness} {
		if timer != nil {
			timer.Stop()
		}
	}
	ui.cancelFrame()
	ui.eventScope.Dispose()
}
