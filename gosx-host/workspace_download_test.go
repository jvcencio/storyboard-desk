package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/desktop"
)

func TestNativeExportSaveCancelAndCopy(t *testing.T) {
	s := testStore(t)
	p := createTestProject(t, s)
	id := shotText(p, "id")
	output := filepath.Join(t.TempDir(), "review 雪.zip")
	if err := os.WriteFile(output, []byte("existing destination"), 0600); err != nil {
		t.Fatal(err)
	}
	cancel, calls := true, 0
	h := &workspaceHandler{store: s, csrf: "token", saveFile: func(options desktop.SaveFileOptions) (string, error) {
		calls++
		if options.DefaultExt != "zip" || !options.OverwritePrompt || options.Filters[0].Pattern != "*.zip" {
			t.Fatalf("unsafe save options: %+v", options)
		}
		if cancel {
			return "", nil
		}
		return output, nil
	}}
	form := url.Values{"csrf": {"token"}, "project": {id}, "revision": {"1"}, "view": {"board"}, "destination": {filepath.Join(t.TempDir(), "untrusted.zip")}}
	if response := workspacePost(h, "/export", form); response.Code != 204 {
		t.Fatalf("cancel: %d %s", response.Code, response.Body.String())
	}
	if data, _ := os.ReadFile(output); string(data) != "existing destination" {
		t.Fatal("cancellation changed an existing file")
	}
	cancel = false
	if response := workspacePost(h, "/export", form); response.Code != 204 {
		t.Fatalf("save: %d %s", response.Code, response.Body.String())
	}
	if calls != 2 {
		t.Fatalf("dialog calls: %d", calls)
	}
	archive, err := zip.OpenReader(output)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	seen := map[string]bool{}
	for _, entry := range archive.File {
		reader, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.Copy(io.Discard, reader)
		reader.Close()
		if err != nil {
			t.Fatalf("ZIP integrity: %v", err)
		}
		seen[entry.Name] = true
	}
	if !seen["project.json"] || !seen["STORYBOARD.txt"] {
		t.Fatalf("incomplete saved review: %v", seen)
	}
	current, _ := s.Load(id)
	if documentRevision(current) != documentRevision(p) {
		t.Fatal("saving an export changed the project")
	}
	if _, err := os.Stat(form.Get("destination")); !os.IsNotExist(err) {
		t.Fatal("request-supplied destination was used")
	}
}

func TestNativeDownloadAuthorizationPathsAndErrors(t *testing.T) {
	s := testStore(t)
	id := shotText(createTestProject(t, s), "id")
	called := 0
	h := &workspaceHandler{store: s, csrf: "token", saveFile: func(desktop.SaveFileOptions) (string, error) { called++; return "", nil }}
	form := url.Values{"csrf": {"token"}, "project": {id}, "revision": {"1"}, "kind": {"plan"}}
	request := func(origin, token, path string) *httptest.ResponseRecorder {
		values := maps.Clone(form)
		values.Set("csrf", token)
		r := httptest.NewRequest("POST", "http://127.0.0.1:4444"+path, strings.NewReader(values.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", origin)
		r.Header.Set("Accept", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, scenario := range [][2]string{{"https://example.org", "token"}, {"http://127.0.0.1:4444", "wrong"}} {
		if response := request(scenario[0], scenario[1], "/download"); response.Code != 403 {
			t.Fatalf("untrusted request: %d", response.Code)
		}
	}
	for _, rel := range []string{"media/source.png", "exports/../../project.json", "exports/missing.zip"} {
		form.Set("kind", "export")
		form.Set("path", rel)
		if response := request("http://127.0.0.1:4444", "token", "/download"); response.Code != 400 {
			t.Fatalf("accepted %s: %d", rel, response.Code)
		}
	}
	if called != 0 {
		t.Fatal("invalid requests opened a native dialog")
	}
	form.Set("kind", "plan")
	response := request("http://127.0.0.1:4444", "token", "/download")
	var result struct{ Saved bool }
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Saved {
		t.Fatal("cancel did not return a JSON cancellation")
	}
	h.saveFile = func(desktop.SaveFileOptions) (string, error) { return "", errors.New("dialog unavailable") }
	response = request("http://127.0.0.1:4444", "token", "/download")
	if response.Code != 400 || !strings.Contains(response.Body.String(), "dialog unavailable") {
		t.Fatal("dialog failure reported success")
	}
	h.saveFile = func(desktop.SaveFileOptions) (string, error) { return filepath.Join(t.TempDir(), "wrong.png"), nil }
	response = request("http://127.0.0.1:4444", "token", "/download")
	if response.Code != 400 || !strings.Contains(response.Body.String(), ".json extension") {
		t.Fatal("mismatched save extension was accepted")
	}
	h.saveMu.Lock()
	response = request("http://127.0.0.1:4444", "token", "/download")
	h.saveMu.Unlock()
	if response.Code != 400 || !strings.Contains(response.Body.String(), "finish the open Save dialog") {
		t.Fatal("nested save dialog was allowed")
	}
}

type interruptedExport struct{}

func (interruptedExport) Read(p []byte) (int, error) { return copy(p, "partial"), io.ErrUnexpectedEOF }

func TestExportCopyPreservesDestinationOnFailureAndProtectsStorage(t *testing.T) {
	s := testStore(t)
	p := createTestProject(t, s)
	output := filepath.Join(t.TempDir(), "save.zip")
	if err := os.WriteFile(output, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := saveExportCopy(s.path, output, interruptedExport{}); err == nil {
		t.Fatal("accepted interrupted copy")
	}
	if data, _ := os.ReadFile(output); string(data) != "original" {
		t.Fatal("partial copy damaged the existing file")
	}
	entries, _ := os.ReadDir(filepath.Dir(output))
	if len(entries) != 1 {
		t.Fatal("partial temporary file was retained")
	}
	projectFile := filepath.Join(s.path, shotText(p, "id"), "project.json")
	before, _ := os.ReadFile(projectFile)
	if err := saveExportCopy(s.path, projectFile, strings.NewReader("damage")); err == nil {
		t.Fatal("allowed replacing project storage")
	}
	after, _ := os.ReadFile(projectFile)
	if !bytes.Equal(before, after) {
		t.Fatal("project was changed")
	}
	t.Run("symlink", func(t *testing.T) {
		alias := filepath.Join(filepath.Dir(output), "alias.zip")
		if err := os.Symlink(output, alias); err != nil {
			t.Skipf("host cannot create symlinks: %v", err)
		}
		if err := saveExportCopy(s.path, alias, strings.NewReader("damage")); err == nil {
			t.Fatal("followed a destination symlink")
		}
		if data, _ := os.ReadFile(output); string(data) != "original" {
			t.Fatal("symlink target was changed")
		}
	})
}
