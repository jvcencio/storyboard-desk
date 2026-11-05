//go:build browser

package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/page"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx/desktop"
)

// These tests run the actual Go-WASM editor in an isolated headless browser.
// Native Save dialog results are injected here; OS dialog proof is separate.
func editorBrowser(t *testing.T, h http.Handler) (context.Context, string) {
	t.Helper()
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	return editorBrowserContext(t), server.URL
}

func editorBrowserContext(t *testing.T, extra ...chromedp.ExecAllocatorOption) context.Context {
	t.Helper()
	name := os.Getenv("STORYBOARD_BROWSER_EXECUTABLE")
	if name == "" {
		name = "google-chrome"
	}
	binary, err := exec.LookPath(name)
	if err != nil {
		binary, err = exec.LookPath("chromium")
	}
	if err != nil {
		if os.Getenv("STORYBOARD_REQUIRE_BROWSER") == "1" {
			t.Fatal("headless Chrome is required")
		}
		t.Skip("headless Chrome is not installed")
	}
	parent, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	options := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.ExecPath(binary), chromedp.WindowSize(1440, 960), chromedp.Flag("headless", "new"), chromedp.NoSandbox)
	options = append(options, extra...)
	allocator, stop := chromedp.NewExecAllocator(parent, options...)
	t.Cleanup(stop)
	ctx, closeBrowser := chromedp.NewContext(allocator)
	t.Cleanup(func() { _ = chromedp.Cancel(ctx); closeBrowser() })
	var mu sync.Mutex
	errors := []string{}
	chromedp.ListenTarget(ctx, func(event any) {
		if exception, ok := event.(*cdpruntime.EventExceptionThrown); ok {
			mu.Lock()
			detail := exception.ExceptionDetails.Text
			if exception.ExceptionDetails.Exception != nil {
				detail += ": " + exception.ExceptionDetails.Exception.Description
			}
			errors = append(errors, detail)
			mu.Unlock()
		}
	})
	t.Cleanup(func() {
		if t.Failed() {
			mu.Lock()
			defer mu.Unlock()
			t.Logf("browser exceptions: %v", errors)
		}
	})
	return ctx
}

func browserText(selector, want string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		deadline, cancel := context.WithTimeout(ctx, 12*time.Second)
		defer cancel()
		for {
			var text string
			if err := chromedp.Run(deadline, chromedp.Text(selector, &text, chromedp.ByQuery)); err != nil {
				if deadline.Err() != nil {
					return err
				}
				continue // A full page navigation can invalidate a just-read node.
			}
			if text == want {
				return nil
			}
			select {
			case <-deadline.Done():
				return fmt.Errorf("%s: got %q, want %q", selector, text, want)
			case <-time.After(30 * time.Millisecond):
			}
		}
	})
}

func browserReady() chromedp.Action {
	return chromedp.WaitReady(`body[data-editor-ready="true"]`, chromedp.ByQuery)
}

