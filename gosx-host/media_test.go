package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMediaImportApprovalBinAndRangeServing(t *testing.T) {
	s := testStore(t)
	p := createTestProject(t, s)
	id := shotText(p, "id")
	sid := shotText(list(p, "shots")[0], "id")
	data := []byte("\x89PNG\r\n\x1a\nsynthetic-image-bytes")
	p, err := s.ImportMedia(id, sid, "keyframe", "雪.png", int64(len(data)), bytes.NewReader(data), 1, "", "")
	if err != nil {
		t.Fatal(err)
	}
	asset := list(list(p, "shots")[0], "assets")[0]
	aid := shotText(asset, "id")
	if shotText(list(p, "shots")[0], "selected_keyframe") != "" {
		t.Fatal("import selected itself")
	}
	p, err = s.Choose(id, sid, "keyframe", aid, true, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !imageApproved(p, list(p, "shots")[0]) {
		t.Fatal("approval not established")
	}
	p, err = s.UpdateProject(id, 3, ProjectDocument{"look": rawValue("New look")})
	if err != nil {
		t.Fatal(err)
	}
	if imageApproved(p, list(p, "shots")[0]) {
		t.Fatal("visual changes did not invalidate approval")
	}
	p, err = s.Bin(id, sid, aid, "Try another", false, 4)
	if err != nil {
		t.Fatal(err)
	}
	if shotText(list(p, "shots")[0], "selected_keyframe") != "" {
		t.Fatal("bin retained selection")
	}
	p, err = s.Bin(id, sid, aid, "", true, 5)
	if err != nil {
		t.Fatal(err)
	}
	h := &workspaceHandler{store: s, csrf: "token"}
	r := httptest.NewRequest("GET", "http://127.0.0.1:4444"+mediaURL(id, shotText(asset, "path")), nil)
	r.Header.Set("Range", "bytes=0-7")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 206 || w.Body.String() != string(data[:8]) {
		t.Fatalf("range: %d %q", w.Code, w.Body.String())
	}
	for jobID, projectID := range map[string]string{"own-job": id, "foreign-job": "another-project"} {
		job := ProjectDocument{"id": rawValue(jobID), "project_id": rawValue(projectID), "status": rawValue("unknown"), "reserved_cost_usd": rawValue(0.1)}
		if err := s.writeAux("", "cloud/jobs/"+jobID+".json", []byte(jsonText(job))); err != nil {
			t.Fatal(err)
		}
	}
	rel, err := s.ExportReview(id)
	if err != nil {
		t.Fatal(err)
	}
	f, err := s.OpenMedia(id, rel)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := f.Stat()
	archive, err := zip.NewReader(f, info.Size())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	found, receiptFound := false, false
	for _, entry := range archive.File {
		if entry.Name == "generation/jobs.json" {
			receiptFound = true
			stream, _ := entry.Open()
			b, _ := io.ReadAll(stream)
			stream.Close()
			if !bytes.Contains(b, []byte("own-job")) || bytes.Contains(b, []byte("foreign-job")) {
				t.Fatal("review export lost its receipt or disclosed another project's job")
			}
		}
		if entry.Name == "sequence.json" {
			found = true
			stream, _ := entry.Open()
			b, _ := io.ReadAll(stream)
			stream.Close()
			if !bytes.Contains(b, []byte(sid)) {
				t.Fatal("sequence lost identity")
			}
		}
	}
	if !found || !receiptFound {
		t.Fatal("missing sequence manifest or generation receipts")
	}
	source, err := s.OpenMedia(id, shotText(asset, "path"))
	if err != nil {
		t.Fatal(err)
	}
	after, _ := io.ReadAll(source)
	source.Close()
	if !bytes.Equal(after, data) {
		t.Fatal("source bytes changed")
	}
	before, _ := s.Load(id)
	if _, err = s.ImportMedia(id, sid, "sketch", "bad.png", 3, strings.NewReader("bad"), 6, "", ""); err == nil {
		t.Fatal("accepted bad image")
	}
	afterDoc, _ := s.Load(id)
	if !reflect.DeepEqual(before, afterDoc) {
		t.Fatal("bad import changed project")
	}
}
func TestMultipartImportAndTraversalRejection(t *testing.T) {
	s := testStore(t)
	p := createTestProject(t, s)
	id := shotText(p, "id")
	sid := shotText(list(p, "shots")[0], "id")
	h := &workspaceHandler{store: s, csrf: "token"}
	var b bytes.Buffer
	m := multipart.NewWriter(&b)
	for k, v := range map[string]string{"csrf": "token", "project": id, "shot": sid, "kind": "sketch", "revision": "1", "view": "library"} {
		m.WriteField(k, v)
	}
	part, _ := m.CreateFormFile("file", "reference.png")
	part.Write([]byte("\x89PNG\r\n\x1a\nfixture"))
	m.Close()
	r := httptest.NewRequest("POST", "http://127.0.0.1:4444/upload", &b)
	r.Header.Set("Content-Type", m.FormDataContentType())
	r.Header.Set("Origin", "http://127.0.0.1:4444")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 303 {
		t.Fatalf("upload: %d %s", w.Code, w.Body.String())
	}
	for _, path := range []string{"../project.json", "media/../../project.json", "media/../project.json", "media/\\private", "exports/../../private"} {
		if f, err := s.OpenMedia(id, path); err == nil {
			f.Close()
			t.Fatalf("accepted %s", path)
		}
	}
	p, _ = s.Load(id)
	if len(list(list(p, "shots")[0], "assets")) != 1 {
		t.Fatal("multipart did not persist media")
	}
}
func TestActualFFmpegImportPlacementAndSilentExport(t *testing.T) {
	if _, err := exec.LookPath(videoTool("ffmpeg")); err != nil {
		if os.Getenv("STORYBOARD_REQUIRE_FFMPEG") == "1" {
			t.Fatal(err)
		}
		t.Skip("FFmpeg not installed")
	}
	source := filepath.Join(t.TempDir(), "source 雪's.mp4")
	_, err := runMedia(30*time.Second, "ffmpeg", "-v", "error", "-nostdin", "-n", "-f", "lavfi", "-i", "testsrc2=size=160x90:rate=24", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=8000", "-t", "2", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-threads", "2", "-c:a", "aac", source)
	if err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(source)
	digest := sha256.Sum256(original)
	s := testStore(t)
	p := createTestProject(t, s)
	id := shotText(p, "id")
	sid := shotText(list(p, "shots")[0], "id")
	p, err = s.ImportMedia(id, sid, "clip", filepath.Base(source), int64(len(original)), bytes.NewReader(original), 1, "", "")
	if err != nil {
		t.Fatal(err)
	}
	a := list(list(p, "shots")[0], "assets")[0]
	if number(a, "duration_seconds") < 1.9 || shotText(a, "poster_path") == "" {
		t.Fatalf("probe failed: %s", jsonText(a))
	}
	p, err = s.TrimClip(id, sid, shotText(a, "id"), 2, .25, 1.25, 0)
	if err != nil {
		t.Fatal(err)
	}
	p, err = s.Insert(id, sid, "after", 3)
	if err != nil {
		t.Fatal(err)
	}
	target := shotText(list(p, "shots")[1], "id")
	p, err = s.PlaceClip(id, sid, shotText(a, "id"), target, 4, .5, 1.5, 0)
	if err != nil {
		t.Fatal(err)
	}
	if shotText(object(list(list(p, "shots")[1], "assets")[0], "source_ref"), "asset_id") != shotText(a, "id") {
		t.Fatal("placement did not reference source")
	}
	_, err = s.UndoPlacement(id, 5)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "range.mp4")
	info, err := renderVideoRange(source, output, .25, 1.25)
	if err != nil {
		t.Fatal(err)
	}
	if number(info, "audio_tracks") != 0 {
		t.Fatal("range has audio")
	}
	reel := filepath.Join(t.TempDir(), "reel.mp4")
	info, err = renderVideoReel([]videoSegment{{source, 0, .37}, {source, .5, .91}, {source, 1, 1.43}}, reel, "16:9")
	if err != nil {
		t.Fatal(err)
	}
	if number(info, "audio_tracks") != 0 || number(info, "width") != 1280 || number(info, "height") != 720 {
		t.Fatal("bad reel")
	}
	after, _ := os.ReadFile(source)
	if sha256.Sum256(after) != digest {
		t.Fatal("FFmpeg modified original")
	}
}
func TestPythonDerivedParity(t *testing.T) {
	path := os.Getenv("STORYBOARD_PARITY_ORACLE")
	if path == "" {
		t.Skip("Python oracle not configured")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var oracle map[string]json.RawMessage
	if err = json.Unmarshal(raw, &oracle); err != nil {
		t.Fatal(err)
	}
	var p ProjectDocument
	json.Unmarshal(oracle["project"], &p)
	shot := list(p, "shots")[0]
	actual := map[string]any{"fingerprint": imageFingerprint(p, shot), "direction_basis": directionBasis(p, shot), "prompt": promptPackage(p, shot, "grok"), "music_timing": musicTiming(p)}
	for name, value := range actual {
		a, b := any(nil), any(nil)
		data, _ := json.Marshal(value)
		json.Unmarshal(data, &a)
		json.Unmarshal(oracle[name], &b)
		if !reflect.DeepEqual(a, b) {
			t.Errorf("%s differs\nactual: %s\nexpected: %s", name, data, oracle[name])
		}
	}
}
