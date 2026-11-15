package main

import (
	"bytes"
	"testing"
)

// Importing something the project already holds changes nothing, so it must
// not raise the revision: every other open form would be told the project
// changed.
func TestDuplicateImportsDoNotRaiseTheRevision(t *testing.T) {
	store := testStore(t)
	project := createTestProject(t, store)
	id := shotText(project, "id")
	sid := shotText(list(project, "shots")[0], "id")
	image := []byte("\x89PNG\r\n\x1a\nreference")
	load := func() ProjectDocument {
		p, err := store.Load(id)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	importReference := func() ProjectDocument {
		p, err := store.importMedia(id, "", "reference", "face.png", int64(len(image)), bytes.NewReader(image), documentRevision(load()), "Face", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	first := importReference()
	second := importReference()
	if documentRevision(second) != documentRevision(first) || len(list(second, "visual_references")) != 1 {
		t.Fatalf("a duplicate reference raised the revision from %d to %d", documentRevision(first), documentRevision(second))
	}

	result := func() ProjectDocument {
		study := ProjectDocument{"integration": rawValue("blocking"), "job_id": rawValue("blocking-0123456789abcdef-0123456789abcdef")}
		p, err := store.importMedia(id, sid, "sketch", "blocking-study.png", int64(len(image)), bytes.NewReader(image), documentRevision(load()), "", "", study)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	first = result()
	second = result()
	if documentRevision(second) != documentRevision(first) || len(list(list(second, "shots")[0], "assets")) != 1 {
		t.Fatalf("a result imported twice raised the revision from %d to %d", documentRevision(first), documentRevision(second))
	}
}

// Binning the approved image ends its approval even when another take is
// selected at that moment. Otherwise restoring and reselecting the image
// brings the approval back without anyone approving.
func TestBinningTheApprovedImageEndsItsApproval(t *testing.T) {
	store := testStore(t)
	project := createTestProject(t, store)
	id := shotText(project, "id")
	sid := shotText(list(project, "shots")[0], "id")
	var err error
	add := func(content string) string {
		data := []byte("\x89PNG\r\n\x1a\n" + content)
		if project, err = store.importMedia(id, sid, "keyframe", content+".png", int64(len(data)), bytes.NewReader(data), documentRevision(project), "", "", nil); err != nil {
			t.Fatal(err)
		}
		assets := list(list(project, "shots")[0], "assets")
		return shotText(assets[len(assets)-1], "id")
	}
	step := func(next ProjectDocument, e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
		project = next
	}
	approved := func() bool {
		shot, _ := findShot(project, sid)
		return imageApproved(project, shot)
	}
	first, second := add("first"), add("second")
	step(store.Choose(id, sid, "keyframe", first, true, documentRevision(project)))
	if !approved() {
		t.Fatal("fixture: the first image is not approved")
	}
	step(store.Choose(id, sid, "keyframe", second, false, documentRevision(project)))
	step(store.Bin(id, sid, first, "wrong face", false, documentRevision(project)))
	step(store.Bin(id, sid, first, "", true, documentRevision(project)))
	step(store.Choose(id, sid, "keyframe", first, false, documentRevision(project)))
	if approved() {
		t.Fatal("an image that was binned came back approved without a new approval")
	}
	// Approving it again works as before.
	step(store.Choose(id, sid, "keyframe", first, true, documentRevision(project)))
	if !approved() {
		t.Fatal("a restored image could not be approved again")
	}
}