func TestWorkspaceGoClientRejectedDraftNavigation(t *testing.T) {
	for _, reason := range []string{"stale", "invalid"} {
		t.Run(reason, func(t *testing.T) {
			s := testStore(t)
			p := createTestProject(t, s)
			id, sid := shotText(p, "id"), shotText(list(p, "shots")[0], "id")
			h := &workspaceHandler{store: s, csrf: "browser-token"}
			ctx, origin := editorBrowser(t, h)
			const selector = `form[action="/save"] input[name="title"]`
			const draft = "Rejected draft 雪 <keep>"
			if err := chromedp.Run(ctx, chromedp.Navigate(origin+pageURL(id, "board", sid, "")), browserReady(),
				chromedp.Clear(selector, chromedp.ByQuery), chromedp.SendKeys(selector, draft, chromedp.ByQuery)); err != nil {
				t.Fatal(err)
			}
			wantStatus, wantRevision := int64(409), 2
			if reason == "stale" {
				if _, err := s.UpdateShot(id, sid, 1, ProjectDocument{"title": rawValue("Saved in another window")}); err != nil {
					t.Fatal(err)
				}
			} else {
				wantStatus, wantRevision = 400, 1
				if err := chromedp.Run(ctx, chromedp.Clear(`form[action="/save"] input[name="duration"]`, chromedp.ByQuery),
					chromedp.SendKeys(`form[action="/save"] input[name="duration"]`, "61", chromedp.ByQuery)); err != nil {
					t.Fatal(err)
				}
			}
			projectPath := filepath.Join(s.path, id, "project.json")
			before, err := os.ReadFile(projectPath)
			if err != nil {
				t.Fatal(err)
			}
			response, err := chromedp.RunResponse(ctx, chromedp.Click(`form[action="/save"] button`, chromedp.ByQuery))
			if err != nil || response == nil || response.Status != wantStatus {
				t.Fatalf("save rejection: response=%+v, err=%v", response, err)
			}
			var title, revision string
			if err = chromedp.Run(ctx, browserReady(), chromedp.Value(selector, &title, chromedp.ByQuery),
				chromedp.Value(`form[action="/save"] input[name="revision"]`, &revision, chromedp.ByQuery)); err != nil {
				t.Fatal(err)
			}
			if title != draft || revision != "1" {
				t.Fatalf("rejection changed submitted draft/revision: %q, %q", title, revision)
			}
			if reason == "invalid" {
				var screenshot []byte
				if err = chromedp.Run(ctx,
					chromedp.WaitVisible(`form[action="/save"] input[name="duration"][aria-invalid="true"]`, chromedp.ByQuery),
					browserText(`#shot-editor .field-error`, "number must be between 1 and 60"),
					chromedp.Screenshot(`#shot-editor label:has(input[name="duration"])`, &screenshot, chromedp.ByQuery)); err != nil {
					t.Fatal(err)
				}
				if err = os.MkdirAll("../build/verification/go-client", 0755); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile("../build/verification/go-client/shot-validation.png", screenshot, 0644); err != nil {
					t.Fatal(err)
				}
			}
			cancelled := make(chan error, 1)
			chromedp.ListenTarget(ctx, func(event any) {
				if _, ok := event.(*page.EventJavascriptDialogOpening); ok {
					go func() { cancelled <- chromedp.Run(ctx, page.HandleJavaScriptDialog(false)) }()
				}
			})
			if err = chromedp.Run(ctx, chromedp.Click(`a[href*="view=library"]`, chromedp.ByQuery)); err != nil {
				t.Fatal(err)
			}
			select {
			case err = <-cancelled:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("navigation discarded a rejected draft without asking")
			}
			if err = chromedp.Run(ctx, chromedp.Value(selector, &title, chromedp.ByQuery), browserText("#save-status", "Unsaved changes")); err != nil {
				t.Fatal(err)
			}
			if title != draft {
				t.Fatalf("cancelled navigation lost rejected draft: %q", title)
			}
			p, err = s.Load(id)
			if err != nil || documentRevision(p) != wantRevision || shotText(list(p, "shots")[0], "title") == draft {
				t.Fatal("rejected edit wrote to storage", err)
			}
			after, err := os.ReadFile(projectPath)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("rejection changed project bytes", err)
			}
			t.Logf("HTTP %d retained the draft and submitted revision 1; navigation asked, Cancel kept the draft and Unsaved changes; storage stayed byte-identical at revision %d", wantStatus, wantRevision)
		})
	}
}

