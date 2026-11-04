package main

import (
	"bytes"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// Read the actual rendered controls to distinguish per-entity forms even when
// several cards submit to the same endpoint.
func renderedInputs(t *testing.T, body, endpoint string) []map[string]string {
	t.Helper()
	root, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	attribute := func(node *html.Node, name string) string {
		for _, attr := range node.Attr {
			if attr.Key == name {
				return attr.Val
			}
		}
		return ""
	}
	var forms []map[string]string
	var walk func(*html.Node, map[string]string)
	walk = func(node *html.Node, form map[string]string) {
		if node.Type == html.ElementNode && node.Data == "form" {
			form = nil
			if attribute(node, "action") == endpoint {
				form = map[string]string{"rejected": attribute(node, "data-rejected-draft")}
				forms = append(forms, form)
			}
		}
		if form != nil && node.Type == html.ElementNode && node.Data == "input" {
			form[attribute(node, "name")] = attribute(node, "value")
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child, form)
		}
	}
	walk(root, nil)
	return forms
}

func TestWorkspaceProjectActionsPreserveLegacyData(t *testing.T) {
	root, id := legacyFixtureCopy(t)
	s, err := OpenProjectStore(root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, _ := s.Load(id)
	revision := documentRevision(p)
	h := &workspaceHandler{store: s, csrf: "token"}
	path := filepath.Join(root, id, "project.json")
	before, _ := os.ReadFile(path)
	mediaPath := filepath.Join(root, id, "media", "first", "e2d9cf447f23.png")
	media, _ := os.ReadFile(mediaPath)
	form := url.Values{"csrf": {"token"}, "project": {id}, "shot": {"first"}, "revision": {strconv.Itoa(revision)}, "view": {"direction"}, "aspect": {"9:16"}, "runtime_limit_seconds": {"4000"}}
	for _, name := range projectTextFields {
		form.Set(name, name+" 雪 <draft>\n  Keep spaces")
	}
	r, result := shotActionPost(h, "/project", form)
	if r.Code != 400 || result.OK || result.FieldErrors["runtime_limit_seconds"] == "" || result.Values["music_video"] != "false" {
		t.Fatalf("project rejection: %d %s", r.Code, r.Body.String())
	}
	for _, name := range projectTextFields {
		if result.Values[name] != form.Get(name) {
			t.Fatalf("project rejection lost %s", name)
		}
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("rejected project action changed bytes")
	}
	form.Set("runtime_limit_seconds", "120")
	r = workspacePost(h, "/project", form)
	p, _ = s.Load(id)
	if r.Code != 303 || r.Header().Get("Location") != pageURL(id, "direction", "first", "") || documentRevision(p) != revision+1 || boolean(p, "music_video") || number(p, "runtime_limit_seconds") != 120 {
		t.Fatal("project save changed redirect, checkbox state or revision")
	}
	for _, name := range projectTextFields {
		if shotText(p, name) != form.Get(name) {
			t.Fatalf("project save lost %s", name)
		}
	}
	if !strings.Contains(string(p["future"]), "900719925474099312345") || len(list(list(p, "shots")[0], "assets")) != 1 {
		t.Fatal("project action lost opaque legacy data")
	}
	order := url.Values{"csrf": {"token"}, "project": {id}, "shot": {"first"}, "revision": {strconv.Itoa(revision + 1)}, "view": {"board"}, "position": {"after"}}
	r, result = shotActionPost(h, "/insert", order)
	p, _ = s.Load(id)
	shots := list(p, "shots")
	if r.Code != 200 || !result.OK || documentRevision(p) != revision+2 || len(shots) != 3 || shotText(shots[0], "id") != "first" || shotText(shots[2], "id") != "second" || len(list(shots[1], "assets")) != 0 || !nullJSON(shots[1]["approval"]) {
		t.Fatal("insertion copied media/approval, lost identity or incremented more than once")
	}
	saved, _ := os.ReadFile(path)
	r, _ = shotActionPost(h, "/insert", order)
	if r.Code != 409 {
		t.Fatal("insertion replay did not reject the stale revision")
	}
	after, _ = os.ReadFile(path)
	if !bytes.Equal(saved, after) {
		t.Fatal("insertion replay wrote project bytes")
	}
	order.Set("shot", shotText(shots[1], "id"))
	order.Set("revision", strconv.Itoa(revision+2))
	order.Set("delta", "1")
	r = workspacePost(h, "/move", order)
	p, _ = s.Load(id)
	if r.Code != 303 || documentRevision(p) != revision+3 || shotText(list(p, "shots")[1], "id") != "second" || shotText(list(p, "shots")[2], "id") != order.Get("shot") {
		t.Fatal("reorder changed identities or revision semantics")
	}
	order.Set("revision", strconv.Itoa(revision+3))
	saved, _ = os.ReadFile(path)
	r, _ = shotActionPost(h, "/move", order)
	after, _ = os.ReadFile(path)
	if r.Code != 400 || !bytes.Equal(saved, after) {
		t.Fatal("edge move changed project bytes")
	}
	mediaAfter, err := os.ReadFile(mediaPath)
	if err != nil || !bytes.Equal(media, mediaAfter) {
		t.Fatal("project/structure actions changed original media", err)
	}
	t.Log("project validation retains unchecked false and all text; successful project/insert/move commands increment once; stale insertion and edge moves write nothing; legacy media and large integers survive")
}

func TestWorkspaceRejectedMoveScopesEachShotCard(t *testing.T) {
	root, id := legacyFixtureCopy(t)
	s, err := OpenProjectStore(root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, _ := s.Load(id)
	h := &workspaceHandler{store: s, csrf: "token"}
	form := url.Values{"csrf": {"token"}, "project": {id}, "shot": {"second"}, "revision": {strconv.Itoa(documentRevision(p))}, "view": {"board"}, "delta": {"0"}}
	r := workspacePost(h, "/move", form)
	if r.Code != 400 {
		t.Fatal("invalid movement was accepted")
	}
	forms := renderedInputs(t, r.Body.String(), "/move")
	if len(forms) != 2 || forms[0]["shot"] != "first" || forms[0]["rejected"] != "" || forms[1]["shot"] != "second" || forms[1]["rejected"] != "true" {
		t.Fatalf("rejected movement leaked into another shot's form: %+v", forms)
	}
}

func TestWorkspaceReferenceNoteActionKeepsItsTarget(t *testing.T) {
	store := testStore(t)
	p := createTestProject(t, store)
	id := shotText(p, "id")
	h := &workspaceHandler{store: store, csrf: "token"}
	p, err := store.UpdateProject(id, documentRevision(p), ProjectDocument{"references": rawValue([]ProjectDocument{
		{"title": rawValue("First source"), "url": rawValue("https://example.com/first")},
		{"title": rawValue("Second source"), "url": rawValue("https://example.com/second")},
	})})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.path, id, "project.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{"csrf": {"token"}, "project": {id}, "revision": {strconv.Itoa(documentRevision(p))}, "view": {"references"}, "operation": {"remove"}}
	for _, index := range []string{"", "wrong"} {
		if index != "" {
			form.Set("index", index)
		}
		if r := workspacePost(h, "/reference-note", form); r.Code != 400 {
			t.Fatalf("remove with index %q returned %d", index, r.Code)
		}
	}
	form.Set("index", "1")
	form.Set("operation", "update")
	form.Set("title", "Keep this draft 雪")
	form.Set("url", "ftp://example.com/source")
	r := workspacePost(h, "/reference-note", form)
	forms := renderedInputs(t, r.Body.String(), "/reference-note")
	if r.Code != 400 || len(forms) != 5 {
		t.Fatalf("invalid reference update returned %d with %d note forms", r.Code, len(forms))
	}
	for _, fields := range forms {
		wantDraft := fields["index"] == "1" && fields["operation"] == "update"
		if (fields["rejected"] == "true") != wantDraft || (fields["title"] == form.Get("title")) != wantDraft {
			t.Fatalf("reference draft attached to the wrong form: %+v", fields)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("invalid reference actions changed project bytes", err)
	}
	form.Set("url", "https://example.com/corrected")
	r, result := shotActionPost(h, "/reference-note", form)
	p, err = store.Load(id)
	refs := list(p, "references")
	if err != nil || r.Code != 200 || !result.OK || len(refs) != 2 || documentRevision(p) != 3 ||
		shotText(refs[0], "title") != "First source" || shotText(refs[1], "title") != form.Get("title") {
		t.Fatalf("corrected reference update did not save only the second note: %d %v", r.Code, err)
	}
}
