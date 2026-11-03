package main

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func workspacePost(h *workspaceHandler, path string, form url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "http://127.0.0.1:4444"+path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "http://127.0.0.1:4444")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestWorkspaceCoreCommandsAndRejectedDrafts(t *testing.T) {
	s := testStore(t)
	p := createTestProject(t, s)
	id := shotText(p, "id")
	shots, _ := documentShots(p)
	sid := shotText(shots[0], "id")
	h := &workspaceHandler{store: s, csrf: "token"}
	form := url.Values{"csrf": {"token"}, "project": {id}, "shot": {sid}, "revision": {"1"}, "view": {"board"}, "duration": {"7.5"}, "dialogue": {"雪 <script>alert(1)</script>"}, "feedback": {"Try again"}}
	if r := workspacePost(h, "/save", form); r.Code != 303 {
		t.Fatalf("save: %d %s", r.Code, r.Body.String())
	}
	q, _ := s.Load(id)
	shots, _ = documentShots(q)
	if number(shots[0], "duration") != 7.5 || shotText(shots[0], "feedback") != "Try again" {
		t.Fatal("full shot fields not saved")
	}
	form.Set("dialogue", "Unsaved <angle> text")
	r := workspacePost(h, "/save", form)
	if r.Code != 409 || !strings.Contains(r.Body.String(), "Unsaved &lt;angle&gt; text") {
		t.Fatalf("stale draft lost: %d", r.Code)
	}
	if strings.Count(r.Body.String(), `data-rejected-draft="true"`) != 1 || !strings.Contains(r.Body.String(), `id="save-status">Unsaved changes`) {
		t.Fatal("rejected shot draft must be unsaved before the client mounts")
	}
	if strings.Contains(r.Body.String(), "<script>alert(1)</script>") {
		t.Fatal("unsafe HTML")
	}
	form.Set("revision", "2")
	form.Set("duration", "61")
	r = workspacePost(h, "/save", form)
	if r.Code != 400 {
		t.Fatalf("invalid duration: %d", r.Code)
	}
	q, _ = s.Load(id)
	if documentRevision(q) != 2 {
		t.Fatal("failed edit committed")
	}
	form = url.Values{"csrf": {"token"}, "project": {id}, "revision": {"2"}, "view": {"direction"}, "brief": {"An updated brief"}, "runtime_limit_seconds": {"120"}, "music_video": {"true"}}
	if r = workspacePost(h, "/project", form); r.Code != 303 {
		t.Fatalf("project: %s", r.Body.String())
	}
	q, _ = s.Load(id)
	if !boolean(q, "music_video") || number(q, "runtime_limit_seconds") != 120 {
		t.Fatal("project fields lost")
	}
	form.Set("revision", "3")
	form.Set("references", `[{"url":"https://example.com","title":"Source","future":900719925474099312345}]`)
	if r = workspacePost(h, "/references", form); r.Code != 303 {
		t.Fatalf("refs: %s", r.Body.String())
	}
	form.Set("revision", "4")
	form.Set("shot", sid)
	form.Set("position", "after")
	if r = workspacePost(h, "/insert", form); r.Code != 303 {
		t.Fatalf("insert: %s", r.Body.String())
	}
	form.Set("revision", "5")
	form.Set("delta", "1")
	if r = workspacePost(h, "/move", form); r.Code != 303 {
		t.Fatalf("move: %s", r.Body.String())
	}
	q, _ = s.Load(id)
	shots, _ = documentShots(q)
	if shotText(shots[1], "id") != sid {
		t.Fatal("reorder lost identity")
	}
	for _, view := range []string{"board", "episode", "direction", "references", "handoff"} {
		r = httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", "http://127.0.0.1:4444/?project="+id+"&view="+view, nil))
		if r.Code != 200 || !strings.Contains(r.Body.String(), "/client/assets/") || strings.Contains(r.Body.String(), "data-rejected-draft") {
			t.Fatalf("view %s: %d", view, r.Code)
		}
	}
}

func TestTemplateAndPlanPreserveMediaAndUnknownData(t *testing.T) {
	s := testStore(t)
	raw := ProjectDocument{"title": rawValue("Template"), "shots": rawValue([]ProjectDocument{{"id": rawValue("old"), "title": rawValue("First"), "assets": rawValue([]any{"must not copy"}), "approval": rawValue(true)}})}
	p, err := s.CreateTemplate(raw)
	if err != nil {
		t.Fatal(err)
	}
	id := shotText(p, "id")
	shots, _ := documentShots(p)
	sid := shotText(shots[0], "id")
	if sid == "old" || len(list(shots[0], "assets")) != 0 || !nullJSON(shots[0]["approval"]) {
		t.Fatal("template copied identity or media")
	}
	shots[0]["future"] = json.RawMessage(`{"n":900719925474099312345}`)
	shots[0]["assets"] = json.RawMessage(`[{"id":"keep","kind":"clip","path":"media/source.mp4"}]`)
	p["shots"] = rawValue(shots)
	dir, _ := projectDirectory(s.root, id)
	if err := writeProjectDocument(dir, p); err != nil {
		t.Fatal(err)
	}
	dir.Close()
	plan := ProjectDocument{"shots": rawValue([]ProjectDocument{{"id": rawValue(sid), "title": rawValue("Refined"), "assets": rawValue([]any{})}})}
	p, err = s.ApplyPlan(id, 1, plan)
	if err != nil {
		t.Fatal(err)
	}
	shots, _ = documentShots(p)
	if shotText(shots[0], "title") != "Refined" || len(list(shots[0], "assets")) != 1 || !strings.Contains(string(shots[0]["future"]), "900719925474099312345") {
		t.Fatal("plan lost saved data")
	}
	for _, bad := range []ProjectDocument{{"shots": rawValue([]ProjectDocument{{"id": rawValue("missing")}})}, {"references": rawValue([]ProjectDocument{{"url": rawValue("javascript:alert(1)")}})}} {
		if _, err = s.ApplyPlan(id, 2, bad); err == nil {
			t.Fatal("accepted bad plan")
		}
	}
	state, _ := s.State()
	if state.Projects[0].Revision != 2 {
		t.Fatal("failed plan wrote revision")
	}
}

func TestSceneFilterChoosesShotFromThatScene(t *testing.T) {
	s := testStore(t)
	p := createTestProject(t, s)
	shots := list(p, "shots")
	shots[0]["scene"] = rawValue("Arrival")
	shots = append(shots, ProjectDocument{"id": rawValue("departure-shot"), "title": rawValue("Departure shot"), "scene": rawValue("Departure"), "duration": rawValue(4)})
	p["shots"] = rawValue(shots)
	dir, _ := projectDirectory(s.root, shotText(p, "id"))
	if err := writeProjectDocument(dir, p); err != nil {
		t.Fatal(err)
	}
	dir.Close()
	h := &workspaceHandler{store: s, csrf: "token"}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1:4444"+pageURL(shotText(p, "id"), "board", "", "Departure"), nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `name="shot" value="departure-shot"`) {
		t.Fatal("filtered scene opened another scene's shot editor")
	}
}
