package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type editingFixture struct {
	t     *testing.T
	store *ProjectStore
	id    string
	shots [2]string
}

func newEditingFixture(t *testing.T) *editingFixture {
	t.Helper()
	s := testStore(t)
	p, err := s.CreateTemplate(ProjectDocument{"title": rawValue("Synthetic editing regression"), "music_video": rawValue(true), "lyrics": rawValue("Exact punctuation “here”\n  Keep spaces"), "shots": rawValue([]ProjectDocument{{"title": rawValue("First"), "scene": rawValue("Verse")}, {"title": rawValue("Second"), "scene": rawValue("Chorus")}})})
	if err != nil {
		t.Fatal(err)
	}
	id := shotText(p, "id")
	wav := fixtureWAV(100)
	p, err = s.ImportMedia(id, "", "soundtrack", "song.wav", int64(len(wav)), bytes.NewReader(wav), documentRevision(p), "", "")
	if err != nil {
		t.Fatal(err)
	}
	shots := list(p, "shots")
	f := &editingFixture{t: t, store: s, id: id}
	for i, shot := range shots {
		f.shots[i] = shotText(shot, "id")
		data := []byte{0, 0, 0, 20, 'f', 't', 'y', 'p', byte(i)}
		digest := sha256.Sum256(data)
		rel := "media/" + f.shots[i] + "/source.mp4"
		path := filepath.Join(s.path, id, filepath.FromSlash(rel))
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		shot["assets"] = rawValue([]ProjectDocument{{"id": rawValue("source"), "kind": rawValue("clip"), "path": rawValue(rel), "sha256": rawValue(hex.EncodeToString(digest[:])), "duration_seconds": rawValue(8), "in_seconds": rawValue(0), "out_seconds": rawValue(8), "take": rawValue(1)}})
		shot["selected_clip"] = rawValue("source")
	}
	p["shots"] = rawValue(shots)
	dir, err := projectDirectory(s.root, id)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if err = writeProjectDocument(dir, p); err != nil {
		t.Fatal(err)
	}
	return f
}
func (f *editingFixture) load() ProjectDocument {
	f.t.Helper()
	p, err := f.store.Load(f.id)
	if err != nil {
		f.t.Fatal(err)
	}
	return p
}
func (f *editingFixture) rev() int { return documentRevision(f.load()) }
func (f *editingFixture) section(label string, start, end float64) string {
	f.t.Helper()
	p, err := f.store.SaveSection(f.id, f.rev(), ProjectDocument{"label": rawValue(label), "kind": rawValue("none"), "start_seconds": rawValue(start), "end_seconds": rawValue(end)})
	if err != nil {
		f.t.Fatal(err)
	}
	for _, section := range list(p, "music_sections") {
		if shotText(section, "label") == label {
			return shotText(section, "id")
		}
	}
	f.t.Fatal("missing section")
	return ""
}
func (f *editingFixture) edit(section string, coverage bool, raw ProjectDocument) ProjectDocument {
	f.t.Helper()
	p, err := f.store.SectionEdit(f.id, section, f.rev(), raw, coverage)
	if err != nil {
		f.t.Fatal(err)
	}
	return p
}
func (f *editingFixture) reject(run func() error) {
	f.t.Helper()
	path := filepath.Join(f.store.path, f.id, "project.json")
	before, err := os.ReadFile(path)
	if err != nil {
		f.t.Fatal(err)
	}
	if err = run(); err == nil {
		f.t.Fatal("accepted invalid operation")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		f.t.Fatal("rejected operation changed stored project")
	}
}
func sourceRange(shot string, start, end float64) ProjectDocument {
	return ProjectDocument{"operation": rawValue("add"), "source_shot": rawValue(shot), "source_asset": rawValue("source"), "in_seconds": rawValue(start), "out_seconds": rawValue(end)}
}

