package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func legacyFixtureID(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "legacy", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		ID string `json:"project_id"`
	}
	if err = json.Unmarshal(data, &manifest); err != nil || manifest.ID == "" {
		t.Fatal("invalid fixture manifest", err)
	}
	return manifest.ID
}

func legacyFixtureCopy(t *testing.T) (string, string) {
	t.Helper()
	source := filepath.Join("testdata", "legacy", "projects")
	root := t.TempDir()
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(root, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	return root, legacyFixtureID(t)
}

func TestLegacyFixtureIntegrityAndUnchangedMedia(t *testing.T) {
	root, id := legacyFixtureCopy(t)
	manifestRaw, err := os.ReadFile(filepath.Join("testdata", "legacy", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Files map[string]string `json:"file_sha256"`
	}
	if err = json.Unmarshal(manifestRaw, &manifest); err != nil {
		t.Fatal(err)
	}
	check := func(project bool) {
		for name, want := range manifest.Files {
			if !project && strings.HasSuffix(name, "project.json") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(raw)
			if hex.EncodeToString(digest[:]) != want {
				t.Fatalf("legacy file changed: %s", name)
			}
		}
	}
	check(true)
	s, err := OpenProjectStore(root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	shot := list(p, "shots")[0]
	if !imageApproved(p, shot) {
		t.Fatal("legacy approval became stale without an edit")
	}
	p, err = s.UpdateShot(id, "first", documentRevision(p), ProjectDocument{"title": rawValue("Edited in GoSX")})
	if err != nil {
		t.Fatal(err)
	}
	if !imageApproved(p, list(p, "shots")[0]) {
		t.Fatal("title edit invalidated legacy visual approval")
	}
	p, err = s.Insert(id, "first", "after", documentRevision(p))
	if err != nil {
		t.Fatal(err)
	}
	shots := list(p, "shots")
	if len(shots) != 3 || shotText(shots[2], "id") != "second" || len(list(shots[1], "assets")) != 0 || !nullJSON(shots[1]["approval"]) {
		t.Fatal("insert copied media/approval or lost identity")
	}
	for _, doc := range []ProjectDocument{p, shots[0]} {
		if !strings.Contains(string(doc["future"]), "900719925474099312345") {
			t.Fatal("opaque integer lost precision")
		}
	}
	p, err = s.UpdateShot(id, "first", documentRevision(p), ProjectDocument{"composition": rawValue("Different framing")})
	if err != nil {
		t.Fatal(err)
	}
	if imageApproved(p, list(p, "shots")[0]) {
		t.Fatal("visual edit retained stale approval")
	}
	check(false)
}
