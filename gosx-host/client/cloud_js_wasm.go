//go:build js && wasm

package main

import (
	"context"
	"syscall/js"
)

func mountCloudControls(parent context.Context, form js.Value) *eventScope {
	scope := newEventScope(parent)
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
	scope.on(kind, "change", sync)
	sync(js.Undefined())
	return scope
}