func TestCoverageCollectionPreservesAssemblyAndEditorialNotes(t *testing.T) {
	f := newEditingFixture(t)
	sid := f.section("Verse", 60, 78)
	beforeShots := jsonText(list(f.load(), "shots"))
	p := f.edit(sid, false, sourceRange(f.shots[0], 1, 7))
	section, _ := findSection(p, sid)
	coverage := list(section, "coverage")[0]
	cid := shotText(coverage, "id")
	if number(coverage, "in_seconds") != 0 || number(coverage, "out_seconds") != 8 {
		t.Fatal("assembly did not collect full source handles")
	}
	assembly := jsonText(list(section, "clips"))
	f.edit(sid, true, ProjectDocument{"operation": rawValue("update"), "coverage": rawValue(cid), "in_seconds": rawValue(2), "out_seconds": rawValue(4), "angle": rawValue("Low angle"), "note": rawValue("Check tail"), "music_tag": rawValue("pre-chorus")})
	beforeDuplicate, err := os.ReadFile(filepath.Join(f.store.path, f.id, "project.json"))
	if err != nil {
		t.Fatal(err)
	}
	p = f.edit(sid, true, sourceRange(f.shots[0], 0, 8))
	afterDuplicate, err := os.ReadFile(filepath.Join(f.store.path, f.id, "project.json"))
	if err != nil || !bytes.Equal(beforeDuplicate, afterDuplicate) {
		t.Fatal("duplicate collection created a revision or undo entry")
	}
	section, _ = findSection(p, sid)
	rows := list(section, "coverage")
	if len(rows) != 1 || number(rows[0], "in_seconds") != 2 || shotText(rows[0], "note") != "Check tail" || shotText(rows[0], "music_tag") != "pre-chorus" || jsonText(list(section, "clips")) != assembly {
		t.Fatal("duplicate collection changed notes, preferred range or assembly")
	}
	if jsonText(list(p, "shots")) != beforeShots {
		t.Fatal("coverage edited the storyboard cut")
	}
	f.reject(func() error {
		_, err := f.store.SectionEdit(f.id, sid, f.rev(), ProjectDocument{"operation": rawValue("remove"), "coverage": rawValue(cid)}, true)
		return err
	})
	entry := shotText(list(section, "clips")[0], "id")
	f.edit(sid, false, ProjectDocument{"operation": rawValue("remove"), "entry": rawValue(entry)})
	p = f.edit(sid, true, ProjectDocument{"operation": rawValue("remove"), "coverage": rawValue(cid)})
	section, _ = findSection(p, sid)
	if len(list(section, "coverage")) != 0 {
		t.Fatal("coverage remove failed")
	}
	p, err = f.store.UndoSection(f.id, f.rev())
	if err != nil {
		t.Fatal(err)
	}
	section, _ = findSection(p, sid)
	if len(list(section, "coverage")) != 1 || len(list(section, "clips")) != 0 {
		t.Fatal("undo restored unrelated assembly edits")
	}
}

