package main

import (
    "encoding/json"
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func TestReadProjectPreservesExistingData(t *testing.T) {
    root := t.TempDir()
    dir := filepath.Join(root, "project_1")
    if err := os.Mkdir(dir, 0700); err != nil { t.Fatal(err) }
    original := []byte(`{"id":"project_1","revision":7,"title":"Test","shots":[{"id":"s","assets":[{"path":"media/take.mp4"}],"future":{"v":1}}],"future":{"enabled":true}}`)
    path := filepath.Join(dir, "project.json")
    if err := os.WriteFile(path, original, 0600); err != nil { t.Fatal(err) }
    project, err := readStoredProject(root, "project_1")
    if err != nil { t.Fatal(err) }
    if string(project["future"]) != `{"enabled":true}` { t.Fatalf("unknown fields lost: %s", project["future"]) }
    var shots []map[string]json.RawMessage
    if err := json.Unmarshal(project["shots"], &shots); err != nil || string(shots[0]["future"]) != `{"v":1}` { t.Fatalf("shots lost: %s: %v", project["shots"], err) }
    after, err := os.ReadFile(path); if err != nil || string(after) != string(original) { t.Fatal("inspection changed project") }
}

func TestReadProjectRejectsBadInputWithoutCreatingFiles(t *testing.T) {
    root := t.TempDir()
    for _, id := range []string{"", "../escape", "x/y", ".", strings.Repeat("x", 65)} {
        if _, err := readStoredProject(root, id); err == nil { t.Errorf("accepted id %q", id) }
    }
    missing := filepath.Join(root, "missing-root")
    if _, err := readStoredProject(missing, "p"); err == nil { t.Fatal("accepted missing root") }
    if _, err := os.Stat(missing); !os.IsNotExist(err) { t.Fatal("created missing root") }
    dir := filepath.Join(root, "p"); if err := os.Mkdir(dir, 0700); err != nil { t.Fatal(err) }
    path := filepath.Join(dir, "project.json")
    for _, content := range []string{`not json`, `null`, `{"id":"other","revision":1,"shots":[]}`, `{"id":"p","revision":"1","shots":[]}`, `{"id":"p","revision":1,"shots":null}`, `{"id":"p","revision":1,"shots":[]} {}`} {
        if err := os.WriteFile(path, []byte(content), 0600); err != nil { t.Fatal(err) }
        if _, err := readStoredProject(root, "p"); err == nil { t.Errorf("accepted %s", content) }
    }
}

func TestReadProjectDoesNotFollowProjectDirectorySymlink(t *testing.T) {
    root := t.TempDir(); outside := t.TempDir()
    if err := os.Symlink(outside, filepath.Join(root, "p")); err != nil { t.Skipf("symlink unavailable: %v", err) }
    if _, err := readStoredProject(root, "p"); err == nil { t.Fatal("accepted symlink project directory") }
}

func TestReadProjectDoesNotFollowProjectFileSymlink(t *testing.T) {
    root := t.TempDir()
    dir := filepath.Join(root, "p")
    if err := os.Mkdir(dir, 0700); err != nil { t.Fatal(err) }
    target := filepath.Join(t.TempDir(), "project.json")
    if err := os.WriteFile(target, []byte(`{"id":"p","revision":1,"shots":[]}`), 0600); err != nil { t.Fatal(err) }
    if err := os.Symlink(target, filepath.Join(dir, "project.json")); err != nil { t.Skipf("symlink unavailable: %v", err) }
    if _, err := readStoredProject(root, "p"); err == nil { t.Fatal("accepted symlink project file") }
}

func TestReadPythonProjectFixture(t *testing.T) {
    root, id := os.Getenv("STORYBOARD_TEST_DATA_DIR"), os.Getenv("STORYBOARD_TEST_PROJECT_ID")
    if root == "" || id == "" { t.Skip("Python fixture not configured") }
    project, err := readStoredProject(root, id); if err != nil { t.Fatal(err) }
    var title string; if err := json.Unmarshal(project["title"], &title); err != nil || title != "Test" { t.Fatalf("title %q: %v", title, err) }
    var shots []map[string]json.RawMessage; if err := json.Unmarshal(project["shots"], &shots); err != nil || len(shots) != 1 { t.Fatalf("shots: %s", project["shots"]) }
}
