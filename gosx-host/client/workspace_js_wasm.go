//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strings"
	"syscall/js"

	"m31labs.dev/gosx/client/browser"
	"m31labs.dev/gosx/client/jsutil"
)

type listener struct {
	target   js.Value
	event    string
	callback js.Func
}
type workspaceClient struct {
	doc              js.Value
	ctx              context.Context
	cancel           context.CancelFunc
	listeners        []listener
	dirty            []js.Value
	players          []*reviewPlayer
	disposed, saving bool
}

func query(root js.Value, selector string) js.Value {
	if !root.Truthy() {
		return js.Null()
	}
	return root.Call("querySelector", selector)
}
func each(root js.Value, selector string, fn func(js.Value)) {
	if !root.Truthy() {
		return
	}
	items := root.Call("querySelectorAll", selector)
	for i := 0; i < items.Length(); i++ {
		fn(items.Index(i))
	}
}
func closest(node js.Value, selector string) js.Value {
	if !node.Truthy() || node.Get("closest").Type() != js.TypeFunction {
		return js.Null()
	}
	return node.Call("closest", selector)
}
func attr(node js.Value, name string) string {
	if !node.Truthy() {
		return ""
	}
	value := node.Call("getAttribute", name)
	if value.Type() != js.TypeString {
		return ""
	}
	return value.String()
}
func setText(node js.Value, text string) {
	if node.Truthy() {
		node.Set("textContent", text)
	}
}
func finite(n float64) bool { return !math.IsNaN(n) && !math.IsInf(n, 0) }
func (ui *workspaceClient) on(target js.Value, event string, fn func(js.Value)) {
	if !target.Truthy() {
		return
	}
	callback := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if !ui.disposed && len(args) > 0 {
			fn(args[0])
		}
		return nil
	})
	target.Call("addEventListener", event, callback)
	ui.listeners = append(ui.listeners, listener{target, event, callback})
}

func (ui *workspaceClient) mount() {
	ui.ctx, ui.cancel = context.WithCancel(context.Background())
	for _, event := range []string{"input", "change"} {
		ui.on(ui.doc, event, func(e js.Value) { ui.markDirty(closest(e.Get("target"), "form[data-dirty]")) })
	}
	ui.on(ui.doc, "submit", ui.submit)
	ui.on(ui.doc, "click", ui.click)
	ui.on(js.Global(), "beforeunload", func(e js.Value) {
		if len(ui.dirty) > 0 {
			e.Call("preventDefault")
			e.Set("returnValue", "")
		}
	})
	each(ui.doc, "[data-json-file]", func(input js.Value) { ui.on(input, "change", func(js.Value) { ui.loadJSON(input) }) })
	each(ui.doc, "[data-asset] video", func(video js.Value) {
		metadata := func(js.Value) {
			duration := video.Get("duration").Float()
			if !finite(duration) || duration <= 0 {
				return
			}
			card := closest(video, "[data-asset]")
			each(card, `input[name="measured"]`, func(input js.Value) { input.Set("value", fmt.Sprint(duration)) })
			each(card, `input[name="end"]`, func(input js.Value) {
				if input.Get("value").String() == "" {
					input.Set("value", fmt.Sprint(duration))
				}
			})
		}
		ui.on(video, "loadedmetadata", metadata)
		if video.Get("readyState").Int() > 0 {
			metadata(js.Undefined())
		}
	})
	each(ui.doc, "[data-cloud-form]", ui.cloudForm)
	each(ui.doc, "[data-review]", ui.review)
	ui.doc.Get("body").Call("setAttribute", "data-editor-ready", "true")
}

func (ui *workspaceClient) Dispose() {
	if ui.disposed {
		return
	}
	ui.disposed = true
	ui.cancel()
	for _, player := range ui.players {
		player.pause()
	}
	for _, item := range ui.listeners {
		item.target.Call("removeEventListener", item.event, item.callback)
		item.callback.Release()
	}
	ui.listeners = nil
}

