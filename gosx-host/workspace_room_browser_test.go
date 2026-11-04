//go:build browser

package main

import (
	"context"
	"fmt"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

func TestWorkspaceGoClientSharedStoryboard(t *testing.T) {
	store := testStore(t)
	project := createTestProject(t, store)
	id := shotText(project, "id")
	first := shotText(list(project, "shots")[0], "id")
	project, err := store.Insert(id, first, "after", documentRevision(project))
	if err != nil {
		t.Fatal(err)
	}
	second := shotText(list(project, "shots")[1], "id")
	base := documentRevision(project)
	backend := httptest.NewUnstartedServer(nil)
	target := &url.URL{Scheme: "http", Host: backend.Listener.Addr().String()}
	server := httptest.NewTLSServer(httputil.NewSingleHostReverseProxy(target))
	t.Cleanup(server.Close)
	origin, err := parseShareOrigin(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	room, err := newStoryboardRoom(store, id, origin)
	if err != nil {
		t.Fatal(err)
	}
	backend.Config.Handler = room
	backend.Start()
	t.Cleanup(backend.Close)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := room.hub.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	// Both browser profiles connect through the synthetic proxy's test certificate.
	testCertificate := chromedp.Flag("ignore-certificate-errors", true)
	a, b := editorBrowserContext(t, testCertificate), editorBrowserContext(t, testCertificate)
	run := func(ctx context.Context, actions ...chromedp.Action) {
		t.Helper()
		if err := chromedp.Run(ctx, actions...); err != nil {
			t.Fatal(err)
		}
	}
	join := func(ctx context.Context, shot string) {
		t.Helper()
		run(ctx, chromedp.Navigate(server.URL), chromedp.SendKeys(`input[name="invite"]`, room.invite, chromedp.ByQuery))
		if _, err := chromedp.RunResponse(ctx, chromedp.Click(`form[action="/join"] button`, chromedp.ByQuery)); err != nil {
			t.Fatal(err)
		}
		run(ctx, browserReady(), chromedp.Navigate(server.URL+pageURL(id, "board", shot, "")), browserReady())
	}
	join(a, first)
	join(b, second)
	presence := func(revision int, newer bool) chromedp.Action {
		message := fmt.Sprintf("2 connected · revision %d", revision)
		if newer {
			message += " · new changes available"
		}
		return browserText("#room-presence", message)
	}
	run(a, presence(base, false))
	run(b, presence(base, false))
	const title = `form[action="/save"] input[name="title"]`
	const save = `form[action="/save"] button`
	edit := func(ctx context.Context, text string) {
		t.Helper()
		run(ctx, chromedp.Clear(title, chromedp.ByQuery), chromedp.SendKeys(title, text, chromedp.ByQuery),
			browserText("#save-status", "Unsaved changes"))
	}
	submit := func(ctx context.Context, want int64) {
		t.Helper()
		response, err := chromedp.RunResponse(ctx, chromedp.Click(save, chromedp.ByQuery))
		if err != nil || response == nil || response.Status != want {
			t.Fatalf("save result: %+v %v", response, err)
		}
		run(ctx, browserReady())
	}
	edit(b, "Second collaborator · 雪")
	edit(a, "Opening by the first collaborator")
	submit(a, 200)
	run(b, presence(base+1, true))
	var draft, revision string
	run(b, chromedp.Value(title, &draft, chromedp.ByQuery), chromedp.Value(`form[action="/save"] input[name="revision"]`, &revision, chromedp.ByQuery))
	if draft != "Second collaborator · 雪" || revision != strconv.Itoa(base) {
		t.Fatalf("remote update changed local draft %q or revision %q", draft, revision)
	}
	if screenshotPath := os.Getenv("STORYBOARD_ROOM_SCREENSHOT"); screenshotPath != "" {
		var screenshot []byte
		run(b, chromedp.CaptureScreenshot(&screenshot))
		if err := os.WriteFile(screenshotPath, screenshot, 0644); err != nil {
			t.Fatal(err)
		}
	}
	submit(b, 200)
	run(a, presence(base+2, true))

	run(b, chromedp.Navigate(server.URL+pageURL(id, "board", first, "")), browserReady())
	edit(b, "Shared opening, latest save")
	submit(b, 200)
	run(a, presence(base+3, true))
	edit(a, "Keep this conflicting draft")
	submit(a, 409)
	run(a, browserText("#save-status", "Unsaved changes"), chromedp.Value(title, &draft, chromedp.ByQuery),
		chromedp.Value(`form[action="/save"] input[name="revision"]`, &revision, chromedp.ByQuery))
	if draft != "Keep this conflicting draft" || revision != strconv.Itoa(base+1) {
		t.Fatalf("conflict changed draft %q or revision %q", draft, revision)
	}
	cancelled := make(chan error, 1)
	chromedp.ListenTarget(a, func(event any) {
		if _, ok := event.(*page.EventJavascriptDialogOpening); ok {
			go func() { cancelled <- chromedp.Run(a, page.HandleJavaScriptDialog(false)) }()
		}
	})
	run(a, chromedp.Click(".room-status a", chromedp.ByQuery))
	select {
	case err := <-cancelled:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("opening the latest board discarded the conflicting draft")
	}
	submit(a, 409)
	run(a, presence(base+3, false))
	previous := map[string]bool{}
	for _, client := range room.hub.Presence().List() {
		previous[client.ID] = true
		room.hub.Disconnect(client.ID, "verification")
	}
	deadline := time.Now().Add(8 * time.Second)
	for {
		clients := room.hub.Presence().List()
		if len(clients) == 2 && !previous[clients[0].ID] && !previous[clients[1].ID] {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("browser sessions did not reconnect")
		}
		time.Sleep(30 * time.Millisecond)
	}
	run(a, presence(base+3, false), chromedp.Value(title, &draft, chromedp.ByQuery))
	if draft != "Keep this conflicting draft" {
		t.Fatal("reconnect lost the draft")
	}
	project, err = store.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	shots := list(project, "shots")
	if documentRevision(project) != base+3 || shotText(shots[0], "title") != "Shared opening, latest save" ||
		shotText(shots[1], "title") != "Second collaborator · 雪" {
		t.Fatal("saved shots or revision did not survive concurrent editing")
	}
	t.Log("Independent sessions used HTTPS and WSS through a loopback proxy, saved different shots from the same revision, retained a conflicting draft on rejection and reconnect, and left the committed revision at three successful changes.")
}
