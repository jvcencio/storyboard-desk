package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// blockingTestPoster is a 16:9 PNG with enough colours to pass the blank-frame
// check. Different seeds give different bytes.
func blockingTestPoster(t *testing.T, seed uint8) []byte {
	t.Helper()
	frame := image.NewRGBA(image.Rect(0, 0, 320, 180))
	for y := 0; y < 180; y++ {
		for x := 0; x < 320; x++ {
			frame.Set(x, y, color.RGBA{uint8(x) + seed, uint8(y), uint8((x + y) / 2), 255})
		}
	}
	var data bytes.Buffer
	if err := png.Encode(&data, frame); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

type blockingFixture struct {
	t       *testing.T
	store   *ProjectStore
	handler *workspaceHandler
	id, sid string
}

func newBlockingFixture(t *testing.T) blockingFixture {
	t.Helper()
	store := testStore(t)
	project := createTestProject(t, store)
	shots, _ := documentShots(project)
	return blockingFixture{t, store, &workspaceHandler{store: store, csrf: "token"}, shotText(project, "id"), shotText(shots[0], "id")}
}

func (f blockingFixture) load() (ProjectDocument, ProjectDocument) {
	f.t.Helper()
	project, err := f.store.Load(f.id)
	if err != nil {
		f.t.Fatal(err)
	}
	shot, err := findShot(project, f.sid)
	if err != nil {
		f.t.Fatal(err)
	}
	return project, shot
}

func (f blockingFixture) scene() blockingScene {
	f.t.Helper()
	_, shot := f.load()
	spec, err := readBlockingScene(shot)
	if err != nil {
		f.t.Fatal(err)
	}
	return spec
}

// post submits the blocking form as the page would, with edit applied to the
// saved fields first.
func (f blockingFixture) post(path string, edit func(url.Values)) *httptest.ResponseRecorder {
	f.t.Helper()
	project, shot := f.load()
	values := url.Values{"csrf": {"token"}, "project": {f.id}, "shot": {f.sid}, "view": {"blocking"},
		"revision": {strconv.Itoa(documentRevision(project))}, "blocking_base": {blockingVersion(shot, shotText(project, "aspect"))}}
	for key, value := range blockingFields(f.scene()) {
		values.Set(key, value)
	}
	if edit != nil {
		edit(values)
	}
	return workspacePost(f.handler, path, values)
}

func (f blockingFixture) capture(poster []byte) {
	f.t.Helper()
	project, shot := f.load()
	if _, err := f.store.saveBlocking(f.id, f.sid, documentRevision(project), blockingVersion(shot, shotText(project, "aspect")), nil, poster); err != nil {
		f.t.Fatal(err)
	}
}

func (f blockingFixture) sketches() []ProjectDocument {
	_, shot := f.load()
	found := []ProjectDocument{}
	for _, asset := range list(shot, "assets") {
		if shotText(asset, "kind") == "sketch" {
			found = append(found, asset)
		}
	}
	return found
}

func TestBlockingActorsCanBeAddedAndRemoved(t *testing.T) {
	f := newBlockingFixture(t)
	op := func(name string) func(url.Values) { return func(v url.Values) { v.Set("op", name) } }

	// An edit typed before pressing "Add actor" is saved with the new actor.
	response := f.post("/blocking/save", func(v url.Values) { v.Set("op", "add-actor"); v.Set("actor.0.x", "-3") })
	if response.Code != 303 {
		t.Fatalf("add actor: %d %s", response.Code, response.Body.String())
	}
	actors := f.scene().Actors
	if len(actors) != 3 || actors[0].X != -3 {
		t.Fatalf("after add: %+v", actors)
	}
	added := actors[2]
	if added.ID != "actor-c" || added.Name != "Actor C" || !blockingColour.MatchString(added.Colour) {
		t.Fatalf("new actor %+v", added)
	}
	for _, other := range actors[:2] {
		if colourName(other.Colour) == colourName(added.Colour) {
			t.Fatalf("new actor shares the colour name %q", colourName(added.Colour))
		}
		if other.X == added.X && other.Z == added.Z {
			t.Fatal("new actor stands on another actor")
		}
	}

	for len(f.scene().Actors) < blockingMaxActors {
		if response = f.post("/blocking/save", op("add-actor")); response.Code != 303 {
			t.Fatalf("fill stage: %d", response.Code)
		}
	}
	names := map[string]bool{}
	for _, actor := range f.scene().Actors {
		names[colourName(actor.Colour)] = true
	}
	if len(names) != blockingMaxActors {
		t.Fatalf("eight actors share colour names: %v", names)
	}
	before, _ := f.load()
	if response = f.post("/blocking/save", op("add-actor")); response.Code != 400 || !strings.Contains(response.Body.String(), "up to 8 actors") {
		t.Fatalf("ninth actor: %d", response.Code)
	}
	if after, _ := f.load(); documentRevision(after) != documentRevision(before) {
		t.Fatal("a refused actor changed the project")
	}

	if response = f.post("/blocking/save", op("remove-actor.0")); response.Code != 303 {
		t.Fatalf("remove: %d", response.Code)
	}
	if actors = f.scene().Actors; len(actors) != blockingMaxActors-1 || actors[0].ID != "actor-b" {
		t.Fatalf("after remove: %+v", actors)
	}
	for _, bad := range []string{"remove-actor.99", "remove-actor.-1", "remove-actor.x", "explode"} {
		if response = f.post("/blocking/save", op(bad)); response.Code != 400 {
			t.Fatalf("%s: %d", bad, response.Code)
		}
	}
	for len(f.scene().Actors) > 1 {
		if response = f.post("/blocking/save", op("remove-actor.0")); response.Code != 303 {
			t.Fatalf("empty stage: %d", response.Code)
		}
	}
	if response = f.post("/blocking/save", op("remove-actor.0")); response.Code != 400 || !strings.Contains(response.Body.String(), "at least one actor") {
		t.Fatalf("last actor: %d", response.Code)
	}
	// A removed identity is free again.
	f.post("/blocking/save", op("add-actor"))
	if actors = f.scene().Actors; len(actors) != 2 || actors[0].ID == actors[1].ID {
		t.Fatalf("re-added: %+v", actors)
	}
}

func TestBlockingPosterBecomesASketchTake(t *testing.T) {
	f := newBlockingFixture(t)
	if response := f.post("/blocking/sketch", nil); response.Code != 400 || len(f.sketches()) != 0 {
		t.Fatalf("sketch without a saved scene: %d", response.Code)
	}
	if response := f.post("/blocking/save", nil); response.Code != 303 {
		t.Fatalf("save: %d", response.Code)
	}
	if response := f.post("/blocking/sketch", nil); response.Code != 400 || len(f.sketches()) != 0 {
		t.Fatalf("sketch without a poster: %d", response.Code)
	}
	poster := blockingTestPoster(t, 0)
	f.capture(poster)
	if response := f.post("/blocking/sketch", nil); response.Code != 303 {
		t.Fatalf("use poster: %d %s", response.Code, response.Body.String())
	}
	project, shot := f.load()
	sketches := f.sketches()
	if len(sketches) != 1 {
		t.Fatalf("sketch takes: %d", len(sketches))
	}
	take := sketches[0]
	digest := sha256.Sum256(poster)
	study := object(take, "blocking_study")
	if shotText(take, "sha256") != hex.EncodeToString(digest[:]) || shotText(take, "provider") != "Blocking editor" ||
		shotText(study, "scene_version") != blockingVersion(shot, shotText(project, "aspect")) || len(list(study, "actors")) != 2 {
		t.Fatalf("take %s", jsonText(take))
	}
	if shotText(shot, "selected_sketch") != shotText(take, "id") {
		t.Fatal("the study was not selected on a shot without an approval")
	}
	file, err := f.store.OpenMedia(f.id, shotText(take, "path"))
	if err != nil {
		t.Fatal(err)
	}
	copied, _ := io.ReadAll(file)
	file.Close()
	if !bytes.Equal(copied, poster) {
		t.Fatal("the take is not the poster's bytes")
	}

	// The take is offered to image generation under a name that says what it is.
	labelled := false
	for _, row := range cloudImages(project) {
		labelled = labelled || (shotText(row, "id") == shotText(take, "id") && strings.Contains(shotText(row, "label"), "blocking study"))
	}
	if !labelled {
		t.Fatal("generation does not offer the blocking study as a reference")
	}

	// Using the same poster twice adds nothing.
	if response := f.post("/blocking/sketch", nil); response.Code != 303 || len(f.sketches()) != 1 {
		t.Fatalf("second use: %d, %d takes", response.Code, len(f.sketches()))
	}

	// After the scene changes the poster is stale and cannot become a take.
	if response := f.post("/blocking/save", func(v url.Values) { v.Set("actor.0.x", "2.5") }); response.Code != 303 {
		t.Fatalf("move: %d", response.Code)
	}
	if _, state := blockingPoster(f.load()); state != blockingPosterStale {
		t.Fatalf("poster state after an edit: %v", state)
	}
	if response := f.post("/blocking/sketch", nil); response.Code != 400 || len(f.sketches()) != 1 {
		t.Fatalf("stale poster: %d, %d takes", response.Code, len(f.sketches()))
	}

	// A new poster of the new scene becomes a second take.
	f.capture(blockingTestPoster(t, 40))
	if response := f.post("/blocking/sketch", nil); response.Code != 303 || len(f.sketches()) != 2 {
		t.Fatalf("second poster: %d, %d takes", response.Code, len(f.sketches()))
	}

	// A study in the bin is not silently duplicated.
	project, shot = f.load()
	latest := f.sketches()[1]
	if _, err = f.store.mutate(f.id, documentRevision(project), func(p ProjectDocument) error {
		return editAssetDocument(p, f.sid, shotText(latest, "id"), func(_, asset ProjectDocument) error {
			asset["binned_at"] = rawValue(stamp())
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
	if response := f.post("/blocking/sketch", nil); response.Code != 400 || !strings.Contains(response.Body.String(), "in the bin") {
		t.Fatalf("binned study: %d", response.Code)
	}
}

func TestBlockingStudyDoesNotEndAnImageApproval(t *testing.T) {
	f := newBlockingFixture(t)
	f.post("/blocking/save", nil)
	f.capture(blockingTestPoster(t, 0))
	project, _ := f.load()
	image := blockingTestPoster(t, 90)
	project, err := f.store.importMedia(f.id, f.sid, "keyframe", "starter.png", int64(len(image)), bytes.NewReader(image), documentRevision(project), "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	shot, _ := findShot(project, f.sid)
	starter := list(shot, "assets")[0]
	if project, err = f.store.Choose(f.id, f.sid, "keyframe", shotText(starter, "id"), true, documentRevision(project)); err != nil {
		t.Fatal(err)
	}
	if shot, _ = findShot(project, f.sid); !imageApproved(project, shot) {
		t.Fatal("fixture: the starter is not approved")
	}
	if response := f.post("/blocking/sketch", nil); response.Code != 303 || len(f.sketches()) != 1 {
		t.Fatalf("use poster: %d", response.Code)
	}
	project, shot = f.load()
	if !imageApproved(project, shot) {
		t.Fatal("adding a blocking study ended the image approval")
	}
	if shotText(shot, "selected_sketch") != "" {
		t.Fatal("the study was selected although an approval was at stake")
	}
}

func TestBlockingLegendJoinsThePromptOnlyForASelectedStudy(t *testing.T) {
	f := newBlockingFixture(t)
	project, shot := f.load()
	plain := promptPackage(project, shot, "manual")
	if strings.Contains(jsonText(plain), "BLOCKING STUDY") {
		t.Fatal("a shot without a study mentions one")
	}
	f.post("/blocking/save", func(v url.Values) { v.Set("actor.0.name", "Mara"); v.Set("actor.1.name", "The stranger") })
	f.capture(blockingTestPoster(t, 0))
	f.post("/blocking/sketch", nil)
	project, shot = f.load()
	prompts := promptPackage(project, shot, "manual")
	for _, key := range []string{"composition", "enhancement"} {
		text := shotText(prompts, key)
		for _, want := range []string{"BLOCKING STUDY", "the orange figure is Mara", "the teal figure is The stranger", "Do not draw the floor grid"} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s prompt lacks %q:\n%s", key, want, text)
			}
		}
	}
	if strings.Contains(shotText(prompts, "animation"), "BLOCKING STUDY") {
		t.Fatal("the video prompt describes a study it does not receive")
	}
	// The legend describes the picture that was taken, not later renames.
	f.post("/blocking/save", func(v url.Values) { v.Set("actor.0.name", "Renamed") })
	project, shot = f.load()
	if text := shotText(promptPackage(project, shot, "manual"), "composition"); !strings.Contains(text, "is Mara") || strings.Contains(text, "Renamed") {
		t.Fatalf("legend followed a later rename:\n%s", text)
	}
	// Deselecting the study removes the legend and restores the plain prompt.
	if _, err := f.store.mutate(f.id, documentRevision(project), func(p ProjectDocument) error {
		return editShotDocument(p, f.sid, func(s ProjectDocument) error { delete(s, "selected_sketch"); return nil })
	}); err != nil {
		t.Fatal(err)
	}
	project, shot = f.load()
	if after := shotText(promptPackage(project, shot, "manual"), "composition"); after != shotText(plain, "composition") {
		t.Fatalf("plain prompt changed:\n%s\n---\n%s", after, shotText(plain, "composition"))
	}
}

func TestColourNamesAreDistinctAcrossThePalette(t *testing.T) {
	want := []string{"orange", "teal", "blue", "purple", "yellow", "red", "green", "pink"}
	for i, colour := range blockingPalette {
		if got := colourName(colour); got != want[i] {
			t.Errorf("%s is %q, want %q", colour, got, want[i])
		}
	}
	for colour, name := range map[string]string{"#000000": "black", "#ffffff": "white", "#808080": "grey", "#5a3a1e": "brown", "not-a-colour": "grey"} {
		if got := colourName(colour); got != name {
			t.Errorf("%s is %q, want %q", colour, got, name)
		}
	}
}

func TestBlockingStageHasNoFixedProps(t *testing.T) {
	props, err := blockingRender(defaultBlockingScene(), "16:9", "camera")
	if err != nil {
		t.Fatal(err)
	}
	scene, _ := json.Marshal(props.SceneIR())
	if strings.Contains(string(scene), "stage-bench") {
		t.Fatal("every poster would contain a bench the shot never asked for")
	}
	if !strings.Contains(string(scene), "actor-a-head") || !strings.Contains(string(scene), "stage-floor") {
		t.Fatal("the stage lost its actors or floor")
	}
}
