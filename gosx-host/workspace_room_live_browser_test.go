//go:build browser

package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

func TestWorkspaceGoClientRoomLiveBoard(t *testing.T) {
	store := testStore(t)
	shots := []ProjectDocument{
		{"title": rawValue("Platform wide"), "scene": rawValue("01 · Last train"),
			"purpose": rawValue("Establish Mara alone beneath the departure board."), "blocking": rawValue("terminal")},
		{"title": rawValue("The departure board"), "scene": rawValue("01 · Last train"),
			"purpose": rawValue("Hold on the delayed service as the station empties."), "blocking": rawValue("sign")},
		{"title": rawValue("Mara turns"), "scene": rawValue("01 · Last train"),
			"purpose": rawValue("A sound from the footbridge interrupts her wait."), "blocking": rawValue("reaction")},
		{"title": rawValue("Across the platform"), "scene": rawValue("02 · The crossing"),
			"purpose": rawValue("Stage the crossing so her eyeline carries into the next cut."), "blocking": rawValue("walkway")},
		{"title": rawValue("At the gate"), "scene": rawValue("02 · The crossing"),
			"purpose": rawValue("Let the gate and the approaching figure share the frame."), "blocking": rawValue("gate")},
		{"title": rawValue("A quiet recognition"), "scene": rawValue("02 · The crossing"),
			"purpose": rawValue("Hold the two figures apart before either speaks."), "blocking": rawValue("passenger")},
	}
	project, err := store.CreateTemplate(ProjectDocument{"title": rawValue("Night Shift"), "brief": rawValue("Sequence 04 · Station exterior · Night"),
		"aspect": rawValue("16:9"), "shots": rawValue(shots)})
	if err != nil {
		t.Fatal(err)
	}
	id := shotText(project, "id")
	stored := list(project, "shots")
	first, second, third := shotText(stored[0], "id"), shotText(stored[1], "id"), shotText(stored[2], "id")
	server := httptest.NewUnstartedServer(nil)
	origin := &url.URL{Scheme: "http", Host: server.Listener.Addr().String()}
	room, err := newStoryboardRoom(store, id, origin)
	if err != nil {
		t.Fatal(err)
	}
	var failBoard atomic.Bool
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/room/board" && failBoard.Swap(false) {
			http.Error(w, "Synthetic update interruption", http.StatusServiceUnavailable)
			return
		}
		room.ServeHTTP(w, r)
	})
	server.Start()
	t.Cleanup(server.Close)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := room.hub.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	ctx := editorBrowserContext(t)
	run := func(actions ...chromedp.Action) {
		t.Helper()
		if err := chromedp.Run(ctx, actions...); err != nil {
			t.Fatal(err)
		}
	}
	run(chromedp.Navigate(server.URL), chromedp.SendKeys(`input[name="invite"]`, room.invite, chromedp.ByQuery))
	if _, err := chromedp.RunResponse(ctx, chromedp.Click(`form[action="/join"] button`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	run(browserReady(), browserText("#room-presence", "1 collaborator online"), chromedp.Evaluate(`window.roomDocumentProof = "same document"`, nil))
	publish := func(shotID, title string) int {
		t.Helper()
		p, err := store.Load(id)
		if err != nil {
			t.Fatal(err)
		}
		p, err = store.UpdateShot(id, shotID, documentRevision(p), ProjectDocument{"title": rawValue(title)})
		if err != nil {
			t.Fatal(err)
		}
		revision := documentRevision(p)
		room.publish(revision)
		return revision
	}
	waitRevision := func(revision int) {
		t.Helper()
		run(chromedp.Poll(fmt.Sprintf(`document.querySelector("[data-room-events]").dataset.revision === "%d"`, revision), nil))
	}
	capture := func(name string) {
		t.Helper()
		folder := os.Getenv("STORYBOARD_ROOM_PROOF_DIR")
		if folder == "" {
			return
		}
		var png []byte
		run(chromedp.CaptureScreenshot(&png))
		if err := os.WriteFile(filepath.Join(folder, name+".png"), png, 0644); err != nil {
			t.Fatal(err)
		}
	}

	secondButton := `.shot-card[data-shot-id="` + second + `"] button[title="Move earlier"]`
	run(chromedp.Focus(secondButton, chromedp.ByQuery))
	revision := publish(second, "Departure board · hold for the reveal")
	waitRevision(revision)
	run(browserText(`.shot-card[data-shot-id="`+second+`"] h2`, "Departure board · hold for the reveal"))
	var focused, sameDocument bool
	run(chromedp.Evaluate(fmt.Sprintf(`document.activeElement.matches(%q)`, secondButton), &focused),
		chromedp.Evaluate(`window.roomDocumentProof === "same document"`, &sameDocument))
	if !focused || !sameDocument {
		t.Fatalf("live card update lost keyboard focus or replaced the document: focus=%v document=%v", focused, sameDocument)
	}
	capture("room-live-desktop")

	p, err := store.Insert(id, second, "after", revision)
	if err != nil {
		t.Fatal(err)
	}
	inserted := shotText(list(p, "shots")[2], "id")
	p, err = store.UpdateShot(id, inserted, documentRevision(p), ProjectDocument{
		"title": rawValue("Footbridge detail"), "scene": rawValue("01 · Last train"),
		"purpose": rawValue("Carry the sound across the empty footbridge."), "blocking": rawValue("walkway"),
	})
	if err != nil {
		t.Fatal(err)
	}
	revision = documentRevision(p)
	room.publish(revision) // One snapshot must include both intervening saves.
	waitRevision(revision)
	run(browserText("#room-shot-count", "7 shots"))
	order := make([]string, 0, 7)
	for _, shot := range list(p, "shots") {
		order = append(order, shotText(shot, "id"))
	}
	order[0], order[1] = order[1], order[0]
	p, err = store.Reorder(id, revision, order)
	if err != nil {
		t.Fatal(err)
	}
	revision = documentRevision(p)
	room.publish(revision)
	waitRevision(revision)
	var displayedOrder []string
	run(chromedp.Evaluate(`Array.from(document.querySelectorAll(".shot-card"), card => card.dataset.shotId)`, &displayedOrder),
		chromedp.Evaluate(fmt.Sprintf(`document.activeElement.matches(%q)`, secondButton), &focused))
	if !slices.Equal(displayedOrder, order) || !focused {
		t.Fatal("a live insertion or reorder lost shot identity, order or keyboard focus")
	}

	const titleField = `form[action="/save"] input[name="title"]`
	run(chromedp.Focus(titleField, chromedp.ByQuery), chromedp.Evaluate(`document.querySelector('form[action="/save"] input[name="title"]').setSelectionRange(3, 3)`, nil))
	beforeDraft := revision
	revision = publish(first, "Platform wide · teammate's revision")
	waitRevision(revision)
	var value, formRevision string
	var caret int
	run(chromedp.Value(titleField, &value, chromedp.ByQuery),
		chromedp.Value(`form[action="/save"] input[name="revision"]`, &formRevision, chromedp.ByQuery),
		chromedp.Evaluate(`document.activeElement.selectionStart`, &caret),
		chromedp.WaitVisible("#room-shot-update", chromedp.ByQuery))
	if value != "Platform wide" || formRevision != strconv.Itoa(beforeDraft) || caret != 3 {
		t.Fatalf("peer save changed an active brief: value=%q revision=%q caret=%d", value, formRevision, caret)
	}
	run(chromedp.SendKeys(titleField, kb.End+" — hold the empty frame", chromedp.ByQuery), browserText("#save-status", "Unsaved changes"))
	capture("room-draft-desktop")
	run(chromedp.EmulateViewport(390, 844), chromedp.Evaluate("window.scrollTo(0, 0)", nil))
	var overflow bool
	run(chromedp.Evaluate("document.documentElement.scrollWidth > innerWidth", &overflow))
	if overflow {
		t.Fatal("shared board overflows the narrow viewport")
	}
	capture("room-live-mobile")
	run(chromedp.Click(`.room-status a[href="#shot-editor"]`, chromedp.ByQuery))
	capture("room-brief-mobile")

	failBoard.Store(true)
	revision = publish(third, "Mara turns · revised eyeline")
	run(chromedp.WaitVisible("#room-sync-error", chromedp.ByQuery))
	clients := room.hub.Presence().List()
	for _, client := range clients {
		room.hub.Disconnect(client.ID, "synthetic reconnect")
	}
	waitRevision(revision)
	run(chromedp.WaitNotVisible("#room-sync-error", chromedp.ByQuery),
		chromedp.Poll(fmt.Sprintf(`document.querySelector('.shot-card[data-shot-id="%s"] h2').textContent === "Mara turns · revised eyeline"`, third), nil),
		chromedp.Value(titleField, &value, chromedp.ByQuery), chromedp.Evaluate(`window.roomDocumentProof === "same document"`, &sameDocument))
	if value != "Platform wide — hold the empty frame" || !sameDocument {
		t.Fatalf("interrupted update or reconnect changed the draft: value=%q document=%v", value, sameDocument)
	}
	t.Log("Live replacement, insertion and reorder preserved shot identities and keyboard focus, including a skipped intermediate revision. A peer save preserved the active shot, concurrency token and caret. An interrupted board request recovered after reconnect without navigation or draft loss. The narrow board had no horizontal overflow.")
}
