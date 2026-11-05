package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func testStore(t *testing.T) *ProjectStore {
	t.Helper()
	s, err := OpenProjectStore(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func createTestProject(t *testing.T, s *ProjectStore) ProjectDocument {
	t.Helper()
	p, err := s.Create("Test", "A brief", "16:9")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestStorePreservesLegacyDataAcrossEditsAndReopen(t *testing.T) {
	s := testStore(t)
	p := createTestProject(t, s)
	id := shotText(p, "id")
	shots, _ := documentShots(p)
	sid := shotText(shots[0], "id")
	// Represent unported and future data, with a number too large for float64.
	p["future"] = json.RawMessage(`{"large":900719925474099312345,"unicode":"雪 🎬"}`)
	p["music_video"] = rawValue(true)
	p["lyrics"] = rawValue("Original lyrics — é\n  Keep spacing")
	p["soundtrack"] = rawValue(map[string]any{"path": "media/song.wav", "duration_seconds": 30})
	p["music_sections"] = json.RawMessage(`[{"id":"section","label":"Verse","clips":[],"coverage":[]}]`)
	shots[0]["future"] = p["future"]
	shots[0]["scene"] = rawValue("Verse")
	shots[0]["assets"] = json.RawMessage(`[{"id":"take","path":"media/original.mp4","sha256":"unchanged","future":true}]`)
	shots[0]["selected_clip"] = rawValue("take")
	shots[0]["approval"] = json.RawMessage(`{"fingerprint":"keep-receipt","at":"yesterday"}`)
	shots[0]["direction_review"] = json.RawMessage(`{"current_basis":"preserved"}`)
	p["shots"] = rawValue(shots)
	dir, err := projectDirectory(s.root, id)
	if err != nil {
		t.Fatal(err)
	}
	if err = writeProjectDocument(dir, p); err != nil {
		t.Fatal(err)
	}
	dir.Close()
	media := filepath.Join(s.path, id, "media")
	if err = os.Mkdir(media, 0700); err != nil {
		t.Fatal(err)
	}
	mediaFile := filepath.Join(media, "original.mp4")
	original := []byte("original source bytes")
	if err = os.WriteFile(mediaFile, original, 0600); err != nil {
		t.Fatal(err)
	}
	p, err = s.UpdateShot(id, sid, 1, ProjectDocument{"title": rawValue("Go edit"), "composition": rawValue("New framing")})
	if err != nil {
		t.Fatal(err)
	}
	p, err = s.Insert(id, sid, "before", 2)
	if err != nil {
		t.Fatal(err)
	}
	shots, _ = documentShots(p)
	inserted := shotText(shots[0], "id")
	if shotText(shots[0], "scene") != "Verse" || string(shots[0]["duration"]) != "8" || string(shots[0]["assets"]) != "[]" || string(shots[0]["approval"]) != "null" {
		t.Fatalf("bad music draft: %s", p["shots"])
	}
	p, err = s.Reorder(id, 3, []string{sid, inserted})
	if err != nil {
		t.Fatal(err)
	}
	p, err = s.UpdateProject(id, 4, ProjectDocument{"brief": rawValue("Updated brief")})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenProjectStore(s.path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	p, err = reopened.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	shots, _ = documentShots(p)
	for _, doc := range []ProjectDocument{p, shots[0]} {
		if !strings.Contains(string(doc["future"]), "900719925474099312345") || !strings.Contains(string(doc["future"]), "雪 🎬") {
			t.Fatal("unknown data lost")
		}
	}
	if documentRevision(p) != 5 || shotText(shots[0], "id") != sid || shotText(shots[0], "title") != "Go edit" || shotText(shots[0], "selected_clip") != "take" {
		t.Fatalf("bad saved state: %s", p["shots"])
	}
	if !strings.Contains(string(shots[0]["approval"]), "keep-receipt") || !strings.Contains(string(shots[0]["direction_review"]), "preserved") || shotText(p, "lyrics") != "Original lyrics — é\n  Keep spacing" {
		t.Fatal("unported data changed")
	}
	data, err := os.ReadFile(mediaFile)
	if err != nil || string(data) != string(original) {
		t.Fatal("media source changed")
	}
}

func TestStoreRejectsInvalidAndStaleCommandsWithoutWriting(t *testing.T) {
	s := testStore(t)
	p := createTestProject(t, s)
	id := shotText(p, "id")
	shots, _ := documentShots(p)
	sid := shotText(shots[0], "id")
	path := filepath.Join(s.path, id, "project.json")
	before, _ := os.ReadFile(path)
	cases := []struct {
		name string
		run  func() error
	}{
		{"stale", func() error { _, e := s.UpdateShot(id, sid, 0, ProjectDocument{"title": rawValue("stale")}); return e }},
		{"missing shot", func() error { _, e := s.UpdateShot(id, "missing", 1, ProjectDocument{}); return e }},
		{"media injection", func() error { _, e := s.UpdateShot(id, sid, 1, ProjectDocument{"assets": rawValue([]any{})}); return e }},
		{"unknown mutation", func() error { _, e := s.UpdateProject(id, 1, ProjectDocument{"future": rawValue(true)}); return e }},
		{"bad duration", func() error { _, e := s.UpdateShot(id, sid, 1, ProjectDocument{"duration": rawValue(61)}); return e }},
		{"invalid raw number", func() error {
			_, e := s.UpdateShot(id, sid, 1, ProjectDocument{"duration": json.RawMessage(`NaN`)})
			return e
		}},
		{"padded null", func() error {
			_, e := s.UpdateProject(id, 1, ProjectDocument{"music_video": json.RawMessage(` null `)})
			return e
		}},
		{"null title", func() error { _, e := s.UpdateShot(id, sid, 1, ProjectDocument{"title": rawValue(nil)}); return e }},
		{"long text", func() error {
			_, e := s.UpdateProject(id, 1, ProjectDocument{"brief": rawValue(strings.Repeat("雪", 12001))})
			return e
		}},
		{"bad aspect", func() error { _, e := s.UpdateProject(id, 1, ProjectDocument{"aspect": rawValue("bad")}); return e }},
		{"bad music flag", func() error {
			_, e := s.UpdateProject(id, 1, ProjectDocument{"music_video": rawValue("true")})
			return e
		}},
		{"bad insert", func() error { _, e := s.Insert(id, sid, "elsewhere", 1); return e }},
		{"missing anchor", func() error { _, e := s.Insert(id, "missing", "after", 1); return e }},
		{"remove by reorder", func() error { _, e := s.Reorder(id, 1, nil); return e }},
		{"replace by reorder", func() error { _, e := s.Reorder(id, 1, []string{"missing"}); return e }},
		{"traversal", func() error { _, e := s.Load("../escape"); return e }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.run() == nil {
				t.Fatal("accepted invalid command")
			}
			after, _ := os.ReadFile(path)
			if string(after) != string(before) {
				t.Fatal("failed command changed file")
			}
		})
	}
	if _, err := s.UpdateShot(id, sid, 1, ProjectDocument{"duration": rawValue(1.5), "dialogue": rawValue("雪")}); err != nil {
		t.Fatal(err)
	}
}

func TestStoreConcurrentCommandsOnlyOneRevisionWins(t *testing.T) {
	s := testStore(t)
	p := createTestProject(t, s)
	id := shotText(p, "id")
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.UpdateProject(id, 1, ProjectDocument{"brief": rawValue(fmt.Sprint(i))})
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrRevision) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflicts=%d", success, conflicts)
	}
	p, err := s.Load(id)
	if err != nil || documentRevision(p) != 2 {
		t.Fatalf("bad revision: %v %v", p, err)
	}
}

func TestStoreOwnershipAndUnavailablePaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing")
	if _, err := OpenProjectStore(root, false); err == nil {
		t.Fatal("created missing root implicitly")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("missing root was created")
	}
	s, err := OpenProjectStore(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if other, err := OpenProjectStore(root, false); err == nil {
		other.Close()
		t.Fatal("second writer accepted")
	}
	s.Close()
	if _, err = s.List(); err == nil {
		t.Fatal("closed store still usable")
	}
	reopened, err := OpenProjectStore(root, false)
	if err != nil {
		t.Fatal(err)
	}
	reopened.Close()
	if runtime.GOOS != "windows" {
		if err := checkStorageVolume("/Volumes/UnavailableStoryboardDisk/projects"); err == nil {
			t.Fatal("missing volume accepted")
		}
	}
}

func TestStoreRejectsSymlinksAndReplacedRoot(t *testing.T) {
	s := testStore(t)
	p := createTestProject(t, s)
	id := shotText(p, "id")
	path := filepath.Join(s.path, id, "project.json")
	outside := filepath.Join(t.TempDir(), "outside.json")
	data, _ := os.ReadFile(path)
	os.WriteFile(outside, data, 0600)
	if err := os.Symlink(outside, filepath.Join(s.path, "symlink-test")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := s.List(); err == nil {
		t.Fatal("listed a symlink")
	}
	os.Remove(filepath.Join(s.path, "symlink-test"))
	os.Remove(path)
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateProject(id, 1, ProjectDocument{"title": rawValue("escaped")}); err == nil {
		t.Fatal("followed project symlink")
	}
	after, _ := os.ReadFile(outside)
	if string(after) != string(data) {
		t.Fatal("changed outside file")
	}
	if runtime.GOOS == "windows" {
		return
	} // Windows may prevent renaming an open directory.
	moved := s.path + "-moved"
	if err := os.Rename(s.path, moved); err != nil {
		t.Fatal(err)
	}
	defer os.Rename(moved, s.path)
	if err := os.Mkdir(s.path, 0700); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(s.path)
	if _, err := s.Create("bad", "", "16:9"); err == nil {
		t.Fatal("wrote after root replacement")
	}
}

func TestStoreIgnoresCrashTempsAndRejectsMalformedProjects(t *testing.T) {
	s := testStore(t)
	p := createTestProject(t, s)
	id := shotText(p, "id")
	os.WriteFile(filepath.Join(s.path, id, ".project-interrupted.tmp"), []byte(`{"incomplete":`), 0600)
	os.Mkdir(filepath.Join(s.path, ".new-interrupted"), 0700)
	os.WriteFile(filepath.Join(s.path, ".new-interrupted", "project.json"), []byte(`bad`), 0600)
	list, err := s.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("crash temps affected list: %v", err)
	}
	shots, _ := documentShots(p)
	p["shots"] = rawValue(append(shots, shots[0]))
	data, _ := json.Marshal(p)
	os.WriteFile(filepath.Join(s.path, id, "project.json"), data, 0600)
	if _, err = s.List(); err == nil {
		t.Fatal("accepted duplicate shot IDs")
	}
}

func TestStoreSceneEditHonorsExistingCoverageReservation(t *testing.T) {
	s := testStore(t)
	p := createTestProject(t, s)
	id := shotText(p, "id")
	shots, _ := documentShots(p)
	sid := shotText(shots[0], "id")
	shots[0]["scene"] = rawValue("Verse")
	shots[0]["selected_clip"] = rawValue("take")
	shots[0]["assets"] = json.RawMessage(`[{"id":"take","sha256":"source"}]`)
	p["shots"] = rawValue(shots)
	p["music_sections"] = json.RawMessage(`[{"id":"section","label":"Verse","coverage":[{"source_sha256":"source","exclusive":true}],"clips":[]}]`)
	dir, _ := projectDirectory(s.root, id)
	if err := writeProjectDocument(dir, p); err != nil {
		t.Fatal(err)
	}
	dir.Close()
	before, _ := s.Load(id)
	if _, err := s.UpdateShot(id, sid, 1, ProjectDocument{"scene": rawValue("Chorus")}); err == nil {
		t.Fatal("invalidated reservation")
	}
	after, _ := s.Load(id)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("failed scene edit changed data")
	}
}

