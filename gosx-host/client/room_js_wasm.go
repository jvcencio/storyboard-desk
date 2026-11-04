//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"syscall/js"
	"time"

	"m31labs.dev/gosx/engine/wasm"
	"m31labs.dev/gosx/hub/socket"
)

func mountRoom(parent context.Context, root js.Value) wasm.Handle {
	scope := newEventScope(parent)
	status := query(root, "#room-presence")
	shown, _ := strconv.Atoi(attr(root, "data-revision"))
	location := js.Global().Get("location")
	scheme := "ws://"
	if location.Get("protocol").String() == "https:" {
		scheme = "wss://"
	}
	address := scheme + location.Get("host").String() + attr(root, "data-room-events")
	var current *socket.Socket
	var timer *time.Timer
	var connect func(js.Value)
	delay := time.Second
	retry := func() {
		setText(status, "Reconnecting to room…")
		var pending *time.Timer
		pending = time.AfterFunc(delay, func() {
			if timer == pending {
				timer = nil
				connect(js.Undefined())
			}
		})
		timer = pending
		delay = min(2*delay, 8*time.Second)
	}
	connect = func(js.Value) {
		if current != nil || timer != nil || scope.disposed {
			return
		}
		var err error
		current, err = socket.Dial(address, socket.Callbacks{
			Message: func(message socket.Message) {
				var update struct {
					Event string `json:"event"`
					Data  struct {
						Revision  int `json:"revision"`
						Connected int `json:"connected"`
					} `json:"data"`
				}
				if message.IsBinary || json.Unmarshal([]byte(message.Text), &update) != nil || update.Event != "board" {
					return
				}
				delay = time.Second
				text := fmt.Sprintf("%d connected · revision %d", update.Data.Connected, update.Data.Revision)
				if update.Data.Revision > shown {
					text += " · new changes available"
				}
				setText(status, text)
			},
			Closed: func(socket.CloseEvent) {
				current = nil
				retry()
			},
		})
		if err != nil {
			retry()
		}
	}
	disconnect := func(js.Value) {
		if timer != nil {
			timer.Stop()
			timer = nil
		}
		if client := current; client != nil {
			current = nil
			client.Dispose()
		}
	}
	scope.on(js.Global(), "pagehide", disconnect)
	scope.on(js.Global(), "pageshow", connect)
	connect(js.Undefined())
	return wasm.HandleFunc(func() {
		scope.Dispose()
		disconnect(js.Undefined())
	})
}
