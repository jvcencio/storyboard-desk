package main

import (
	"bytes"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestBlockingMaterialsAndLegacySave(t *testing.T) {
	props, err := blockingRender(defaultBlockingScene(), "16:9", "camera")
	if err != nil {
		t.Fatal(err)
	}
	if materials := blockingMaterials(); len(props.SceneIR().Objects) < 10 || materials.clay.FragmentGLSL == "" || materials.clay.FragmentWGSL == "" || materials.marks.FragmentGLSL == "" {
		t.Fatal("missing compiled Selena scene")
	}
	root, id := legacyFixtureCopy(t)
	if fixture := os.Getenv("STORYBOARD_BLOCKING_NATIVE_FIXTURE"); fixture != "" {
		if err := os.CopyFS(fixture, os.DirFS(root)); err != nil {
			t.Fatal(err)
		}
	}
	store, err := OpenProjectStore(root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	project, err := store.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	shot, _ := findShot(project, "first")
	before := ProjectDocument{}
	_ = json.Unmarshal(rawValue(shot), &before)
	stripDerivedShots([]ProjectDocument{before})
	spec := defaultBlockingScene()
	base := blockingVersion(shot, shotText(project, "aspect"))
	project, err = store.saveBlocking(id, "first", documentRevision(project), base, &spec, nil)
	if err != nil {
		t.Fatal(err)
	}
	shot, _ = findShot(project, "first")
	for key, value := range before {
		if !bytes.Equal(value, shot[key]) {
			t.Fatalf("changed legacy shot field %s", key)
		}
	}
	var extended ProjectDocument
	_ = json.Unmarshal(shot["blocking_scene"], &extended)
	extended["future"] = json.RawMessage(`900719925474099312345`)
	actors := list(extended, "actors")
	actors[0]["future"] = json.RawMessage(`900719925474099312345`)
	extended["actors"] = rawValue(actors)
	_, err = store.mutateChecked(id, func(ProjectDocument) error { return nil }, func(p ProjectDocument) error {
		return editShotDocument(p, "first", func(s ProjectDocument) error { s["blocking_scene"] = rawValue(extended); return nil })
	})
	if err != nil {
		t.Fatal(err)
	}
	project, _ = store.Load(id)
	shot, _ = findShot(project, "first")
	base = blockingVersion(shot, shotText(project, "aspect"))
	spec.Actors[0].X = -2.25
	project, err = store.saveBlocking(id, "first", documentRevision(project), base, &spec, nil)
	if err != nil {
		t.Fatal(err)
	}
	shot, _ = findShot(project, "first")
	extended = object(shot, "blocking_scene")
	if string(extended["future"]) != "900719925474099312345" || string(list(extended, "actors")[0]["future"]) != "900719925474099312345" {
		t.Fatal("lost unknown large integers")
	}
	beforeFile, _ := os.ReadFile(filepath.Join(root, id, "project.json"))
	if _, err = store.saveBlocking(id, "first", documentRevision(project), base, &spec, nil); err != ErrRevision {
		t.Fatal("stale scene accepted", err)
	}
	h := &workspaceHandler{store: store, csrf: "blocking-test"}
	values := url.Values{"csrf": {"blocking-test"}, "project": {id}, "shot": {"first"}, "view": {"blocking"}, "revision": {strconv.Itoa(documentRevision(project))}, "blocking_base": {blockingVersion(shot, shotText(project, "aspect"))}}
	for key, value := range blockingFields(spec) {
		values.Set(key, value)
	}
	values.Set("camera.lens", "NaN")
	response := workspacePost(h, "/blocking/save", values)
	if response.Code != 400 || !strings.Contains(response.Body.String(), `aria-invalid="true"`) {
		t.Fatal("invalid camera was not rejected", response.Code)
	}
	afterFile, _ := os.ReadFile(filepath.Join(root, id, "project.json"))
	if !bytes.Equal(beforeFile, afterFile) {
		t.Fatal("rejected draft changed project bytes")
	}
}