func TestCoverageReservationSurvivesTrackReplacementAndBlocksDuplicateBytes(t *testing.T) {
	f := newEditingFixture(t)
	sid := f.section("Verse", 0, 18)
	other := f.section("Chorus", 20, 38)
	p := f.edit(sid, true, sourceRange(f.shots[0], 1, 7))
	section, _ := findSection(p, sid)
	cid := shotText(list(section, "coverage")[0], "id")
	f.edit(sid, true, ProjectDocument{"operation": rawValue("update"), "coverage": rawValue(cid), "exclusive": rawValue(true)})
	for _, coverage := range []bool{true, false} {
		f.reject(func() error {
			_, err := f.store.SectionEdit(f.id, other, f.rev(), sourceRange(f.shots[0], 0, 4), coverage)
			return err
		})
	}
	f.reject(func() error {
		_, err := f.store.PlaceClip(f.id, f.shots[0], "source", f.shots[1], f.rev(), 0, 4, 8)
		return err
	})
	f.reject(func() error {
		_, err := f.store.UpdateShot(f.id, f.shots[0], f.rev(), ProjectDocument{"scene": rawValue("Changed scene")})
		return err
	})
	// A second asset with the same bytes cannot bypass the reservation.
	p = f.load()
	shots := list(p, "shots")
	duplicate := list(shots[0], "assets")[0]
	duplicate["id"] = rawValue("duplicate-bytes")
	shots[1]["assets"] = rawValue(append(list(shots[1], "assets"), duplicate))
	p["shots"] = rawValue(shots)
	dir, err := projectDirectory(f.store.root, f.id)
	if err != nil {
		t.Fatal(err)
	}
	err = writeProjectDocument(dir, p)
	dir.Close()
	if err != nil {
		t.Fatal(err)
	}
	f.reject(func() error {
		_, err := f.store.Choose(f.id, f.shots[1], "clip", "duplicate-bytes", false, f.rev())
		return err
	})
	oldTrack := object(p, "soundtrack")
	wav := fixtureWAV(90)
	if _, err = f.store.ImportMedia(f.id, "", "soundtrack", "replacement.wav", int64(len(wav)), bytes.NewReader(wav), f.rev(), "", ""); err != nil {
		t.Fatal(err)
	}
	f.reject(func() error {
		_, err := f.store.SectionEdit(f.id, other, f.rev(), sourceRange(f.shots[0], 0, 4), true)
		return err
	})
	f.edit(sid, true, ProjectDocument{"operation": rawValue("update"), "coverage": rawValue(cid), "exclusive": rawValue(false)})
	p, err = f.store.PlaceClip(f.id, f.shots[0], "source", f.shots[1], f.rev(), 0, 4, 8)
	if err != nil {
		t.Fatal("release on historical soundtrack failed", err)
	}
	if shotText(list(p, "soundtrack_history")[0], "sha256") != shotText(oldTrack, "sha256") {
		t.Fatal("replacement lost original recording")
	}
}

func TestInvalidSectionInputsAndPortionBatchesNeverWrite(t *testing.T) {
	f := newEditingFixture(t)
	sid := f.section("Chorus", 60, 78)
	for _, bad := range []json.RawMessage{rawValue(true), rawValue("0"), json.RawMessage(`null`), json.RawMessage(`NaN`)} {
		f.reject(func() error {
			_, err := f.store.SaveSection(f.id, f.rev(), ProjectDocument{"label": rawValue("Other"), "kind": rawValue("none"), "start_seconds": bad, "end_seconds": rawValue(20)})
			return err
		})
		f.reject(func() error {
			row := sourceRange(f.shots[0], 0, 4)
			row["in_seconds"] = bad
			_, err := f.store.SectionEdit(f.id, sid, f.rev(), row, true)
			return err
		})
	}
	f.edit(sid, false, sourceRange(f.shots[0], 0, 4))
	section, _ := findSection(f.load(), sid)
	cid := shotText(list(section, "coverage")[0], "id")
	for key, bad := range map[string]json.RawMessage{"exclusive": rawValue("yes"), "include_in_handoff": rawValue(1), "in_seconds": rawValue(8), "out_seconds": rawValue(9), "music_tag": rawValue("imaginary")} {
		f.reject(func() error {
			_, err := f.store.SectionEdit(f.id, sid, f.rev(), ProjectDocument{"operation": rawValue("update"), "coverage": rawValue(cid), key: bad}, true)
			return err
		})
	}
	for _, span := range []float64{0, -1, math.NaN(), math.Inf(1), .01} {
		f.reject(func() error { _, err := f.store.SplitSections(f.id, f.rev(), 0, 60, span, "Part", "none"); return err })
	}
	for _, bounds := range [][2]float64{{-1, 10}, {0, 101}, {0, 61}} {
		f.reject(func() error {
			_, err := f.store.SplitSections(f.id, f.rev(), bounds[0], bounds[1], 15, "Part", "none")
			return err
		})
	}
	before := f.load()
	p, err := f.store.SplitSections(f.id, f.rev(), .25, 32.5, 15, "Part", "none")
	if err != nil {
		t.Fatal(err)
	}
	sections := list(p, "music_sections")
	for i, want := range [][2]float64{{.25, 15.25}, {15.25, 30.25}, {30.25, 32.5}} {
		if number(sections[i], "start_seconds") != want[0] || number(sections[i], "end_seconds") != want[1] {
			t.Fatal("fractional split boundary drift")
		}
	}
	if jsonText(sections[3]) != jsonText(list(before, "music_sections")[0]) || jsonText(p["shots"]) != jsonText(before["shots"]) || shotText(p, "lyrics") != shotText(before, "lyrics") {
		t.Fatal("split changed existing sections, cut or lyrics")
	}
	f.reject(func() error {
		_, err := f.store.SplitSections(f.id, f.rev(), .25, 32.5, 15, "Part", "none")
		return err
	})
	p, err = f.store.UndoSection(f.id, f.rev())
	if err != nil || jsonText(p["music_sections"]) != jsonText(before["music_sections"]) {
		t.Fatal("split undo changed existing sections", err)
	}
}

