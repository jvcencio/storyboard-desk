package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeGenerationProvider struct {
	images, videos, polls int
	imageError            error
	video                 []byte
	pending               bool
	imageCost             float64
	imageBody, videoBody  ProjectDocument
	ready                 func() error
}

func (f *fakeGenerationProvider) Ready() error {
	if f.ready != nil {
		return f.ready()
	}
	return nil
}
func (f *fakeGenerationProvider) Call(path string, body any, auth bool) (ProjectDocument, error) {
	var catalog ProjectDocument
	json.Unmarshal(cachedCatalog, &catalog)
	switch path {
	case "auth/key":
		return ProjectDocument{}, nil
	case "images/models":
		return ProjectDocument{"data": catalog["images"]}, nil
	case "videos/models":
		return ProjectDocument{"data": catalog["videos"]}, nil
	case "images":
		f.images++
		f.imageBody = body.(ProjectDocument)
		if f.imageError != nil {
			return nil, f.imageError
		}
		png := []byte("\x89PNG\r\n\x1a\ncloud-fixture")
		return ProjectDocument{"data": rawValue([]ProjectDocument{{"media_type": rawValue("image/png"), "b64_json": rawValue(base64.StdEncoding.EncodeToString(png))}}), "usage": rawValue(ProjectDocument{"cost": rawValue(f.imageCost)})}, nil
	case "videos":
		f.videos++
		f.videoBody = body.(ProjectDocument)
		return ProjectDocument{"id": rawValue("provider-123"), "status": rawValue("pending")}, nil
	case "videos/provider-123":
		f.polls++
		status := "completed"
		if f.pending {
			status = "pending"
		}
		return ProjectDocument{"status": rawValue(status), "usage": rawValue(ProjectDocument{"cost": rawValue(.64)}), "urls": rawValue([]string{"https://private.invalid/signed"})}, nil
	}
	return nil, fmt.Errorf("unexpected fake provider path %q", path)
}
func (f *fakeGenerationProvider) Download(id string) ([]byte, error) {
	if id != "provider-123" {
		return nil, fmt.Errorf("unexpected job")
	}
	return f.video, nil
}
func cloudFixture(t *testing.T) (*ProjectStore, *CloudService, *fakeGenerationProvider, string, string, string) {
	t.Helper()
	s := testStore(t)
	p := createTestProject(t, s)
	id := shotText(p, "id")
	sid := shotText(list(p, "shots")[0], "id")
	png := []byte("\x89PNG\r\n\x1a\noriginal")
	p, err := s.ImportMedia(id, sid, "keyframe", "starter.png", int64(len(png)), strings.NewReader(string(png)), 1, "", "")
	if err != nil {
		t.Fatal(err)
	}
	aid := shotText(list(list(p, "shots")[0], "assets")[0], "id")
	fake := &fakeGenerationProvider{imageCost: .18}
	c, err := newCloudService(s, fake)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Connect(id); err != nil {
		t.Fatal(err)
	}
	if err = c.Allowance(id, 5); err != nil {
		t.Fatal(err)
	}
	if err = c.CreateBrief(id); err != nil {
		t.Fatal(err)
	}
	brief, _ := c.brief(id)
	if err = c.ApproveBrief(id, pythonDigest(object(brief, "content"))); err != nil {
		t.Fatal(err)
	}
	return s, c, fake, id, sid, aid
}
func imageQuote(t *testing.T, s *ProjectStore, c *CloudService, id, sid, aid string) ProjectDocument {
	t.Helper()
	p, _ := s.Load(id)
	q, err := c.Prepare(id, sid, documentRevision(p), ProjectDocument{"kind": rawValue("image"), "model": rawValue(imageModels[0]), "prompt": rawValue("One scene, same face."), "reference_ids": rawValue([]string{aid})})
	if err != nil {
		t.Fatal(err)
	}
	return q
}
func TestCloudConfirmationBudgetAndIdempotentImageImport(t *testing.T) {
	s, c, fake, id, sid, aid := cloudFixture(t)
	q := imageQuote(t, s, c, id, sid, aid)
	qid := shotText(q, "id")
	state, _ := c.Status(id)
	if fake.images != 0 || number(state, "committed_usd") != 0 {
		t.Fatal("preparation spent credits")
	}
	if _, err := c.Submit(qid, false); err == nil {
		t.Fatal("unconfirmed request accepted")
	}
	c.Allowance(id, 0)
	if _, err := c.Submit(qid, true); err == nil {
		t.Fatal("budget bypass")
	}
	c.Allowance(id, 5)
	job, err := c.Submit(qid, true)
	if err != nil {
		t.Fatal(err)
	}
	if fake.images != 0 {
		t.Fatal("reservation itself submitted generation")
	}
	body, err := c.body(job)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body["input_references"]), "data:image/png;base64,") || !strings.Contains(shotText(body, "prompt"), "INPUT IMAGE GUIDE") {
		t.Fatal("missing exact image snapshot")
	}
	job, err = c.Process(qid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Submit(qid, true); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Process(qid); err != nil {
		t.Fatal(err)
	}
	if fake.images != 1 {
		t.Fatal("double-click replayed provider submission")
	}
	p, _ := s.Load(id)
	shot := list(p, "shots")[0]
	if len(list(shot, "assets")) != 2 || shotText(shot, "selected_keyframe") != "" || shotText(job, "status") != "done" {
		t.Fatal("result selected itself or duplicated media")
	}
	state, _ = c.Status(id)
	if number(state, "committed_usd") != .18 {
		t.Fatal("cost not reconciled")
	}
	if strings.Contains(jsonText(state), "Authorization") {
		t.Fatal("credential field saved")
	}
	delete(job, "asset_id")
	job["status"] = rawValue("importing")
	c.save(job)
	if _, err = c.Process(qid); err != nil {
		t.Fatal(err)
	}
	p, _ = s.Load(id)
	if len(list(list(p, "shots")[0], "assets")) != 2 || fake.images != 1 {
		t.Fatal("recovery duplicated result")
	}
}
func TestCloudUnknownRequestKeepsReservationAndIsNeverReplayed(t *testing.T) {
	s, c, fake, id, sid, aid := cloudFixture(t)
	q := imageQuote(t, s, c, id, sid, aid)
	qid := shotText(q, "id")
	fake.imageError = errors.New("provider interrupted")
	if _, err := c.Submit(qid, true); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Process(qid); err == nil {
		t.Fatal("expected interruption")
	}
	state, _ := c.Status(id)
	if number(state, "committed_usd") != 1 {
		t.Fatal("unknown cost released reservation")
	}
	restarted, err := newCloudService(s, fake)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = restarted.Process(qid); err == nil {
		t.Fatal("unknown request was replayed")
	}
	if fake.images != 1 {
		t.Fatal("second paid call")
	}
	state, _ = restarted.Status(id)
	if shotText(list(state, "jobs")[0], "status") != "unknown" || number(state, "committed_usd") != 1 {
		t.Fatal("unknown outcome lost")
	}
}
func TestCloudStaleQuotesAndChangedReferencesFailBeforeSubmission(t *testing.T) {
	s, c, fake, id, sid, aid := cloudFixture(t)
	q := imageQuote(t, s, c, id, sid, aid)
	p, _ := s.Load(id)
	s.UpdateProject(id, documentRevision(p), ProjectDocument{"look": rawValue("Changed")})
	if _, err := c.Submit(shotText(q, "id"), true); err == nil {
		t.Fatal("stale quote accepted")
	}
	q = imageQuote(t, s, c, id, sid, aid)
	p, _ = s.Load(id)
	asset := list(list(p, "shots")[0], "assets")[0]
	os.WriteFile(filepath.Join(s.path, id, shotText(asset, "path")), []byte("changed"), 0600)
	if _, err := c.Submit(shotText(q, "id"), true); err == nil {
		t.Fatal("changed bytes accepted")
	}
	if fake.images != 0 || len(c.jobs) != 0 {
		t.Fatal("invalid request committed spend")
	}
}
func TestCloudReportedCostAboveEstimateAndBriefApproval(t *testing.T) {
	s, c, fake, id, sid, aid := cloudFixture(t)
	c.CreateBrief(id)
	q := imageQuote(t, s, c, id, sid, aid)
	if _, err := c.Submit(shotText(q, "id"), true); err == nil {
		t.Fatal("unapproved brief accepted")
	}
	brief, _ := c.brief(id)
	c.ApproveBrief(id, pythonDigest(object(brief, "content")))
	fake.imageCost = 1.2
	if _, err := c.Submit(shotText(q, "id"), true); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Process(shotText(q, "id")); err != nil {
		t.Fatal(err)
	}
	if err := c.Allowance(id, 1.1); err == nil {
		t.Fatal("allowance below actual charge accepted")
	}
	state, _ := c.Status(id)
	if number(state, "committed_usd") != 1.2 {
		t.Fatal("actual charge not retained")
	}
}
func TestCloudVideoResumesExistingJobAndPreservesStarterBinding(t *testing.T) {
	if _, err := exec.LookPath(videoTool("ffmpeg")); err != nil {
		t.Skip("FFmpeg required for generated-video inspection")
	}
	source := filepath.Join(t.TempDir(), "silent.mp4")
	if _, err := runMedia(30*time.Second, "ffmpeg", "-v", "error", "-nostdin", "-n", "-f", "lavfi", "-i", "color=c=green:size=160x90:rate=24", "-t", "1", "-c:v", "libx264", "-threads", "2", source); err != nil {
		t.Fatal(err)
	}
	payload, _ := os.ReadFile(source)
	s, c, fake, id, sid, aid := cloudFixture(t)
	p, _ := s.Load(id)
	p, err := s.Choose(id, sid, "keyframe", aid, true, documentRevision(p))
	if err != nil {
		t.Fatal(err)
	}
	q, err := c.Prepare(id, sid, documentRevision(p), ProjectDocument{"kind": rawValue("video"), "model": rawValue(videoModels[0]), "prompt": rawValue("One slow nod."), "duration": rawValue(8), "resolution": rawValue("720p")})
	if err != nil {
		t.Fatal(err)
	}
	if number(q, "reserved_usd") != .64 {
		t.Fatal("silent estimate changed")
	}
	fake.video, fake.pending = payload, true
	qid := shotText(q, "id")
	if _, err = c.Submit(qid, true); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Process(qid); err != nil {
		t.Fatal(err)
	}
	if boolean(fake.videoBody, "generate_audio") || shotText(list(fake.videoBody, "frame_images")[0], "frame_type") != "first_frame" {
		t.Fatal("video not bound to a silent first frame")
	}
	png := []byte("\x89PNG\r\n\x1a\nnew-starter")
	p, err = s.ImportMedia(id, sid, "keyframe", "new.png", int64(len(png)), strings.NewReader(string(png)), documentRevision(p), "", "")
	if err != nil {
		t.Fatal(err)
	}
	second := shotText(list(list(p, "shots")[0], "assets")[1], "id")
	p, err = s.Choose(id, sid, "keyframe", second, true, documentRevision(p))
	if err != nil {
		t.Fatal(err)
	}
	fake.pending = false
	restarted, err := newCloudService(s, fake)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = restarted.Process(qid); err != nil {
		t.Fatal(err)
	}
	if fake.videos != 1 {
		t.Fatal("video was submitted again")
	}
	p, _ = s.Load(id)
	shot := list(p, "shots")[0]
	assets := list(shot, "assets")
	clip := assets[len(assets)-1]
	if shotText(clip, "kind") != "clip" || number(clip, "audio_tracks") != 0 || shotText(clip, "keyframe_id") != aid || !boolean(object(clip, "cloud_generation"), "direction_stale_on_return") || shotText(shot, "selected_keyframe") != second || shotText(shot, "selected_clip") != "" {
		t.Fatal("late result changed selection or lost starter provenance")
	}
	state, _ := restarted.Status(id)
	if strings.Contains(jsonText(state), "private.invalid") {
		t.Fatal("provider URL leaked")
	}
}
func TestProviderRejectsArbitraryEndpointsBeforeReadingCredentials(t *testing.T) {
	p := newOpenRouterProvider()
	p.key = func() (string, error) { t.Fatal("credential read for invalid endpoint"); return "", nil }
	for _, path := range []string{"https://evil.invalid", "videos/../auth/key", "videos/id/content?url=x"} {
		if _, err := p.Call(path, nil, true); err == nil {
			t.Fatal("accepted arbitrary endpoint")
		}
	}
}
