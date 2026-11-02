//go:build windows

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func directoryJunction(t *testing.T, link, target string) {
	t.Helper()
	if output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Fatalf("create test junction: %v: %s", err, output)
	}
	t.Cleanup(func() { os.Remove(link) })
}

func TestWindowsRejectsProjectDirectoryJunction(t *testing.T) {
	s := testStore(t)
	p := createTestProject(t, s)
	outside := t.TempDir()
	p["id"] = rawValue("junction")
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(outside, "project.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	directoryJunction(t, filepath.Join(s.path, "junction"), outside)
	if projects, err := s.List(); err == nil {
		for _, project := range projects {
			if shotText(project, "id") == "junction" {
				t.Fatal("project listing followed an NTFS junction")
			}
		}
	}
	if _, err = s.Load("junction"); err == nil {
		t.Fatal("project load followed an NTFS junction")
	}
}

func TestWindowsRejectsMediaDirectoryJunction(t *testing.T) {
	s := testStore(t)
	p := createTestProject(t, s)
	id := shotText(p, "id")
	sid := shotText(list(p, "shots")[0], "id")
	outside := t.TempDir()
	directoryJunction(t, filepath.Join(s.path, id, "media"), outside)
	image := []byte("\x89PNG\r\n\x1a\njunction-fixture")
	if _, err := s.ImportMedia(id, sid, "keyframe", "fixture.png", int64(len(image)), bytes.NewReader(image), 1, "", ""); err == nil {
		t.Fatal("media import wrote through an NTFS junction")
	}
	files, err := os.ReadDir(outside)
	if err != nil || len(files) != 0 {
		t.Fatal("media import changed the junction target")
	}
	saved, err := s.Load(id)
	if err != nil || documentRevision(saved) != 1 {
		t.Fatal("rejected junction import changed the project")
	}
}