// This subprocess uses the real store/atomic replacement continuously. Killing
// it proves that OS ownership is released and the committed JSON stays readable.
func TestStorageWriterProcess(t *testing.T) {
	root := os.Getenv("SBD_WRITER_HELPER")
	if root == "" {
		t.Skip("subprocess helper")
	}
	s, err := OpenProjectStore(root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.Create("Crash fixture", "", "16:9")
	if err != nil {
		t.Fatal(err)
	}
	id := shotText(p, "id")
	fmt.Println(id)
	for i := 0; i < 100000; i++ {
		p, err = s.UpdateProject(id, documentRevision(p), ProjectDocument{"brief": rawValue(strings.Repeat("x", 12000))})
		if err != nil {
			t.Fatal(err)
		}
		if i == 5 {
			fmt.Println("committed")
		}
	}
}

func TestStoreSurvivesWriterProcessCrash(t *testing.T) {
	root := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestStorageWriterProcess$")
	cmd.Env = append(os.Environ(), "SBD_WRITER_HELPER="+root)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.ProcessState == nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
	}()
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() {
		t.Fatal("writer did not start")
	}
	id := scanner.Text()
	if !scanner.Scan() || scanner.Text() != "committed" {
		t.Fatal("writer did not commit")
	}
	if other, err := OpenProjectStore(root, false); err == nil {
		other.Close()
		t.Fatal("live writer not excluded")
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	s, err := OpenProjectStore(root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	if documentRevision(p) < 7 || len(shotText(p, "brief")) != 12000 {
		t.Fatal("committed data lost")
	}
	if _, err = s.UpdateProject(id, documentRevision(p), ProjectDocument{"title": rawValue("Recovered")}); err != nil {
		t.Fatal(err)
	}
}
