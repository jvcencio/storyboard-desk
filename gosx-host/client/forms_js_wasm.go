//go:build js && wasm

package main

import (
	"context"
	"strings"
	"syscall/js"
)

type formState struct {
	*eventScope
	doc   js.Value
	dirty []js.Value
}

func mountFormState(parent context.Context, doc js.Value) *formState {
	forms := &formState{eventScope: newEventScope(parent), doc: doc}
	each(doc, `form[data-rejected-draft="true"]`, forms.markDirty)
	for _, event := range []string{"input", "change"} {
		forms.on(doc, event, func(e js.Value) { forms.markDirty(closest(e.Get("target"), "form[data-dirty]")) })
	}
	forms.on(doc, "submit", forms.submit)
	forms.on(doc, "click", forms.click)
	forms.on(js.Global(), "beforeunload", func(e js.Value) {
		if len(forms.dirty) > 0 {
			e.Call("preventDefault")
			e.Set("returnValue", "")
		}
	})
	return forms
}

func (forms *formState) markDirty(form js.Value) {
	if !form.Truthy() {
		return
	}
	for _, known := range forms.dirty {
		if known.Equal(form) {
			return
		}
	}
	forms.dirty = append(forms.dirty, form)
	setText(forms.doc.Call("getElementById", "save-status"), "Unsaved changes")
}

func (forms *formState) protects(root js.Value) bool {
	for _, form := range forms.dirty {
		if root.Call("contains", form).Bool() {
			return true
		}
	}
	return false
}

func (forms *formState) submit(e js.Value) {
	form := e.Get("target")
	if form.Call("hasAttribute", "data-download-form").Bool() {
		return // Downloads are owned by the capability adapter and keep drafts.
	}
	for _, other := range forms.dirty {
		if !other.Equal(form) && !js.Global().Call("confirm", "Other forms have unsaved changes. Continue and discard those edits?").Bool() {
			e.Call("preventDefault")
			return
		}
		if !other.Equal(form) {
			break
		}
	}
	forms.dirty = nil
}

func (forms *formState) click(e js.Value) {
	anchor := closest(e.Get("target"), "a[href]")
	if !anchor.Truthy() || e.Get("ctrlKey").Bool() || e.Get("metaKey").Bool() || e.Get("button").Int() > 0 || anchor.Call("hasAttribute", "download").Bool() {
		return
	}
	href := attr(anchor, "href")
	if href == "#new-project" {
		form := forms.doc.Call("getElementById", "new-project")
		form.Set("open", true)
		query(form, `input[name="title"]`).Call("focus")
		return
	}
	if len(forms.dirty) > 0 && !strings.HasPrefix(href, "#") {
		if !js.Global().Call("confirm", "Discard unsaved edits and leave this view?").Bool() {
			e.Call("preventDefault")
		} else {
			forms.dirty = nil
		}
	}
}