func (ui *workspaceClient) markDirty(form js.Value) {
	if !form.Truthy() {
		return
	}
	for _, known := range ui.dirty {
		if known.Equal(form) {
			return
		}
	}
	ui.dirty = append(ui.dirty, form)
	setText(ui.doc.Call("getElementById", "save-status"), "Unsaved changes")
}

func (ui *workspaceClient) submit(e js.Value) {
	form := e.Get("target")
	if form.Call("hasAttribute", "data-download-form").Bool() {
		if attr(ui.doc.Get("body"), "data-native-save") == "true" {
			e.Call("preventDefault")
			ui.saveNative(attr(form, "action"), formValues(form))
		}
		return
	}
	for _, other := range ui.dirty {
		if !other.Equal(form) && !js.Global().Call("confirm", "Other forms have unsaved changes. Continue and discard those edits?").Bool() {
			e.Call("preventDefault")
			return
		}
		if !other.Equal(form) {
			break
		}
	}
	ui.dirty = nil
}

func (ui *workspaceClient) click(e js.Value) {
	target := e.Get("target")
	if copy := closest(target, "[data-copy]"); copy.Truthy() {
		source := ui.doc.Call("getElementById", attr(copy, "data-copy"))
		if source.Truthy() {
			browser.WriteClipboardContext(ui.ctx, source.Get("value").String(), func(err error) {
				if ui.disposed {
					return
				}
				if err == nil {
					setText(copy, "Copied")
					return
				}
				source.Call("focus")
				source.Call("select")
				setText(copy, "Select text and copy")
			})
		}
	}
	anchor := closest(target, "a[href]")
	if !anchor.Truthy() || e.Get("ctrlKey").Bool() || e.Get("metaKey").Bool() || e.Get("button").Int() > 0 {
		return
	}
	href := attr(anchor, "href")
	if href == "#new-project" {
		form := ui.doc.Call("getElementById", "new-project")
		form.Set("open", true)
		query(form, `input[name="title"]`).Call("focus")
		return
	}
	if anchor.Call("hasAttribute", "download").Bool() {
		if attr(ui.doc.Get("body"), "data-native-save") == "true" {
			if values, ok := ui.downloadValues(href); ok {
				e.Call("preventDefault")
				ui.saveNative("/download", values)
			}
		}
		return
	}
	if len(ui.dirty) > 0 && !strings.HasPrefix(href, "#") {
		if !js.Global().Call("confirm", "Discard unsaved edits and leave this view?").Bool() {
			e.Call("preventDefault")
		} else {
			ui.dirty = nil
		}
	}
}

func formValues(form js.Value) url.Values {
	values := url.Values{}
	fields := form.Get("elements")
	for i := 0; i < fields.Length(); i++ {
		field := fields.Index(i)
		name, kind := field.Get("name").String(), field.Get("type").String()
		if name == "" || field.Get("disabled").Bool() || kind == "submit" || kind == "button" || kind == "reset" || kind == "file" {
			continue
		}
		if (kind == "checkbox" || kind == "radio") && !field.Get("checked").Bool() {
			continue
		}
		values.Add(name, field.Get("value").String())
	}
	return values
}

func (ui *workspaceClient) downloadValues(href string) (url.Values, bool) {
	parsed, err := url.Parse(href)
	if err != nil || parsed.IsAbs() || parsed.Host != "" {
		return nil, false
	}
	values := url.Values{}
	for _, name := range []string{"csrf", "project", "revision"} {
		if field := query(ui.doc, `input[name="`+name+`"]`); field.Truthy() {
			values.Set(name, field.Get("value").String())
		}
	}
	switch parsed.Path {
	case "/download/plan":
		values.Set("kind", "plan")
		values.Set("project", parsed.Query().Get("project"))
	case "/download/script":
		values.Set("kind", "script")
		values.Set("project", parsed.Query().Get("project"))
	default:
		parts := strings.SplitN(strings.TrimPrefix(parsed.Path, "/media/"), "/", 2)
		if !strings.HasPrefix(parsed.Path, "/media/") || len(parts) != 2 || !strings.HasPrefix(parts[1], "exports/") {
			return nil, false
		}
		values.Set("project", parts[0])
		values.Set("kind", "export")
		values.Set("path", parts[1])
	}
	return values, true
}