func TestPlacementsFlattenLineageAndUndoProtectsLaterSelection(t *testing.T) {
	f := newEditingFixture(t)
	source := list(list(f.load(), "shots")[0], "assets")[0]
	path := filepath.Join(f.store.path, f.id, filepath.FromSlash(shotText(source, "path")))
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.store.PlaceClip(f.id, f.shots[0], "source", f.shots[1], f.rev(), 1, 7, 8)
	if err != nil {
		t.Fatal(err)
	}
	use := selectedAsset(list(p, "shots")[1], "clip")
	p, err = f.store.PlaceClip(f.id, f.shots[1], shotText(use, "id"), f.shots[0], f.rev(), 2, 4, 8)
	if err != nil {
		t.Fatal(err)
	}
	ref := object(selectedAsset(list(p, "shots")[0], "clip"), "source_ref")
	if shotText(ref, "shot_id") != f.shots[0] || shotText(ref, "asset_id") != "source" {
		t.Fatal("placement alias did not flatten to original")
	}
	p, err = f.store.UndoPlacement(f.id, f.rev())
	if err != nil || shotText(list(p, "shots")[0], "selected_clip") != "source" {
		t.Fatal("undo did not restore prior selection", err)
	}
	if _, err = f.store.Choose(f.id, f.shots[1], "clip", "source", false, f.rev()); err != nil {
		t.Fatal(err)
	}
	f.reject(func() error { _, err := f.store.UndoPlacement(f.id, f.rev()); return err })
	for _, bounds := range [][2]float64{{-1, 4}, {0, 9}, {4, 4}, {math.NaN(), 4}} {
		f.reject(func() error {
			_, err := f.store.PlaceClip(f.id, f.shots[0], "source", f.shots[1], f.rev(), bounds[0], bounds[1], 8)
			return err
		})
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("placement changed source bytes")
	}
}

func TestLargeEpisodeAndDirectionContextCompatibility(t *testing.T) {
	s := testStore(t)
	shots := make([]ProjectDocument, 600)
	for i := range shots {
		shots[i] = ProjectDocument{"title": rawValue("Episode shot"), "composition": rawValue("A quiet platform.")}
	}
	p, err := s.CreateTemplate(ProjectDocument{"title": rawValue("Long episode"), "shots": rawValue(shots)})
	if err != nil {
		t.Fatal(err)
	}
	id := shotText(p, "id")
	shot := list(p, "shots")[0]
	basis := directionBasis(p, shot)
	p, err = s.UpdateProject(id, documentRevision(p), ProjectDocument{"brief": rawValue("Different story intent")})
	if err != nil {
		t.Fatal(err)
	}
	if directionBasis(p, list(p, "shots")[0]) == basis {
		t.Fatal("brief did not invalidate direction basis")
	}
	basis = directionBasis(p, list(p, "shots")[0])
	sid := shotText(list(p, "shots")[0], "id")
	order := make([]string, len(shots))
	for i, s := range list(p, "shots") {
		order[i] = shotText(s, "id")
	}
	order[0], order[1] = order[1], order[0]
	p, err = s.Reorder(id, documentRevision(p), order)
	if err != nil {
		t.Fatal(err)
	}
	shot, err = findShot(p, sid)
	if err != nil {
		t.Fatal(err)
	}
	if directionBasis(p, shot) == basis {
		t.Fatal("sequence change did not invalidate direction basis")
	}
	if _, err = s.Insert(id, sid, "after", documentRevision(p)); err == nil || !strings.Contains(err.Error(), "600") {
		t.Fatal("episode shot limit not enforced", err)
	}
}
