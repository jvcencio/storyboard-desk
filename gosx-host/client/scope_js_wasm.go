//go:build js && wasm

package main

import (
	"context"
	"syscall/js"
)

type listener struct {
	target   js.Value
	event    string
	callback js.Func
}

// eventScope gives every controller the same idempotent cleanup contract.
// Disposing cancels pending promises and releases only this owner's listeners.
type eventScope struct {
	ctx       context.Context
	cancel    context.CancelFunc
	listeners []listener
	disposed  bool
}

func newEventScope(parent context.Context) *eventScope {
	ctx, cancel := context.WithCancel(parent)
	return &eventScope{ctx: ctx, cancel: cancel}
}

func (scope *eventScope) on(target js.Value, event string, fn func(js.Value)) {
	if !target.Truthy() {
		return
	}
	callback := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if !scope.disposed && len(args) > 0 {
			fn(args[0])
		}
		return nil
	})
	target.Call("addEventListener", event, callback)
	scope.listeners = append(scope.listeners, listener{target, event, callback})
}

func (scope *eventScope) Dispose() {
	if scope.disposed {
		return
	}
	scope.disposed = true
	scope.cancel()
	for _, item := range scope.listeners {
		item.target.Call("removeEventListener", item.event, item.callback)
		item.callback.Release()
	}
	scope.listeners = nil
}
