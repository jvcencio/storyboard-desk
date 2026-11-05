package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A real child executable exercises argv, snapshots, cancellation and ownership
// without loading a model. It only activates in the test-owned renderer child.
func TestMain(m *testing.M) {
	if os.Getenv("SBD_DRAW_HELPER") == "render" {
		args := os.Args[1:]
		output := ""
		for i, arg := range args {
			if arg == "--output" && i+1 < len(args) {
				output = args[i+1]
			}
		}
		if output == "" {
			os.Exit(2)
		}
		data, _ := json.Marshal(args)
		os.WriteFile(filepath.Join(filepath.Dir(output), "args.json"), data, 0600)
		if os.Getenv("SBD_DRAW_HOLD") == "1" {
			time.Sleep(30 * time.Second)
		}
		if err := os.WriteFile(output, []byte("\x89PNG\r\n\x1a\nlocal-render-fixture"), 0600); err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func drawingFixture(t *testing.T) (*ProjectStore, *DrawingService, string, string) {
	t.Helper()
	s := testStore(t)
	t.Setenv("SBD_DRAW_HELPER", "render")
	p := createTestProject(t, s)
	id := shotText(p, "id")
	sid := shotText(list(p, "shots")[0], "id")
	d, err := newDrawingService(s, AppSettings{DrawCLI: os.Args[0], ModelsDir: t.TempDir(), Model: "fixture.ckpt"})
	if err != nil {
		t.Fatal(err)
	}
	d.preflight = func() error { return nil }
	d.metrics = func() ProjectDocument { return ProjectDocument{"pressure": rawValue("normal")} }
	d.interval = 10 * time.Millisecond
	s.drawing = d
	t.Cleanup(d.Close)
	return s, d, id, sid
}
func TestLocalDrawingSnapshotsOfflineArgumentsAndIdempotentRecovery(t *testing.T) {
	s, d, id, sid := drawingFixture(t)
	data := []byte("\x89PNG\r\n\x1a\ninput-fixture")
	p, err := s.ImportMedia(id, sid, "sketch", "input.png", int64(len(data)), strings.NewReader(string(data)), 1, "", "")
	if err != nil {
		t.Fatal(err)
	}
	aid := shotText(list(list(p, "shots")[0], "assets")[0], "id")
	jid, err := d.Start(id, sid, 2, "image", aid, .4)
	if err != nil {
		t.Fatal(err)
	}
	d.wg.Wait()
	job := d.Status(id)[0]
	if shotText(job, "status") != "done" {
		t.Fatalf("drawing: %s", jsonText(job))
	}
	folder := filepath.Join(s.path, id, "jobs", jid)
	argsRaw, err := os.ReadFile(filepath.Join(folder, "args.json"))
	if err != nil {
		t.Fatal(err)
	}
	var args []string
	json.Unmarshal(argsRaw, &args)
	for _, flag := range []string{"--offline", "--local", "--no-download-missing", "--disable-preview", "--image", "--strength"} {
		if !containsField(args, flag) {
			t.Fatalf("missing %s", flag)
		}
	}
	input := object(job, "input_image")
	snapshot, err := s.readAux(id, shotText(input, "snapshot_path"))
	if err != nil || string(snapshot) != string(data) {
		t.Fatal("input snapshot lost")
	}
	p, _ = s.Load(id)
	shot := list(p, "shots")[0]
	if len(list(shot, "assets")) != 2 || shotText(shot, "selected_sketch") != "" {
		t.Fatal("drawing selected itself")
	}
	a := list(shot, "assets")[1]
	if shotText(object(a, "local_generation"), "job_id") != jid || object(a, "cloud_generation") != nil {
		t.Fatal("local provenance not preserved")
	}
	if err = d.Recover(id, jid); err != nil {
		t.Fatal(err)
	}
	p, _ = s.Load(id)
	if len(list(list(p, "shots")[0], "assets")) != 2 {
		t.Fatal("recovery duplicated local result")
	}
}
func TestLocalDrawingCancellationAndCriticalMemoryPreserveMedia(t *testing.T) {
	for _, critical := range []bool{false, true} {
		t.Run(fmt.Sprint(critical), func(t *testing.T) {
			s, d, id, sid := drawingFixture(t)
			t.Setenv("SBD_DRAW_HOLD", "1")
			if critical {
				d.metrics = func() ProjectDocument { return ProjectDocument{"pressure": rawValue("critical")} }
			}
			jid, err := d.Start(id, sid, 1, "text", "", .4)
			if err != nil {
				t.Fatal(err)
			}
			if !critical {
				if err = d.Cancel(id, jid); err != nil {
					t.Fatal(err)
				}
			}
			d.wg.Wait()
			job := d.Status(id)[0]
			expected := "cancelled"
			if critical {
				expected = "blocked"
			}
			if shotText(job, "status") != expected {
				t.Fatalf("state %s", jsonText(job))
			}
			p, _ := s.Load(id)
			if len(list(list(p, "shots")[0], "assets")) != 0 {
				t.Fatal("cancelled drawing imported media")
			}
		})
	}
}
func TestReviewedDirectionImportUsesCurrentImageAndBasis(t *testing.T) {
	s := testStore(t)
	p := createTestProject(t, s)
	id := shotText(p, "id")
	sid := shotText(list(p, "shots")[0], "id")
	data := []byte("\x89PNG\r\n\x1a\nimage")
	p, err := s.ImportMedia(id, sid, "keyframe", "starter.png", int64(len(data)), strings.NewReader(string(data)), 1, "", "")
	if err != nil {
		t.Fatal(err)
	}
	aid := shotText(list(list(p, "shots")[0], "assets")[0], "id")
	p, err = s.Choose(id, sid, "keyframe", aid, true, 2)
	if err != nil {
		t.Fatal(err)
	}
	shot := list(p, "shots")[0]
	basis := directionBasis(p, shot)
	raw := ProjectDocument{"shots": rawValue([]ProjectDocument{{"id": rawValue(sid), "basis": rawValue(basis), "image_inspected": rawValue(true), "verdict": rawValue("ready_for_review"), "grok_prompt": rawValue("One small visible action. The camera stays fixed."), "action": rawValue("A small nod.")}})}
	p, err = s.ApplyPlan(id, 3, raw)
	if err != nil {
		t.Fatal(err)
	}
	shot = list(p, "shots")[0]
	if !optimizedDirectionReady(p, shot) {
		t.Fatal("review did not bind to final direction")
	}
	if _, err = s.ApplyPlan(id, 4, raw); err == nil {
		t.Fatal("accepted an obsolete basis")
	}
	p, err = s.UpdateShot(id, sid, 4, ProjectDocument{"composition": rawValue("A different starting pose")})
	if err != nil {
		t.Fatal(err)
	}
	if optimizedDirectionReady(p, list(p, "shots")[0]) {
		t.Fatal("visual edit did not stale approved direction")
	}
}