func (ui *workspaceClient) saveNative(action string, values url.Values) {
	status := ui.doc.Call("getElementById", "download-status")
	if ui.saving {
		setText(status, "Finish the open Save dialog first.")
		return
	}
	ui.saving = true
	setText(status, "Choose where to save…")
	go func() {
		defer func() { ui.saving = false }()
		response, err := browser.Fetch(ui.ctx, browser.FetchRequest{URL: action, Method: "POST", ContentType: "application/x-www-form-urlencoded", Headers: map[string]string{"Accept": "application/json"}, Body: []byte(values.Encode())})
		if ui.disposed {
			return
		}
		if err != nil {
			setText(status, "Saving could not finish. Check Exports before trying again.")
			return
		}
		var result struct {
			Saved       bool
			Name, Error string
		}
		if json.Unmarshal(response.Body, &result) != nil {
			setText(status, "Saving could not finish. Check Exports before trying again.")
			return
		}
		if result.Error != "" {
			setText(status, result.Error)
			return
		}
		if response.Status != 200 {
			setText(status, "Saving could not finish. Check Exports before trying again.")
			return
		}
		if result.Saved {
			setText(status, "Saved "+result.Name)
		} else {
			setText(status, "Save cancelled")
		}
	}()
}

func (ui *workspaceClient) loadJSON(input js.Value) {
	files := input.Get("files")
	if files.Length() == 0 {
		return
	}
	file := files.Index(0)
	status := query(input.Get("parentElement"), "[role=status]")
	if file.Get("size").Float() > 1048576 {
		setText(status, "Choose a JSON file smaller than 1 MiB.")
		return
	}
	go func() {
		text, err := jsutil.AwaitPromiseContext(ui.ctx, file.Call("text"))
		if ui.disposed || input.Get("files").Length() == 0 || !input.Get("files").Index(0).Equal(file) {
			return
		}
		if err != nil {
			setText(status, "The JSON file could not be read.")
			return
		}
		value := text.String()
		if !json.Valid([]byte(value)) {
			setText(status, "Choose a valid JSON file.")
			return
		}
		target := input.Get("form").Get("elements").Call("namedItem", attr(input, "data-json-file"))
		target.Set("value", value)
		target.Call("dispatchEvent", js.Global().Get("Event").New("input", map[string]any{"bubbles": true}))
		setText(status, "Loaded into the form. Review the text before applying it.")
	}()
}

func (ui *workspaceClient) cloudForm(form js.Value) {
	fields := form.Get("elements")
	kind, model, prompt := fields.Call("namedItem", "kind"), fields.Call("namedItem", "model"), fields.Call("namedItem", "prompt")
	previous := query(form, "[data-default-image]").Get("value").String()
	sync := func(js.Value) {
		options := model.Get("options")
		first, selectedValid := -1, false
		for i := 0; i < options.Length(); i++ {
			option := options.Index(i)
			hidden := attr(option, "data-kind") != kind.Get("value").String()
			option.Set("hidden", hidden)
			if !hidden && first == -1 {
				first = i
			}
			if !hidden && option.Get("selected").Bool() {
				selectedValid = true
			}
		}
		if !selectedValid && first >= 0 {
			model.Set("value", options.Index(first).Get("value"))
		}
		selector := "[data-default-image]"
		if kind.Get("value").String() == "video" {
			selector = "[data-default-video]"
		}
		next := query(form, selector).Get("value").String()
		if value := prompt.Get("value").String(); value == "" || value == previous {
			prompt.Set("value", next)
		}
		previous = next
		each(form, `input[name="reference_ids"]`, func(input js.Value) { input.Set("disabled", kind.Get("value").String() == "video") })
	}
	ui.on(kind, "change", sync)
	sync(js.Undefined())
}
