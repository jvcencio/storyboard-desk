//go:build js && wasm

package main

import (
	"net/url"
	"strconv"
	"syscall/js"

	"m31labs.dev/gosx/client/browser"
)

// Room updates reconcile server-rendered cards. The editor and its original
// concurrency token stay owned by the person working in the shot brief.
type roomBoard struct {
	*eventScope
	doc, root     js.Value
	forms         *formState
	shown, latest int
	refreshing    bool
}

func (board *roomBoard) refresh() {
	if board.disposed || board.refreshing || board.latest <= board.shown {
		return
	}
	current := query(board.doc, "[data-room-board]")
	if !current.Truthy() {
		return
	}
	values := url.Values{"shot": {attr(current, "data-selected-shot")}, "scene": {attr(current, "data-scene")}}
	board.refreshing = true
	go func() {
		progress := false
		defer func() {
			board.refreshing = false
			if progress {
				board.refresh() // A newer save may have arrived during this request.
			}
		}()
		response, err := browser.Fetch(board.ctx, browser.FetchRequest{URL: "/room/board?" + values.Encode()})
		if board.disposed || !current.Get("isConnected").Bool() {
			return
		}
		failure := board.doc.Call("getElementById", "room-sync-error")
		if err != nil || response.Status != 200 {
			failure.Set("hidden", false)
			return
		}
		template := board.doc.Call("createElement", "template")
		template.Set("innerHTML", string(response.Body))
		next := query(template.Get("content"), "[data-room-board]")
		revision, err := strconv.Atoi(attr(next, "data-revision"))
		if err != nil || revision < board.shown || attr(next, "data-selected-shot") != values.Get("shot") {
			failure.Set("hidden", false)
			return
		}
		previous := board.shown
		board.reconcile(current, next)
		board.shown = revision
		board.root.Call("setAttribute", "data-revision", strconv.Itoa(revision))
		current.Call("setAttribute", "data-revision", strconv.Itoa(revision))
		failure.Set("hidden", true)
		progress = revision > previous
	}()
}

func (board *roomBoard) reconcile(current, next js.Value) {
	active := board.doc.Get("activeElement")
	board.reconcileCards(query(current, ".shot-grid"), query(next, ".shot-grid"), active)
	oldMove, newMove := query(current, "#board-move"), query(next, "#board-move")
	if oldMove.Truthy() {
		oldMove.Call("remove")
	}
	if newMove.Truthy() {
		query(current, ".board-layout").Call("before", newMove)
	}
	oldFilter, newFilter := query(current, ".scene-filter select"), query(next, ".scene-filter select")
	if !oldFilter.Equal(active) && !oldFilter.Call("isEqualNode", newFilter).Bool() {
		oldFilter.Call("replaceWith", newFilter)
	}
	board.reconcileEditor(query(current, "#shot-editor"), query(next, "#shot-editor"), active, attr(next, "data-revision"))
	setText(board.doc.Call("getElementById", "room-shot-count"), attr(next, "data-shot-count")+" shots")
}

func (board *roomBoard) reconcileCards(current, next, active js.Value) {
	cards := map[string]js.Value{}
	each(current, ".shot-card", func(card js.Value) { cards[attr(card, "data-shot-id")] = card })
	focusedCard := closest(active, ".shot-card")
	focusedID, controlIndex := attr(focusedCard, "data-shot-id"), -1
	if focusedCard.Truthy() {
		controls := focusedCard.Call("querySelectorAll", "a,button")
		for i := 0; i < controls.Length(); i++ {
			if controls.Index(i).Equal(active) {
				controlIndex = i
			}
		}
	}
	var focus js.Value
	incoming := next.Call("querySelectorAll", ".shot-card")
	for i := 0; i < incoming.Length(); i++ {
		card := incoming.Index(i)
		id := attr(card, "data-shot-id")
		old, exists := cards[id]
		if exists && (old.Call("isEqualNode", card).Bool() || board.forms.protects(old)) {
			card = old
		}
		children := current.Get("children")
		if i >= children.Length() {
			current.Call("appendChild", card)
		} else if !card.Equal(children.Index(i)) {
			current.Call("insertBefore", card, children.Index(i))
		}
		if exists && !old.Equal(card) {
			old.Call("remove")
		}
		if id == focusedID && controlIndex >= 0 {
			controls := card.Call("querySelectorAll", "a,button")
			if controlIndex < controls.Length() {
				focus = controls.Index(controlIndex)
			}
		}
		delete(cards, id)
	}
	for _, card := range cards {
		card.Call("remove")
	}
	if focus.Truthy() && !focus.Equal(board.doc.Get("activeElement")) {
		focus.Call("focus", map[string]any{"preventScroll": true})
	}
}

func (board *roomBoard) reconcileEditor(current, next, active js.Value, revision string) {
	oldBase, newBase := query(current, `input[name="shot_base"]`), query(next, `input[name="shot_base"]`)
	if !oldBase.Truthy() || !newBase.Truthy() {
		return
	}
	if oldBase.Get("value").String() == newBase.Get("value").String() {
		each(current, "form", func(form js.Value) {
			if !board.forms.protects(form) {
				each(form, `input[name="revision"]`, func(input js.Value) { input.Call("setAttribute", "value", revision) })
			}
		})
		return
	}
	if board.forms.protects(current) || current.Call("contains", active).Bool() {
		query(current, "#room-shot-update").Set("hidden", false)
		return
	}
	scroll := current.Get("scrollTop")
	current.Call("replaceWith", next)
	next.Set("scrollTop", scroll)
}
