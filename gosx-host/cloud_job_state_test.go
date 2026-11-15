package main

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func cloudJobStatus(t *testing.T, c *CloudService, id, jid string) (string, float64) {
	t.Helper()
	state, err := c.Status(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, job := range list(state, "jobs") {
		if shotText(job, "id") == jid {
			return shotText(job, "status"), number(state, "committed_usd")
		}
	}
	t.Fatalf("job %s is not listed", jid)
	return "", 0
}

// videoQuote approves the fixture's starter and prepares one silent video.
func videoQuote(t *testing.T, s *ProjectStore, c *CloudService, id, sid, aid string) ProjectDocument {
	t.Helper()
	p, _ := s.Load(id)
	if shot, _ := findShot(p, sid); !imageApproved(p, shot) {
		var err error
		if p, err = s.Choose(id, sid, "keyframe", aid, true, documentRevision(p)); err != nil {
			t.Fatal(err)
		}
	}
	q, err := c.Prepare(id, sid, documentRevision(p), ProjectDocument{"kind": rawValue("video"), "model": rawValue(videoModels[0]), "prompt": rawValue("One slow nod."), "duration": rawValue(8), "resolution": rawValue("720p")})
	if err != nil {
		t.Fatal(err)
	}
	return q
}

// Every status the generation code can write has a rule. A new status without
// one would silently fall back to "unfinished and charged".
func TestCloudEveryWrittenStatusHasARule(t *testing.T) {
	written := regexp.MustCompile(`"status"\]?(?:, [^=\n]+)?(?::| =|, [a-z"\[\]_ ]+=)\s*rawValue\("([a-z_]+)"\)`)
	found := map[string]bool{}
	for _, name := range []string{"cloud.go", "cloud_process.go", "cloud_job_state.go"} {
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range written.FindAllStringSubmatch(string(source), -1) {
			found[match[1]] = true
		}
	}
	for _, expected := range []string{"queued", "submitting", "pending", "downloading", "importing", "done", "failed", "unknown", "cancelled", "reconciled"} {
		if !found[expected] {
			t.Errorf("the scan no longer sees status %q being written; fix the pattern", expected)
		}
	}
	for status := range found {
		if _, ok := cloudJobRules[status]; !ok {
			t.Errorf("status %q is written without a rule in cloudJobRules", status)
		}
	}
	// Process chooses these three by name when a check fails, and the provider
	// reports in_progress.
	for _, status := range []string{"recoverable", "blocked", "unknown", "in_progress"} {
		if _, ok := cloudJobRules[status]; !ok {
			t.Errorf("status %q has no rule", status)
		}
	}
}

// One failed status check must not let the shot start a second paid video
// while the first is still running at the provider.
func TestCloudFailedCheckKeepsTheShotClosed(t *testing.T) {
	s, c, fake, id, sid, aid := cloudFixture(t)
	first := shotText(videoQuote(t, s, c, id, sid, aid), "id")
	fake.pending = true
	if _, err := c.Submit(first, true); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Process(first); err != nil {
		t.Fatal(err)
	}
	fake.pollError = errors.New("network unreachable")
	if _, err := c.Process(first); err == nil {
		t.Fatal("expected the status check to fail")
	}
	if status, _ := cloudJobStatus(t, c, id, first); status != "recoverable" {
		t.Fatalf("status after a failed check: %s", status)
	}
	second := shotText(videoQuote(t, s, c, id, sid, aid), "id")
	if _, err := c.Submit(second, true); err == nil || !strings.Contains(err.Error(), "already has an active cloud job") {
		t.Fatalf("a second paid video was accepted while the first may be running: %v", err)
	}
	if fake.videos != 1 {
		t.Fatalf("paid video requests: %d", fake.videos)
	}
	// The same job resumes once the provider answers again.
	fake.pollError = nil
	if _, err := c.Process(first); err != nil {
		t.Fatal(err)
	}
	if status, _ := cloudJobStatus(t, c, id, first); status != "pending" || fake.videos != 1 {
		t.Fatalf("resume: %s after %d requests", status, fake.videos)
	}
}

func TestCloudUnknownOutcomeClosesTheShotUntilReconciled(t *testing.T) {
	s, c, fake, id, sid, aid := cloudFixture(t)
	first := shotText(imageQuote(t, s, c, id, sid, aid), "id")
	fake.imageError = errors.New("connection reset")
	if _, err := c.Submit(first, true); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Process(first); err == nil {
		t.Fatal("expected an interrupted request")
	}
	fake.imageError = nil
	second := shotText(imageQuote(t, s, c, id, sid, aid), "id")
	if _, err := c.Submit(second, true); err == nil {
		t.Fatal("a new paid request was accepted before the unknown one was reconciled")
	}
	if _, err := c.Reconcile(second); err == nil {
		t.Fatal("reconciled a request that was never confirmed")
	}
	if _, err := c.Reconcile(first); err != nil {
		t.Fatal(err)
	}
	if status, committed := cloudJobStatus(t, c, id, first); status != "reconciled" || committed != 1 {
		t.Fatalf("after reconcile: %s, committed $%g; the unknown charge must stay reserved", status, committed)
	}
	if _, err := c.Reconcile(first); err == nil {
		t.Fatal("reconciled twice")
	}
	if _, err := c.Process(first); err == nil || fake.images != 1 {
		t.Fatalf("a reconciled request was sent again: %v, %d requests", err, fake.images)
	}
	if _, err := c.Submit(second, true); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Process(second); err != nil {
		t.Fatal(err)
	}
	if _, committed := cloudJobStatus(t, c, id, second); fake.images != 2 || committed != 1.18 {
		t.Fatalf("after the new request: %d requests, committed $%g", fake.images, committed)
	}
}

func TestCloudRequestStoppedBeforeSendingReleasesItsReservation(t *testing.T) {
	s, c, fake, id, sid, aid := cloudFixture(t)
	first := shotText(imageQuote(t, s, c, id, sid, aid), "id")
	checks := 0
	fake.ready = func() error {
		if checks++; checks > 1 {
			return errors.New("credential unavailable")
		}
		return nil
	}
	if _, err := c.Submit(first, true); err != nil {
		t.Fatal(err)
	}
	job, err := c.Process(first)
	if err == nil {
		t.Fatal("expected the credential check to stop the request")
	}
	status, committed := cloudJobStatus(t, c, id, first)
	if status != "blocked" || committed != 0 || fake.images != 0 || !strings.Contains(shotText(job, "message"), "Nothing was sent") {
		t.Fatalf("%s, committed $%g, %d requests, message %q", status, committed, fake.images, shotText(job, "message"))
	}
	if _, err = c.Process(first); err == nil || fake.images != 0 {
		t.Fatal("a blocked request was sent later")
	}
	// The shot is free for a fresh request.
	fake.ready = nil
	second := shotText(imageQuote(t, s, c, id, sid, aid), "id")
	if _, err = c.Submit(second, true); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Process(second); err != nil || fake.images != 1 {
		t.Fatalf("fresh request: %v, %d requests", err, fake.images)
	}
}

func TestCloudJobStillQueuedAtStartupIsWithdrawn(t *testing.T) {
	s, c, fake, id, sid, aid := cloudFixture(t)
	first := shotText(imageQuote(t, s, c, id, sid, aid), "id")
	if _, err := c.Submit(first, true); err != nil {
		t.Fatal(err)
	}
	// The process stops here, before Process sends anything.
	restarted, err := newCloudService(s, fake)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = restarted.Process(first); err == nil || fake.images != 0 {
		t.Fatalf("a request confirmed before the restart was sent after it: %v, %d requests", err, fake.images)
	}
	if status, committed := cloudJobStatus(t, restarted, id, first); status != "cancelled" || committed != 0 {
		t.Fatalf("after restart: %s, committed $%g", status, committed)
	}
	if err = restarted.Connect(id); err != nil {
		t.Fatal(err)
	}
	second := shotText(imageQuote(t, s, restarted, id, sid, aid), "id")
	if _, err = restarted.Submit(second, true); err != nil {
		t.Fatalf("the shot stayed closed after the withdrawn request: %v", err)
	}
}

func TestCloudFailedSaveDoesNotClaimTheRequestWasSent(t *testing.T) {
	s, c, fake, id, sid, aid := cloudFixture(t)
	first := shotText(imageQuote(t, s, c, id, sid, aid), "id")
	if _, err := c.Submit(first, true); err != nil {
		t.Fatal(err)
	}
	// Make every job save fail: the jobs folder becomes a file.
	jobs := filepath.Join(s.path, "cloud", "jobs")
	if err := os.Rename(jobs, jobs+"-aside"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jobs, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Process(first); err == nil {
		t.Fatal("expected the save to fail")
	}
	if fake.images != 0 {
		t.Fatal("a request was sent although its state could not be saved")
	}
	if status := shotText(c.jobs[first], "status"); status != "queued" {
		t.Fatalf("memory says %q although the disk still says queued", status)
	}
	if err := os.Remove(jobs); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(jobs+"-aside", jobs); err != nil {
		t.Fatal(err)
	}
	restarted, err := newCloudService(s, fake)
	if err != nil {
		t.Fatal(err)
	}
	if status, committed := cloudJobStatus(t, restarted, id, first); status != "cancelled" || committed != 0 || fake.images != 0 {
		t.Fatalf("after restart: %s, committed $%g, %d requests", status, committed, fake.images)
	}
}

func TestCloudFinishedJobIsNeverAskedAgain(t *testing.T) {
	s, c, fake, id, sid, aid := cloudFixture(t)
	first := shotText(videoQuote(t, s, c, id, sid, aid), "id")
	fake.pending = true
	if _, err := c.Submit(first, true); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Process(first); err != nil {
		t.Fatal(err)
	}
	fake.pollStatus = "failed"
	if _, err := c.Process(first); err != nil {
		t.Fatal(err)
	}
	polls := fake.polls
	fake.pollError = errors.New("network unreachable")
	if _, err := c.Process(first); err == nil {
		t.Fatal("a finished job was checked again")
	}
	if status, _ := cloudJobStatus(t, c, id, first); status != "failed" || fake.polls != polls {
		t.Fatalf("a failed check turned a reported failure into %q (%d extra checks)", status, fake.polls-polls)
	}
	// A reported failure does not close the shot.
	fake.pollError, fake.pollStatus = nil, ""
	second := shotText(videoQuote(t, s, c, id, sid, aid), "id")
	if _, err := c.Submit(second, true); err != nil {
		t.Fatalf("the shot stayed closed after a reported failure: %v", err)
	}
}
