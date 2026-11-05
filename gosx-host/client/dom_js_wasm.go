//go:build js && wasm

package main

import (
	"math"
	"syscall/js"
)

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
