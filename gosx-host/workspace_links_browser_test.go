//go:build browser

package main

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// A link to the page that is already open only scrolls, so it must neither ask
// about unsaved edits nor stop guarding them. A link marked data-reload, such
// as the room's "Review latest shot", must load the saved version instead.
func TestWorkspaceGoClientSamePageLinks(t *testing.T) {
	s := testStore(t)
	p := createTestProject(t, s)
	id, sid := shotText(p, "id"), shotText(list(p, "shots")[0], "id")
	saved := shotText(list(p, "shots")[0], "title")
	ctx, origin := editorBrowser(t, &workspaceHandler{store: s, csrf: "browser-token"})
	const selector = `form[action="/save"] input[name="title"]`
	dialogs := make(chan string, 8)
	var accept atomic.Bool
	chromedp.ListenTarget(ctx, func(event any) {
		if dialog, ok := event.(*page.EventJavascriptDialogOpening); ok {
			dialogs <- dialog.Message
			go chromedp.Run(ctx, page.HandleJavaScriptDialog(accept.Load()))
		}
	})
	run := func(name string, actions ...chromedp.Action) {
		t.Helper()
		if err := chromedp.Run(ctx, actions...); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	asked := func(wait time.Duration) string {
		select {
		case message := <-dialogs:
			return message
		case <-time.After(wait):
			return ""
		}
	}
	run("open and edit", chromedp.Navigate(origin+pageURL(id, "board", sid, "")), browserReady(),
		chromedp.Clear(selector, chromedp.ByQuery), chromedp.SendKeys(selector, "Unsaved draft", chromedp.ByQuery), browserText("#save-status", "Unsaved changes"),
		chromedp.Evaluate(`window.proof = 1;
			for (const reload of [false, true]) {
				const link = document.createElement('a');
				link.id = reload ? 'reload-link' : 'scroll-link';
				link.href = location.pathname + location.search + '#shot-editor';
				if (reload) link.setAttribute('data-reload', 'true');
				link.textContent = link.id;
				document.body.append(link);
			}
			const away = document.createElement('a');
			away.id = 'away-link';
			away.href = location.pathname + '?project=' + new URLSearchParams(location.search).get('project') + '&view=media';
			away.textContent = 'away';
			document.body.append(away);`, nil))

	var title string
	var proof int
	run("scroll link", chromedp.Evaluate(`setTimeout(() => document.getElementById('scroll-link').click(), 0)`, nil))
	if message := asked(500 * time.Millisecond); message != "" {
		t.Fatalf("a link that only scrolls asked: %q", message)
	}
	run("after scroll", chromedp.Value(selector, &title, chromedp.ByQuery), chromedp.Evaluate(`window.proof`, &proof), browserText("#save-status", "Unsaved changes"))
	if title != "Unsaved draft" || proof != 1 {
		t.Fatalf("the scroll link changed the page: title %q, proof %d", title, proof)
	}

	// The draft is still guarded: leaving asks, and declining keeps the page.
	run("away link", chromedp.Evaluate(`setTimeout(() => document.getElementById('away-link').click(), 0)`, nil))
	if message := asked(5 * time.Second); message == "" {
		t.Fatal("the scroll link released the draft guard: leaving no longer asks")
	}
	run("after declining", chromedp.Value(selector, &title, chromedp.ByQuery), chromedp.Evaluate(`window.proof`, &proof))
	if title != "Unsaved draft" || proof != 1 {
		t.Fatalf("declining left the page: title %q, proof %d", title, proof)
	}

	// The reload link asks once, then loads the saved version of this page.
	accept.Store(true)
	run("reload link", chromedp.Evaluate(`setTimeout(() => document.getElementById('reload-link').click(), 0)`, nil))
	if message := asked(5 * time.Second); message == "" {
		t.Fatal("reloading over a draft did not ask")
	}
	// The page is replaced under the test, so a check may land on a context
	// that is being torn down. Ask again until the new page answers.
	for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		var reloaded bool
		err := chromedp.Run(ctx, chromedp.Evaluate(`typeof window.proof === 'undefined' && document.body.dataset.editorReady === 'true'`, &reloaded))
		if err == nil && reloaded {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the reload link did not load the page again: reloaded=%v err=%v", reloaded, err)
		}
	}
	var hash string
	run("after reload", chromedp.Value(selector, &title, chromedp.ByQuery), chromedp.Evaluate(`location.hash`, &hash))
	if title != saved || hash != "#shot-editor" {
		t.Fatalf("the reload link did not load the saved shot: title %q (saved %q), hash %q", title, saved, hash)
	}
}