func TestWorkspaceGoClientProjectActions(t *testing.T) {
	s := testStore(t)
	p := createTestProject(t, s)
	id, sid := shotText(p, "id"), shotText(list(p, "shots")[0], "id")
	if _, err := s.UpdateProject(id, 1, ProjectDocument{"music_video": rawValue(true)}); err != nil {
		t.Fatal(err)
	}
	h := &workspaceHandler{store: s, csrf: "browser-token"}
	ctx, origin := editorBrowser(t, h)
	const titleField = `form[action="/project"] input[name="title"]`
	const budgetField = `form[action="/project"] input[name="runtime_limit_seconds"]`
	const draft = "Project draft 雪 <keep>"
	if err := chromedp.Run(ctx, chromedp.Navigate(origin+pageURL(id, "direction", sid, "")), browserReady(),
		chromedp.Clear(titleField, chromedp.ByQuery), chromedp.SendKeys(titleField, draft, chromedp.ByQuery),
		chromedp.Click(`form[action="/project"] input[name="music_video"]`, chromedp.ByQuery),
		chromedp.Clear(budgetField, chromedp.ByQuery), chromedp.SendKeys(budgetField, "4000", chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	response, err := chromedp.RunResponse(ctx, chromedp.Click(`form[action="/project"] button`, chromedp.ByQuery))
	if err != nil || response == nil || response.Status != 400 {
		t.Fatalf("project validation response: %+v, %v", response, err)
	}
	var title string
	if err = chromedp.Run(ctx, browserReady(), browserText("#save-status", "Unsaved changes"),
		chromedp.Value(titleField, &title, chromedp.ByQuery),
		chromedp.WaitVisible(`form[action="/project"] input[name="music_video"]:not(:checked)`, chromedp.ByQuery),
		chromedp.WaitVisible(`form[action="/project"] input[name="runtime_limit_seconds"][aria-invalid="true"]`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if title != draft {
		t.Fatal("project rejection lost its title")
	}
	cancelled := make(chan error, 1)
	chromedp.ListenTarget(ctx, func(event any) {
		if _, ok := event.(*page.EventJavascriptDialogOpening); ok {
			go func() { cancelled <- chromedp.Run(ctx, page.HandleJavaScriptDialog(false)) }()
		}
	})
	if err = chromedp.Run(ctx, chromedp.Click(`a[href*="view=library"]`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-cancelled:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("project rejection lost its navigation guard")
	}
	if err = chromedp.Run(ctx, chromedp.Value(titleField, &title, chromedp.ByQuery)); err != nil || title != draft {
		t.Fatal("cancelled navigation lost the project draft", err)
	}
	if err = chromedp.Run(ctx, chromedp.Clear(budgetField, chromedp.ByQuery), chromedp.SendKeys(budgetField, "120", chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if _, err = chromedp.RunResponse(ctx, chromedp.Click(`form[action="/project"] button`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if err = chromedp.Run(ctx, browserReady(), browserText("#save-status", "Saved locally")); err != nil {
		t.Fatal(err)
	}
	p, _ = s.Load(id)
	if documentRevision(p) != 3 || boolean(p, "music_video") || shotText(p, "title") != draft {
		t.Fatal("corrected project draft did not save once with the checkbox off")
	}
	if _, err = chromedp.RunResponse(ctx, chromedp.Click(`a[href*="view=board"]`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if err = chromedp.Run(ctx, browserReady()); err != nil {
		t.Fatal(err)
	}
	if _, err = chromedp.RunResponse(ctx, chromedp.Click(`form[action="/insert"] button[value="after"]`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if err = chromedp.Run(ctx, browserReady()); err != nil {
		t.Fatal(err)
	}
	move := `button[form="board-move"][name="move"][value="1:` + sid + `"]`
	if _, err = chromedp.RunResponse(ctx, chromedp.Click(move, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if err = chromedp.Run(ctx, browserReady()); err != nil {
		t.Fatal(err)
	}
	p, _ = s.Load(id)
	shots := list(p, "shots")
	if documentRevision(p) != 5 || len(shots) != 2 || shotText(shots[1], "id") != sid {
		t.Fatal("rendered insertion/reorder controls changed identities or revision behavior")
	}
	t.Log("project rejection retained title, unchecked checkbox and navigation guard; correcting and saving, inserting, then moving each incremented the revision once")
}

func TestWorkspaceGoClientFormsFilesAndNativeSaving(t *testing.T) {
	s := testStore(t)
	p := createTestProject(t, s)
	id := shotText(p, "id")
	var cancelSave atomic.Bool
	cancelSave.Store(true)
	var dialogCalls atomic.Int32
	destination := t.TempDir()
	h := &workspaceHandler{store: s, csrf: "browser-token", saveFile: func(options desktop.SaveFileOptions) (string, error) {
		dialogCalls.Add(1)
		if cancelSave.Load() {
			return "", nil
		}
		name := "review.zip"
		if options.DefaultExt == "json" {
			name = "plan.json"
		}
		return filepath.Join(destination, name), nil
	}}
	ctx, origin := editorBrowser(t, h)
	started := time.Now()
	var title, status string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(origin+pageURL(id, "board", "", "")), browserReady(),
		chromedp.Clear(`form[action="/save"] input[name="title"]`, chromedp.ByQuery),
		chromedp.SendKeys(`form[action="/save"] input[name="title"]`, "The opening · unsaved", chromedp.ByQuery),
		browserText("#save-status", "Unsaved changes"),
		chromedp.Click(`form[action="/export"] button`, chromedp.ByQuery), browserText("#download-status", "Save cancelled"),
		chromedp.Value(`form[action="/save"] input[name="title"]`, &title, chromedp.ByQuery),
		chromedp.Text("#save-status", &status, chromedp.ByQuery),
	); err != nil {
		t.Fatal(err)
	}
	bootAndCancel := time.Since(started)
	if title != "The opening · unsaved" || status != "Unsaved changes" || dialogCalls.Load() != 1 {
		t.Fatalf("cancellation lost a draft: %q, %q, %d dialogs", title, status, dialogCalls.Load())
	}
	cancelSave.Store(false)
	if err := chromedp.Run(ctx, chromedp.Click(`form[action="/export"] button`, chromedp.ByQuery), browserText("#download-status", "Saved review.zip"), chromedp.Value(`form[action="/save"] input[name="title"]`, &title, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if title != "The opening · unsaved" {
		t.Fatal("export replaced the dirty editor page")
	}
	archive, err := zip.OpenReader(filepath.Join(destination, "review.zip"))
	if err != nil {
		t.Fatal(err)
	}
	archive.Close()
	var dismissals atomic.Int32
	chromedp.ListenTarget(ctx, func(event any) {
		if _, ok := event.(*page.EventJavascriptDialogOpening); ok {
			dismissals.Add(1)
			go func() { _ = chromedp.Run(ctx, page.HandleJavaScriptDialog(false)) }()
		}
	})
	if err = chromedp.Run(ctx, chromedp.Click(`a[href*="view=library"]`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(4 * time.Second)
	for dismissals.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if dismissals.Load() != 1 {
		t.Fatal("dirty navigation did not ask before leaving")
	}
	if err = chromedp.Run(ctx, chromedp.Value(`form[action="/save"] input[name="title"]`, &title, chromedp.ByQuery)); err != nil || title != "The opening · unsaved" {
		t.Fatalf("cancelled navigation lost the draft: %v %q", err, title)
	}
	if _, err = chromedp.RunResponse(ctx, chromedp.Click(`form[action="/save"] button`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if err = chromedp.Run(ctx, browserReady(), browserText("#save-status", "Saved locally")); err != nil {
		t.Fatal(err)
	}
	p, _ = s.Load(id)
	if documentRevision(p) != 2 || shotText(list(p, "shots")[0], "title") != title {
		t.Fatal("Go client did not submit the shot form")
	}
	if _, err = chromedp.RunResponse(ctx, chromedp.Click(`a[href*="view=handoff"]`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if err = chromedp.Run(ctx, browserReady(), chromedp.Click(`a[href^="/download/plan"]`, chromedp.ByQuery), browserText("#download-status", "Saved plan.json")); err != nil {
		t.Fatal(err)
	}
	plan, err := os.ReadFile(filepath.Join(destination, "plan.json"))
	if err != nil || !json.Valid(plan) {
		t.Fatal("native plan download did not save JSON", err)
	}
	if err = chromedp.Run(ctx, chromedp.Click(`details:has(> #claude-brief) > summary`, chromedp.ByQuery), chromedp.WaitVisible("#claude-brief", chromedp.ByQuery), chromedp.Click(`button[data-copy="claude-brief"]`, chromedp.ByQuery), browserText(`button[data-copy="claude-brief"]`, "Copied")); err != nil {
		t.Fatal(err)
	}
	valid := filepath.Join(t.TempDir(), "plan 雪.json")
	raw := `{"shots":[],"future":900719925474099312345,"text":"preserve 雪"}`
	if err = os.WriteFile(valid, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	var loaded string
	if err = chromedp.Run(ctx,
		chromedp.SetUploadFiles(`input[data-json-file="plan"]`, []string{valid}, chromedp.ByQuery),
		browserText(`form[action="/plan"] [role=status]`, "Loaded into the form. Review the text before applying it."),
		chromedp.Value(`textarea[name="plan"]`, &loaded, chromedp.ByQuery),
	); err != nil {
		t.Fatal(err)
	}
	if loaded != raw {
		t.Fatal("JSON preview changed opaque JSON bytes")
	}
	invalid := filepath.Join(t.TempDir(), "invalid.json")
	if err = os.WriteFile(invalid, []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = chromedp.Run(ctx, chromedp.SetUploadFiles(`input[data-json-file="plan"]`, []string{invalid}, chromedp.ByQuery), browserText(`form[action="/plan"] [role=status]`, "Choose a valid JSON file."), chromedp.Value(`textarea[name="plan"]`, &loaded, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if loaded != raw {
		t.Fatal("invalid JSON replaced the previous draft")
	}
	large := filepath.Join(t.TempDir(), "large.json")
	if err = os.WriteFile(large, bytes.Repeat([]byte(" "), 1048577), 0600); err != nil {
		t.Fatal(err)
	}
	if err = chromedp.Run(ctx, chromedp.SetUploadFiles(`input[data-json-file="plan"]`, []string{large}, chromedp.ByQuery), browserText(`form[action="/plan"] [role=status]`, "Choose a JSON file smaller than 1 MiB.")); err != nil {
		t.Fatal(err)
	}
	if err = chromedp.Run(ctx, chromedp.Click(`a[href="#new-project"]`, chromedp.ByQuery), chromedp.WaitVisible(`#new-project[open] input[name="title"]`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	t.Logf("Go-WASM cold navigation, draft edit and cancelled native-save adapter: %s", bootAndCancel)
}

func TestWorkspaceGoClientMediaImportMetadataAndCoverage(t *testing.T) {
	if _, err := exec.LookPath(videoTool("ffmpeg")); err != nil {
		if os.Getenv("STORYBOARD_REQUIRE_FFMPEG") == "1" {
			t.Fatal(err)
		}
		t.Skip("FFmpeg is required for the real media fixture")
	}
	source := filepath.Join(t.TempDir(), "clip sample.mp4")
	if _, err := runMedia(30*time.Second, "ffmpeg", "-v", "error", "-nostdin", "-n", "-f", "lavfi", "-i", "color=c=0x355e50:size=160x90:rate=24", "-t", "1", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-threads", "2", source); err != nil {
		t.Fatal(err)
	}
	s := testStore(t)
	p, err := s.CreateTemplate(ProjectDocument{"title": rawValue("Go editor verification"), "aspect": rawValue("9:16"), "shots": rawValue([]ProjectDocument{{"title": rawValue("The opening"), "purpose": rawValue("A synthetic media import and playback check."), "duration": rawValue(2)}})})
	if err != nil {
		t.Fatal(err)
	}
	id, sid := shotText(p, "id"), shotText(list(p, "shots")[0], "id")
	clip, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	p, err = s.ImportMedia(id, sid, "clip", filepath.Base(source), int64(len(clip)), bytes.NewReader(clip), documentRevision(p), "", "")
	if err != nil {
		t.Fatal(err)
	}
	aid := shotText(list(list(p, "shots")[0], "assets")[0], "id")
	p, err = s.Choose(id, sid, "clip", aid, false, documentRevision(p))
	if err != nil {
		t.Fatal(err)
	}
	// Older imports can lack cached duration. The Go browser adapter must read
	// actual metadata, populate range measurements, and stop at missing footage.
	shots := list(p, "shots")
	assets := list(shots[0], "assets")
	delete(assets[0], "duration_seconds")
	shots[0]["assets"] = rawValue(assets)
	p["shots"] = rawValue(shots)
	dir, err := projectDirectory(s.root, id)
	if err != nil {
		t.Fatal(err)
	}
	err = writeProjectDocument(dir, p)
	dir.Close()
	if err != nil {
		t.Fatal(err)
	}
	png, err := os.ReadFile("testdata/legacy/projects/664aadfb5b62/media/first/e2d9cf447f23.png")
	if err != nil {
		t.Fatal(err)
	}
	imageFile := filepath.Join(t.TempDir(), "Picker sample 雪.png")
	if err = os.WriteFile(imageFile, png, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, origin := editorBrowser(t, &workspaceHandler{store: s, csrf: "browser-token"})
	if err = chromedp.Run(ctx, chromedp.Navigate(origin+pageURL(id, "library", sid, "")), browserReady(), chromedp.SendKeys(`form[action="/upload"] select[name="kind"]`, "k", chromedp.ByQuery), chromedp.SetUploadFiles(`form[action="/upload"] input[type="file"]`, []string{imageFile}, chromedp.ByQuery), chromedp.Click(`form[action="/upload"] button`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	var imported ProjectDocument
	if err = chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		for {
			current, err := s.Load(id)
			if err != nil {
				return err
			}
			for _, asset := range list(list(current, "shots")[0], "assets") {
				if shotText(asset, "original_name") == filepath.Base(imageFile) {
					imported = asset
					return nil
				}
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(20 * time.Millisecond):
			}
		}
	})); err != nil {
		t.Fatal(err)
	}
	if err = chromedp.Run(ctx, chromedp.WaitReady(`[data-asset="`+shotText(imported, "id")+`"]`, chromedp.ByQuery), browserReady()); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(png)
	if shotText(imported, "kind") != "keyframe" || shotText(imported, "sha256") != hex.EncodeToString(digest[:]) {
		t.Fatal("Go client uploaded the wrong type or bytes")
	}
	var measured string
	if err = chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		for {
			if err := chromedp.Run(ctx, chromedp.Value(`[data-asset="`+aid+`"] input[name="measured"]`, &measured, chromedp.ByQuery)); err != nil {
				return err
			}
			if measured == "1" {
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(20 * time.Millisecond):
			}
		}
	})); err != nil {
		t.Fatal(err)
	}
	var screenshot []byte
	if err = chromedp.Run(ctx, chromedp.CaptureScreenshot(&screenshot)); err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll("../build/verification/go-client", 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile("../build/verification/go-client/desktop.png", screenshot, 0644); err != nil {
		t.Fatal(err)
	}
	if err = chromedp.Run(ctx, chromedp.Click(`a[href*="view=review"]`, chromedp.ByQuery), browserReady(), chromedp.Click("[data-review-play]", chromedp.ByQuery), browserText("[data-review-status]", "This take leaves 1.00 seconds of missing coverage. Choose another range or advance manually.")); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceGoClientReviewAndCloudControls(t *testing.T) {
	s := testStore(t)
	p := createTestProject(t, s)
	id, sid := shotText(p, "id"), shotText(list(p, "shots")[0], "id")
	png, err := os.ReadFile("testdata/legacy/projects/664aadfb5b62/media/first/e2d9cf447f23.png")
	if err != nil {
		t.Fatal(err)
	}
	p, err = s.ImportMedia(id, sid, "keyframe", "sample.png", int64(len(png)), bytes.NewReader(png), documentRevision(p), "", "")
	if err != nil {
		t.Fatal(err)
	}
	aid := shotText(list(list(p, "shots")[0], "assets")[0], "id")
	p, err = s.Choose(id, sid, "keyframe", aid, true, documentRevision(p))
	if err != nil {
		t.Fatal(err)
	}
	ctx, origin := editorBrowser(t, &workspaceHandler{store: s, csrf: "browser-token"})
	var pressed string
	if err = chromedp.Run(ctx,
		chromedp.Navigate(origin+pageURL(id, "review", sid, "")), browserReady(),
		browserText("[data-review-status]", "Shot 1 of 1"), chromedp.Click("[data-review-play]", chromedp.ByQuery),
		chromedp.WaitReady(`[data-review-play][aria-pressed="true"]`, chromedp.ByQuery),
		chromedp.Click("[data-review-play]", chromedp.ByQuery),
		chromedp.WaitReady(`[data-review-play][aria-pressed="false"]`, chromedp.ByQuery),
		browserText("[data-review-status]", "Shot 1 of 1"),
		chromedp.AttributeValue("[data-review-play]", "aria-pressed", &pressed, nil, chromedp.ByQuery),
	); err != nil {
		t.Fatal(err)
	}
	if pressed != "false" {
		t.Fatal("review did not pause")
	}
	if err = chromedp.Run(ctx, chromedp.Click("[data-review-play]", chromedp.ByQuery), browserText("[data-review-status]", "Sequence complete")); err != nil {
		t.Fatal(err)
	}
	// Exercise the actual mounted handle that the pinned GoSX loader received.
	// Disposal must stop active playback and release its button listener.
	var disposed bool
	if err = chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const engine = [...window.__gosx.engines.values()].find(e => e.component === "Workspace");
		const play = document.querySelector("[data-review-play]");
		document.querySelector("[data-review-prev]").click();
		play.click();
		if (play.getAttribute("aria-pressed") !== "true") return false;
		engine.handle.dispose();
		if (play.getAttribute("aria-pressed") !== "false" || engine.handle.dispose !== undefined) return false;
		play.click();
		return play.getAttribute("aria-pressed") === "false";
	})()`, &disposed)); err != nil || !disposed {
		t.Fatalf("GoSX handle did not dispose the active review: %v", err)
	}
	if err = chromedp.Run(ctx, chromedp.Click(`a[href*="view=generate"]`, chromedp.ByQuery), browserReady(), chromedp.WaitReady("[data-cloud-form]", chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	var originalPrompt, prompt, model string
	if err = chromedp.Run(ctx,
		chromedp.Value(`[data-cloud-form] textarea[name="prompt"]`, &originalPrompt, chromedp.ByQuery),
		chromedp.SetValue(`[data-cloud-form] select[name="kind"]`, "video", chromedp.ByQuery),
		chromedp.SendKeys(`[data-cloud-form] select[name="kind"]`, "v", chromedp.ByQuery),
	); err != nil {
		t.Fatal(err)
	}
	// Use native select keyboard events so the Go change listener executes.
	if err = chromedp.Run(ctx, chromedp.SendKeys(`[data-cloud-form] select[name="kind"]`, "i", chromedp.ByQuery), chromedp.SendKeys(`[data-cloud-form] select[name="kind"]`, "v", chromedp.ByQuery), chromedp.Value(`[data-cloud-form] textarea[name="prompt"]`, &prompt, chromedp.ByQuery), chromedp.Value(`[data-cloud-form] select[name="model"]`, &model, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if prompt == originalPrompt || model == "" {
		t.Fatal("Go cloud controls did not switch the prompt and model")
	}
	if err = chromedp.Run(ctx, chromedp.SetValue(`[data-cloud-form] textarea[name="prompt"]`, "My custom direction", chromedp.ByQuery), chromedp.SendKeys(`[data-cloud-form] select[name="kind"]`, "i", chromedp.ByQuery), chromedp.Value(`[data-cloud-form] textarea[name="prompt"]`, &prompt, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if prompt != "My custom direction" {
		t.Fatal("kind switch replaced a custom prompt")
	}
	if err = chromedp.Run(ctx, emulation.SetDeviceMetricsOverride(390, 844, 1, true)); err != nil {
		t.Fatal(err)
	}
	var screenshot []byte
	if err = chromedp.Run(ctx, chromedp.CaptureScreenshot(&screenshot)); err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll("../build/verification/go-client", 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile("../build/verification/go-client/mobile.png", screenshot, 0644); err != nil {
		t.Fatal(err)
	}
}
