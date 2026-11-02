package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

// Invoked by Python to validate Go-written projects, receipts and local jobs
// through the real legacy loaders. Every integration is a deterministic fake.
func TestCompleteCompatibilityFixture(t *testing.T) {
	root := os.Getenv("STORYBOARD_COMPLETE_FIXTURE")
	if root == "" {
		t.Skip("Python compatibility harness not configured")
	}
	store, err := OpenProjectStore(root, true)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	p, err := store.CreateTemplate(ProjectDocument{"title": rawValue("Go compatibility 雪"), "aspect": rawValue("16:9"), "lyrics": rawValue("Original lyrics “here”\n  Keep spacing"), "shots": rawValue([]ProjectDocument{{"title": rawValue("Opening"), "composition": rawValue("A quiet platform."), "local_prompt": rawValue("A traveler beside a departure board.")}})})
	if err != nil {
		t.Fatal(err)
	}
	pid := shotText(p, "id")
	sid := shotText(list(p, "shots")[0], "id")
	p, err = store.UpdateShot(pid, sid, documentRevision(p), ProjectDocument{"scene": rawValue("Verse"), "feedback": rawValue("Keep the gesture subtle.")})
	if err != nil {
		t.Fatal(err)
	}
	wav := fixtureWAV(10)
	p, err = store.ImportMedia(pid, "", "soundtrack", "song.wav", int64(len(wav)), strings.NewReader(string(wav)), documentRevision(p), "", "")
	if err != nil {
		t.Fatal(err)
	}
	p, err = store.SaveSection(pid, documentRevision(p), ProjectDocument{"label": rawValue("Verse"), "kind": rawValue("verse"), "start_seconds": rawValue(0), "end_seconds": rawValue(5)})
	if err != nil {
		t.Fatal(err)
	}
	end := 6.5
	p, err = store.MusicCue(pid, documentRevision(p), "Crossing lyric", 4.5, &end, "", false)
	if err != nil {
		t.Fatal(err)
	}
	png := []byte("\x89PNG\r\n\x1a\nfixture")
	p, err = store.ImportMedia(pid, sid, "keyframe", "starter.png", int64(len(png)), strings.NewReader(string(png)), documentRevision(p), "", "")
	if err != nil {
		t.Fatal(err)
	}
	aid := shotText(list(list(p, "shots")[0], "assets")[0], "id")
	p, err = store.Choose(pid, sid, "keyframe", aid, true, documentRevision(p))
	if err != nil {
		t.Fatal(err)
	}
	provider := &fakeGenerationProvider{imageCost: .18}
	cloud, err := newCloudService(store, provider)
	if err != nil {
		t.Fatal(err)
	}
	if err = cloud.Connect(pid); err != nil {
		t.Fatal(err)
	}
	if err = cloud.Allowance(pid, 5); err != nil {
		t.Fatal(err)
	}
	if err = cloud.CreateBrief(pid); err != nil {
		t.Fatal(err)
	}
	brief, _ := cloud.brief(pid)
	if err = cloud.ApproveBrief(pid, pythonDigest(object(brief, "content"))); err != nil {
		t.Fatal(err)
	}
	quote := imageQuote(t, store, cloud, pid, sid, aid)
	if _, err = cloud.Submit(shotText(quote, "id"), true); err != nil {
		t.Fatal(err)
	}
	if _, err = cloud.Process(shotText(quote, "id")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SBD_DRAW_HELPER", "render")
	draw, err := newDrawingService(store, AppSettings{DrawCLI: os.Args[0], ModelsDir: t.TempDir(), Model: "fixture.ckpt"})
	if err != nil {
		t.Fatal(err)
	}
	store.drawing = draw
	draw.preflight = func() error { return nil }
	draw.metrics = func() ProjectDocument { return ProjectDocument{"pressure": rawValue("normal")} }
	draw.interval = 10 * time.Millisecond
	p, _ = store.Load(pid)
	if _, err = draw.Start(pid, sid, documentRevision(p), "text", "", .4); err != nil {
		t.Fatal(err)
	}
	draw.wg.Wait()
	if shotText(draw.Status(pid)[0], "status") != "done" {
		t.Fatal("fixture drawing failed")
	}
}
