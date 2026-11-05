package main

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"m31labs.dev/gosx/action"
)

func shotActionPost(h *workspaceHandler, path string, form url.Values) (*httptest.ResponseRecorder, action.Result) {
	r := httptest.NewRequest("POST", "http://127.0.0.1:4444"+path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "http://127.0.0.1:4444")
	r.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var result action.Result
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	return w, result
}

func TestWorkspaceShotActionContractAndLegacyCompatibility(t *testing.T) {
	root, id := legacyFixtureCopy(t)
	s, err := OpenProjectStore(root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	originalRevision := documentRevision(p)
	originalShot := list(p, "shots")[0]
	projectPath := filepath.Join(root, id, "project.json")
	original, _ := os.ReadFile(projectPath)
	mediaPath := filepath.Join(root, id, "media", "first", "e2d9cf447f23.png")
	mediaBefore, err := os.ReadFile(mediaPath)
	if err != nil {
		t.Fatal(err)
	}
	h := &workspaceHandler{store: s, csrf: "token"}
	form := url.Values{"csrf": {"token"}, "project": {id}, "shot": {"first"}, "revision": {strconv.Itoa(originalRevision)}, "view": {"board"}, "scene_filter": {"Verse"}, "duration": {"61"}}
	for _, name := range shotFields {
		form.Set(name, name+" 雪 <draft>\n  Keep spaces")
	}
	// A decoder must not allow hidden fields to overwrite opaque project data.
	form.Set("future", "replace")
	form.Set("assets", "[]")
	r, result := shotActionPost(h, "/save", form)
	if r.Code != 400 || result.OK || result.FieldErrors["duration"] == "" {
		t.Fatalf("validation result: %d %s", r.Code, r.Body.String())
	}
	for name := range form {
		if name != "csrf" && result.Values[name] != form.Get(name) {
			t.Fatalf("validation lost submitted %s", name)
		}
	}
	if _, ok := result.Values["csrf"]; ok {
		t.Fatal("action result echoed its CSRF token")
	}
	r = workspacePost(h, "/save", form)
	if r.Code != 400 || !strings.Contains(r.Body.String(), `aria-invalid="true"`) || !strings.Contains(r.Body.String(), "aria-describedby=") || strings.Contains(r.Body.String(), "<draft>") {
		t.Fatalf("native form validation/escaping: %d", r.Code)
	}
	after, _ := os.ReadFile(projectPath)
	if !bytes.Equal(original, after) {
		t.Fatal("rejected action wrote project bytes")
	}
	form.Set("duration", "7.5")
	r = workspacePost(h, "/save", form)
	if r.Code != 303 || r.Header().Get("Location") != pageURL(id, "board", "first", "Verse") {
		t.Fatalf("legacy form redirect: %d %q", r.Code, r.Header().Get("Location"))
	}
	p, err = s.Load(id)
	if err != nil || documentRevision(p) != originalRevision+1 {
		t.Fatal("a successful shot action must increment the revision once", err)
	}
	shot := list(p, "shots")[0]
	for _, name := range shotFields {
		if shotText(shot, name) != form.Get(name) {
			t.Fatalf("saved shot lost %s", name)
		}
	}
	if number(shot, "duration") != 7.5 || shotText(shot, "selected_keyframe") != shotText(originalShot, "selected_keyframe") {
		t.Fatal("shot action lost duration, media selection or approval data")
	}
	for _, name := range []string{"assets", "approval", "future"} {
		var before, after bytes.Buffer
		if json.Compact(&before, originalShot[name]) != nil || json.Compact(&after, shot[name]) != nil || !bytes.Equal(before.Bytes(), after.Bytes()) {
			t.Fatalf("shot action changed opaque %s", name)
		}
	}
	for _, doc := range []ProjectDocument{p, shot} {
		if !strings.Contains(string(doc["future"]), "900719925474099312345") {
			t.Fatal("shot action changed an opaque large integer")
		}
	}
	mediaAfter, err := os.ReadFile(mediaPath)
	if err != nil || !bytes.Equal(mediaBefore, mediaAfter) {
		t.Fatal("shot action changed legacy media bytes", err)
	}
	// The submitted revision is deliberately still the old one. Repeating the
	// request must continue to reject it, without silently rebasing the edit.
	saved, _ := os.ReadFile(projectPath)
	for range 2 {
		r, result = shotActionPost(h, "/save", form)
		if r.Code != 409 || result.OK || result.Values["revision"] != strconv.Itoa(originalRevision) || result.Values["dialogue"] != form.Get("dialogue") {
			t.Fatalf("stale action: %d %s", r.Code, r.Body.String())
		}
	}
	after, _ = os.ReadFile(projectPath)
	if !bytes.Equal(saved, after) {
		t.Fatal("stale action or retry changed project bytes")
	}
	form.Set("revision", strconv.Itoa(originalRevision+1))
	r, result = shotActionPost(h, "/save?title=Query+must+not+win&revision=0", form)
	var receipt struct {
		ProjectID string `json:"project_id"`
		ShotID    string `json:"shot_id"`
		Revision  int    `json:"revision"`
	}
	if err = json.Unmarshal(result.Data, &receipt); err != nil || r.Code != 200 || !result.OK || receipt.ProjectID != id || receipt.ShotID != "first" || receipt.Revision != originalRevision+2 || result.Redirect != pageURL(id, "board", "first", "Verse") || strings.Contains(r.Body.String(), `"csrf"`) {
		t.Fatalf("managed action receipt: %d %s (%v)", r.Code, r.Body.String(), err)
	}
	// Older clients omit view and fields they do not edit.
	partial := url.Values{"csrf": {"token"}, "project": {id}, "shot": {"first"}, "revision": {strconv.Itoa(originalRevision + 2)}, "title": {"Partial client 雪"}}
	r = workspacePost(h, "/save", partial)
	p, err = s.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	shot = list(p, "shots")[0]
	if r.Code != 303 || r.Header().Get("Location") != "/" || documentRevision(p) != originalRevision+3 || shotText(shot, "title") != partial.Get("title") || shotText(shot, "dialogue") != form.Get("dialogue") || number(shot, "duration") != 7.5 {
		t.Fatal("partial legacy form compatibility failed", err)
	}
	partial.Set("shot", "missing-shot")
	partial.Set("revision", strconv.Itoa(originalRevision+3))
	r = workspacePost(h, "/save", partial)
	if r.Code != 400 || !strings.Contains(r.Body.String(), `name="shot" value="missing-shot"`) {
		t.Fatal("unavailable shot draft changed its target to another shot")
	}
	t.Log("HTML and GoSX JSON actions preserve submitted fields on rejection, increment once on success, retain opaque legacy data/media, reject stale retries and keep partial-client redirects")
}

func TestWorkspaceShotActionKeepsTransportGuards(t *testing.T) {
	s := testStore(t)
	p := createTestProject(t, s)
	id, sid := shotText(p, "id"), shotText(list(p, "shots")[0], "id")
	h := &workspaceHandler{store: s, csrf: "token"}
	form := url.Values{"csrf": {"token"}, "project": {id}, "shot": {sid}, "revision": {"1"}, "title": {"Must not save"}}
	for _, test := range []struct{ name, host, origin, token, path string }{
		{"host", "example.com", "http://127.0.0.1:4444", "token", "/save"},
		{"origin", "127.0.0.1:4444", "https://example.com", "token", "/save"},
		{"token", "127.0.0.1:4444", "http://127.0.0.1:4444", "wrong", "/save?csrf=token"},
	} {
		t.Run(test.name, func(t *testing.T) {
			form.Set("csrf", test.token)
			r := httptest.NewRequest("POST", "http://"+test.host+test.path, strings.NewReader(form.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set("Origin", test.origin)
			r.Header.Set("Accept", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 403 {
				t.Fatalf("guard accepted request: %d", w.Code)
			}
		})
	}
	p, err := s.Load(id)
	if err != nil || documentRevision(p) != 1 || shotText(list(p, "shots")[0], "title") == "Must not save" {
		t.Fatal("rejected transport request reached the store", err)
	}
}
