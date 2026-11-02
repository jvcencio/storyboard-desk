//go:build js && wasm

package main

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"syscall/js"

	"m31labs.dev/gosx/client/browser"
	"m31labs.dev/gosx/client/jsutil"
	"m31labs.dev/gosx/engine/wasm"
)

type reviewPlayer struct {
	ui                    *workspaceClient
	shots                 []js.Value
	audio, status, button js.Value
	index                 int
	playing               bool
	version               int
	started, elapsed      float64
	stopFrames            context.CancelFunc
}

func dataNumber(node js.Value, name string) float64 {
	n, err := strconv.ParseFloat(attr(node, "data-"+name), 64)
	if err != nil || !finite(n) {
		return 0
	}
	return n
}

func (ui *workspaceClient) review(root js.Value) {
	p := &reviewPlayer{ui: ui, audio: query(root, "[data-review-audio]"), status: query(root, "[data-review-status]"), button: query(root, "[data-review-play]")}
	each(root, "[data-review-shot]", func(shot js.Value) {
		p.shots = append(p.shots, shot)
		video := query(shot, "video")
		if !video.Truthy() {
			return
		}
		video.Set("muted", true)
		metadata := func(js.Value) {
			duration := video.Get("duration").Float()
			if dataNumber(shot, "available") <= 0 && finite(duration) {
				shot.Call("setAttribute", "data-available", fmt.Sprint(math.Max(0, duration-dataNumber(shot, "start"))))
			}
			if len(p.shots) > p.index && p.shots[p.index].Equal(shot) && !p.playing {
				seek(video, dataNumber(shot, "start"))
			}
		}
		ui.on(video, "loadedmetadata", metadata)
		if video.Get("readyState").Int() > 0 {
			metadata(js.Undefined())
		}
	})
	ui.on(p.button, "click", func(js.Value) {
		if p.playing {
			p.pause()
		} else {
			p.play()
		}
	})
	ui.on(query(root, "[data-review-prev]"), "click", func(js.Value) { p.index = max(0, p.index-1); p.show() })
	ui.on(query(root, "[data-review-next]"), "click", func(js.Value) { p.index = max(0, min(len(p.shots)-1, p.index+1)); p.show() })
	ui.on(js.Global(), "pagehide", func(js.Value) { p.pause() })
	ui.players = append(ui.players, p)
	p.show()
}

func seek(media js.Value, seconds float64) {
	if !media.Truthy() {
		return
	}
	// Some WebKit versions defer seeking until metadata is available. The
	// loadedmetadata listener repeats the seek before playback starts.
	defer func() { _ = recover() }()
	media.Set("currentTime", seconds)
}

func (p *reviewPlayer) pause() {
	p.playing = false
	p.version++
	if p.stopFrames != nil {
		p.stopFrames()
		p.stopFrames = nil
	}
	for _, shot := range p.shots {
		if video := query(shot, "video"); video.Truthy() {
			video.Call("pause")
		}
	}
	if p.audio.Truthy() {
		p.audio.Call("pause")
	}
	p.button.Call("setAttribute", "aria-pressed", "false")
}

func (p *reviewPlayer) show() {
	p.pause()
	p.elapsed = 0
	for i, shot := range p.shots {
		shot.Set("hidden", i != p.index)
	}
	if len(p.shots) == 0 {
		setText(p.status, "Add a shot to review the sequence.")
		return
	}
	shot := p.shots[p.index]
	seek(query(shot, "video"), dataNumber(shot, "start"))
	seek(p.audio, dataNumber(shot, "song-start"))
	setText(p.status, fmt.Sprintf("Shot %d of %d", p.index+1, len(p.shots)))
}

func (p *reviewPlayer) play() {
	if p.ui.disposed || len(p.shots) == 0 {
		return
	}
	shot := p.shots[p.index]
	if attr(shot, "data-kind") == "gap" {
		setText(p.status, "Choose a take for this shot before continuing.")
		return
	}
	p.playing = true
	p.button.Call("setAttribute", "aria-pressed", "true")
	version := p.version
	video := query(shot, "video")
	videoPromise := js.Undefined()
	// Invoke play synchronously within the user's gesture. Awaiting belongs in
	// a goroutine so the browser can continue delivering input and media events.
	if video.Truthy() {
		videoPromise = video.Call("play")
	}
	if p.audio.Truthy() {
		audioPromise := p.audio.Call("play")
		go func() { _, _ = jsutil.AwaitPromiseContext(p.ui.ctx, audioPromise) }()
	}
	go func() {
		if videoPromise.Truthy() {
			if _, err := jsutil.AwaitPromiseContext(p.ui.ctx, videoPromise); err != nil {
				if p.version == version {
					p.pause()
					setText(p.status, "This source could not play. Review it in the Media Library.")
				}
				return
			}
		}
		if p.ui.disposed || p.version != version || !p.playing {
			return
		}
		p.started = browser.Now() - p.elapsed*1000
		ctx, cancel := context.WithCancel(p.ui.ctx)
		p.stopFrames = cancel
		defer cancel()
		_ = wasm.EachFrame(ctx, func(frame wasm.Frame) error {
			if !p.playing || p.version != version {
				return wasm.StopFrames
			}
			p.elapsed = (frame.Time - p.started) / 1000
			target, available := dataNumber(shot, "duration"), dataNumber(shot, "available")
			if p.elapsed < math.Min(target, available) {
				return nil
			}
			if available+.05 < target {
				p.pause()
				setText(p.status, fmt.Sprintf("This take leaves %.2f seconds of missing coverage. Choose another range or advance manually.", target-available))
			} else if p.index+1 < len(p.shots) {
				p.index++
				p.show()
				p.play()
			} else {
				p.pause()
				setText(p.status, "Sequence complete")
			}
			return wasm.StopFrames
		})
	}()
}
