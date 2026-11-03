//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"syscall/js"

	"m31labs.dev/gosx/client/browser"
	"m31labs.dev/gosx/client/jsutil"
)

// File APIs, clipboard, metadata and native saving stay in the unrestricted
// Go-WASM adapter. They do not own dirty forms or review playback.
type capabilities struct {
	*eventScope
	doc    js.Value
	saving bool
}

func mountCapabilities(parent context.Context, doc js.Value) *capabilities {
	ui := &capabilities{eventScope: newEventScope(parent), doc: doc}
	ui.on(doc, "submit", func(e js.Value) {
		form := e.Get("target")
		if form.Call("hasAttribute", "data-download-form").Bool() && attr(doc.Get("body"), "data-native-save") == "true" {
			e.Call("preventDefault")
			ui.saveNative(attr(form, "action"), formValues(form))
		}
	})
	ui.on(doc, "click", ui.click)
	each(doc, "[data-json-file]", func(input js.Value) { ui.on(input, "change", func(js.Value) { ui.loadJSON(input) }) })
	each(doc, "[data-asset] video", func(video js.Value) {
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
	return ui
}

func (ui *capabilities) click(e js.Value) {
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
	if anchor.Call("hasAttribute", "download").Bool() && attr(ui.doc.Get("body"), "data-native-save") == "true" {
		if values, ok := ui.downloadValues(attr(anchor, "href")); ok {
			e.Call("preventDefault")
			ui.saveNative("/download", values)
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

func (ui *capabilities) downloadValues(href string) (url.Values, bool) {
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

func (ui *capabilities) saveNative(action string, values url.Values) {
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

func (ui *capabilities) loadJSON(input js.Value) {
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
