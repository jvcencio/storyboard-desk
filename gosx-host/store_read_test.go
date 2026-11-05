package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReadProjectPreservesExistingData(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "project_1")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"id":"superseded","id":"project_1","revision":7,"title":"Legacy \ud800 雪",
		"shots":[{"id":"old","id":"s","assets":[{"path":"media/take.mp4"}],"future":{"v":1}}],
		"future":{"enabled":true, "large":900719925474099312345, "nullable":null}}`)
	path := filepath.Join(dir, "project.json")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	project, err := readStoredProject(root, "project_1")
	if err != nil {
		t.Fatal(err)
	}
	var want ProjectDocument
	if err := json.Unmarshal(original, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ProjectDocument(project), want) {
		t.Fatal("project reader changed legacy keys or raw values")
	}
	var wantShots []ProjectDocument
	if err := json.Unmarshal(want["shots"], &wantShots); err != nil {
		t.Fatal(err)
	}
	shots, err := documentShots(project)
	if err != nil || !reflect.DeepEqual(shots, wantShots) {
		t.Fatalf("shot reader changed legacy keys or raw values: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(original) {
		t.Fatal("inspection changed project")
	}
}

func TestReadProjectRejectsBadInputWithoutCreatingFiles(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"", "../escape", "x/y", ".", strings.Repeat("x", 65)} {
		if _, err := readStoredProject(root, id); err == nil {
			t.Errorf("accepted id %q", id)
		}
	}
	missing := filepath.Join(root, "missing-root")
	if _, err := readStoredProject(missing, "p"); err == nil {
		t.Fatal("accepted missing root")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("created missing root")
	}
	dir := filepath.Join(root, "p")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "project.json")
	for _, content := range []string{
		`not json`, `null`, `{"id":"other","revision":1,"shots":[]}`,
		`{"id":"p","revision":"1","shots":[]}`, `{"id":"p","revision":1,"shots":null}`,
		`{"id":"p","revision":1,"shots":[]} {}`,
		`{"id":"p","revision":1,"shots":[{"id":"s","assets":null}]}`,
		`{"id":"p","revision":1,"shots":[{"id":"s","assets":{}}]}`,
		`{"id":"p","revision":1,"shots":[{"id":"s","assets":[1,]}]}`,
	} {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readStoredProject(root, "p"); err == nil {
			t.Errorf("accepted %s", content)
		}
	}
}

func TestReadProjectDoesNotFollowProjectDirectorySymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "p")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := readStoredProject(root, "p"); err == nil {
		t.Fatal("accepted symlink project directory")
	}
}

func TestReadProjectDoesNotFollowProjectFileSymlink(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "p")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "project.json")
	if err := os.WriteFile(target, []byte(`{"id":"p","revision":1,"shots":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "project.json")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := readStoredProject(root, "p"); err == nil {
		t.Fatal("accepted symlink project file")
	}
}

func TestReadLegacyProjectFixture(t *testing.T) {
	root, id := filepath.Join("testdata", "legacy", "projects"), legacyFixtureID(t)
	project, err := readStoredProject(root, id)
	if err != nil {
		t.Fatal(err)
	}
	var title string
	if err := json.Unmarshal(project["title"], &title); err != nil || title != "Existing 雪 project" {
		t.Fatalf("title %q: %v", title, err)
	}
	var shots []map[string]json.RawMessage
	if err := json.Unmarshal(project["shots"], &shots); err != nil || len(shots) != 2 {
		t.Fatalf("shots: %s", project["shots"])
	}
}
