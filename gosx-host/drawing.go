package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

type DrawingService struct {
	mu        sync.Mutex
	store     *ProjectStore
	config    AppSettings
	jobs      map[string]ProjectDocument
	cancels   sync.Map
	slot      chan struct{}
	wg        sync.WaitGroup
	closed    bool
	preflight func() error
	metrics   func() ProjectDocument
	interval  time.Duration
}

func (s *ProjectStore) Drawing() (*DrawingService, error) {
	s.featuresMu.Lock()
	defer s.featuresMu.Unlock()
	if s.drawing == nil {
		var settings AppSettings
		var err error
		if s.settings != nil {
			settings = *s.settings
		} else {
			settings, err = runtimeSettings()
		}
		if err != nil {
			return nil, err
		}
		d, err := newDrawingService(s, settings)
		if err != nil {
			return nil, err
		}
		s.drawing = d
	}
	return s.drawing, nil
}
func newDrawingService(s *ProjectStore, config AppSettings) (*DrawingService, error) {
	d := &DrawingService{store: s, config: config, jobs: map[string]ProjectDocument{}, slot: make(chan struct{}, 1), interval: 4 * time.Second}
	d.metrics = func() ProjectDocument { return memoryMetrics(config.Meter) }
	d.preflight = func() error { return drawingPreflight(config, d.metrics) }
	projects, err := s.List()
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
		jobs, err := s.localJobDocuments(shotText(p, "id"))
		if err != nil {
			return nil, err
		}
		for _, j := range jobs {
			jid := shotText(j, "id")
			if !preparedJobID.MatchString(jid) {
				return nil, fmt.Errorf("invalid saved local drawing job")
			}
			if shotText(j, "status") == "queued" || shotText(j, "status") == "running" {
				j["status"], j["message"] = rawValue("interrupted"), rawValue("Server stopped. Review before starting a new local draft.")
				if err = d.save(j); err != nil {
					return nil, err
				}
			}
			d.jobs[jid] = j
		}
	}
	return d, nil
}
func (s *ProjectStore) localJobDocuments(id string) ([]ProjectDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return nil, err
	}
	dir, err := projectDirectory(s.root, id)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	info, err := dir.Lstat("jobs")
	if os.IsNotExist(err) {
		return []ProjectDocument{}, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("local jobs must use a real directory")
	}
	f, err := dir.Open("jobs")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	rows := []ProjectDocument{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		rel := "jobs/" + entry.Name()
		if err := safeProjectPath(dir, rel, false); err != nil {
			return nil, err
		}
		data, err := dir.ReadFile(rel)
		if err != nil {
			return nil, err
		}
		var job ProjectDocument
		if json.Unmarshal(data, &job) != nil || job == nil {
			return nil, fmt.Errorf("invalid local job receipt")
		}
		rows = append(rows, job)
	}
	return rows, nil
}
func (d *DrawingService) save(j ProjectDocument) error {
	return d.store.writeAuxJSON(shotText(j, "project_id"), "jobs/"+shotText(j, "id")+".json", j)
}
func (d *DrawingService) update(id string, patch ProjectDocument) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	j := d.jobs[id]
	for k, v := range patch {
		j[k] = v
	}
	return d.save(j)
}
func (d *DrawingService) Status(project string) []ProjectDocument {
	d.mu.Lock()
	defer d.mu.Unlock()
	rows := []ProjectDocument{}
	for _, j := range d.jobs {
		if shotText(j, "project_id") == project {
			var copy ProjectDocument
			json.Unmarshal(rawValue(j), &copy)
			rows = append(rows, copy)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return shotText(rows[i], "created_at") > shotText(rows[j], "created_at") })
	return rows
}
func (d *DrawingService) Start(id, sid string, rev int, mode, inputID string, strength float64) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return "", fmt.Errorf("local drawing is stopping")
	}
	d.store.workflowMu.Lock()
	defer d.store.workflowMu.Unlock()
	p, err := d.store.Load(id)
	if err != nil {
		return "", err
	}
	if documentRevision(p) != rev {
		return "", ErrRevision
	}
	shot, err := findShot(p, sid)
	if err != nil {
		return "", err
	}
	if (mode != "text" && mode != "image") || (mode == "image" && inputID == "") || (mode == "text" && inputID != "") || !finite(strength) || strength < .1 || strength > .85 {
		return "", fmt.Errorf("choose text or image variation with strength between 0.1 and 0.85")
	}
	if boolean(p, "music_video") && strings.TrimSpace(shotText(shot, "local_prompt")) == "" {
		return "", fmt.Errorf("add a short visual drawing prompt for this music-video shot")
	}
	prompt, negative := localDrawingPrompt(p, shot), strings.TrimSpace(shotText(shot, "local_negative_prompt"))
	if len([]rune(prompt)) > 1200 || len([]rune(negative)) > 1000 {
		return "", fmt.Errorf("keep the drawing prompt under 1200 characters and exclusions under 1000")
	}
	width, height := 1024, 576
	if shotText(p, "aspect") == "9:16" {
		width, height = 576, 1024
	} else if shotText(p, "aspect") != "16:9" {
		return "", fmt.Errorf("local drafting supports 16:9 and 9:16")
	}
	active := 0
	for _, j := range d.jobs {
		if containsField([]string{"queued", "running"}, shotText(j, "status")) {
			active++
			if shotText(j, "project_id") == id && shotText(j, "shot_id") == sid {
				return "", fmt.Errorf("this shot already has a local draft queued")
			}
		}
	}
	if active >= 30 {
		return "", fmt.Errorf("local drawing queue is full")
	}
	jid, err := newStorageID()
	if err != nil {
		return "", err
	}
	var seed [4]byte
	if _, err = rand.Read(seed[:]); err != nil {
		return "", err
	}
	job := ProjectDocument{"id": rawValue(jid), "project_id": rawValue(id), "shot_id": rawValue(sid), "status": rawValue("queued"), "created_at": rawValue(stamp()), "message": rawValue("Waiting for the local image slot."), "seed": rawValue(binary.LittleEndian.Uint32(seed[:]) & 0x7fffffff), "prompt": rawValue(prompt), "negative_prompt": rawValue(negative), "mode": rawValue(mode), "strength": rawValue(nil), "samples": rawValue([]any{}), "width": rawValue(width), "height": rawValue(height), "aspect": p["aspect"], "basis": rawValue(directionBasis(p, shot))}
	if mode == "image" {
		var image ProjectDocument
		for _, ref := range list(p, "visual_references") {
			if shotText(ref, "id") == inputID {
				image = ref
			}
		}
		for _, s := range list(p, "shots") {
			for _, a := range list(s, "assets") {
				if shotText(a, "id") == inputID && containsField([]string{"sketch", "keyframe"}, shotText(a, "kind")) && shotText(a, "binned_at") == "" {
					image = a
				}
			}
		}
		if image == nil {
			return "", fmt.Errorf("choose an active saved image")
		}
		f, err := d.store.OpenMedia(id, shotText(image, "path"))
		if err != nil {
			return "", err
		}
		data, err := io.ReadAll(io.LimitReader(f, maxUpload+1))
		f.Close()
		if err != nil || int64(len(data)) > maxUpload {
			return "", fmt.Errorf("input image is unavailable or too large")
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != shotText(image, "sha256") {
			return "", fmt.Errorf("input image changed; import it again")
		}
		snapshot := "jobs/" + jid + "/input" + filepath.Ext(shotText(image, "path"))
		if err = d.store.writeAux(id, snapshot, data); err != nil {
			return "", err
		}
		job["input_image"] = rawValue(ProjectDocument{"id": image["id"], "path": image["path"], "sha256": image["sha256"], "snapshot_path": rawValue(snapshot), "use": rawValue("image-to-image starting canvas; not a character identity adapter")})
		job["input_transform"], job["strength"] = rawValue("CLI aspect-preserving scale and center crop to output frame"), rawValue(strength)
	}
	if err = d.save(job); err != nil {
		return "", err
	}
	d.jobs[jid] = job
	ctx, cancel := context.WithCancel(context.Background())
	d.cancels.Store(jid, drawingCancellation{id, cancel})
	d.wg.Add(1)
	go d.run(ctx, jid)
	return jid, nil
}

type drawingCancellation struct {
	project string
	cancel  context.CancelFunc
}

func (d *DrawingService) Cancel(project, id string) error {
	value, ok := d.cancels.Load(id)
	if !ok {
		return fmt.Errorf("local job is already finished or unavailable")
	}
	entry := value.(drawingCancellation)
	if entry.project != project {
		return fmt.Errorf("choose a local job from this project")
	}
	entry.cancel()
	return nil
}
func (d *DrawingService) Close() {
	d.cancels.Range(func(_, v any) bool { v.(drawingCancellation).cancel(); return true })
	d.mu.Lock()
	d.closed = true
	d.mu.Unlock()
	d.cancels.Range(func(_, v any) bool { v.(drawingCancellation).cancel(); return true })
	d.wg.Wait()
}

func (d *DrawingService) run(parent context.Context, id string) {
	defer d.wg.Done()
	defer func() {
		if value, ok := d.cancels.LoadAndDelete(id); ok {
			value.(drawingCancellation).cancel()
		}
	}()

	select {
	case d.slot <- struct{}{}:
		defer func() { <-d.slot }()
	case <-parent.Done():
		d.update(id, ProjectDocument{"status": rawValue("cancelled"), "message": rawValue("Local draft cancelled before rendering."), "ended_at": rawValue(stamp())})
		return
	}
	d.mu.Lock()
	var job ProjectDocument
	json.Unmarshal(rawValue(d.jobs[id]), &job)
	d.mu.Unlock()
	ctx, cancel := context.WithTimeout(parent, 300*time.Second)
	defer cancel()
	err := d.render(ctx, job)
	if err != nil {
		status := "blocked"
		if parent.Err() != nil {
			status = "cancelled"
		}
		d.update(id, ProjectDocument{"status": rawValue(status), "message": rawValue(err.Error()), "ended_at": rawValue(stamp())})
	}
}
func drawingArgs(j ProjectDocument, folder string, c AppSettings) []string {
	args := []string{"generate", "--models-dir", c.ModelsDir, "--model", c.Model, "--prompt-file", filepath.Join(folder, "prompt.txt"), "--negative-prompt", shotText(j, "negative_prompt"), "--config-file", filepath.Join(folder, "config.json"), "--width", fmt.Sprint(int(number(j, "width"))), "--height", fmt.Sprint(int(number(j, "height"))), "--steps", "4", "--cfg", "2", "--seed", fmt.Sprint(int(number(j, "seed"))), "--output", filepath.Join(folder, "draft.png"), "--local", "--offline", "--no-download-missing", "--disable-preview"}
	if image := object(j, "input_image"); image != nil {
		args = append(args, "--image", filepath.Join(folder, filepath.Base(shotText(image, "snapshot_path"))), "--strength", fmt.Sprint(number(j, "strength")))
	}
	return args
}
func (d *DrawingService) render(ctx context.Context, job ProjectDocument) error {
	if err := d.preflight(); err != nil {
		return err
	}
	id, pid := shotText(job, "id"), shotText(job, "project_id")
	base := "jobs/" + id
	image := object(job, "input_image")
	if image != nil {
		data, err := d.store.readAux(pid, shotText(image, "snapshot_path"))
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != shotText(image, "sha256") {
			return fmt.Errorf("queued input changed; prepare a fresh variation")
		}
	}
	if err := d.store.writeAux(pid, base+"/prompt.txt", []byte(shotText(job, "prompt"))); err != nil {
		return err
	}
	if err := d.store.writeAuxJSON(pid, base+"/config.json", ProjectDocument{"sampler": rawValue(10), "batchSize": rawValue(1), "batchCount": rawValue(1), "hiresFix": rawValue(false), "controls": rawValue([]any{}), "loras": rawValue([]any{})}); err != nil {
		return err
	}
	settings := ProjectDocument{"width": job["width"], "height": job["height"], "steps": rawValue(4), "cfg": rawValue(2), "sampler": rawValue(10), "mode": job["mode"], "strength": job["strength"]}
	if err := d.update(id, ProjectDocument{"status": rawValue("running"), "started_at": rawValue(stamp()), "message": rawValue("Drawing one local composition."), "model": rawValue(d.config.Model), "settings": rawValue(settings)}); err != nil {
		return err
	}
	folder := filepath.Join(d.store.path, pid, base)
	log, err := os.OpenFile(filepath.Join(folder, "render.log"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer log.Close()
	cmd := exec.CommandContext(ctx, d.config.DrawCLI, drawingArgs(job, folder, d.config)...)
	configureLocalCommand(cmd)
	cmd.Stdout, cmd.Stderr = log, log
	cmd.Env = childEnvironment()
	if err = cmd.Start(); err != nil {
		return fmt.Errorf("local renderer could not start; inspect the retained job log")
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()
	critical := 0
	samples := []ProjectDocument{}
	for running := true; running; {
		select {
		case err = <-done:
			running = false
		case <-ticker.C:
			sample := d.metrics()
			samples = append(samples, sample)
			if shotText(sample, "pressure") == "critical" {
				critical++
			} else {
				critical = 0
			}
			if e := d.update(id, ProjectDocument{"samples": rawValue(samples)}); e != nil {
				cmd.Cancel()
				<-done
				return e
			}
			if critical >= 2 {
				cmd.Cancel()
				<-done
				return fmt.Errorf("draft stopped after sustained critical memory pressure")
			}
		}
	}
	if ctx.Err() != nil {
		return fmt.Errorf("local draft cancelled or stopped after the five-minute limit")
	}
	if err != nil {
		return fmt.Errorf("local renderer did not finish; job log retained")
	}
	if err = d.update(id, ProjectDocument{"result_file": rawValue(base + "/draft.png")}); err != nil {
		return err
	}
	return d.importResult(job, settings)
}
func (d *DrawingService) importResult(job, settings ProjectDocument) error {
	pid, jid := shotText(job, "project_id"), shotText(job, "id")
	data, err := d.store.readAux(pid, "jobs/"+jid+"/draft.png")
	if err != nil || !mediaSignature(".png", data[:min(len(data), 32)]) {
		return fmt.Errorf("renderer returned no valid PNG")
	}
	p, err := d.store.Load(pid)
	if err != nil {
		return err
	}
	provenance := ProjectDocument{"integration": rawValue("local"), "job_id": rawValue(jid), "prompt": job["prompt"], "negative_prompt": rawOr(job, "negative_prompt", ""), "seed": job["seed"], "model": rawValue(d.config.Model), "settings": rawValue(settings), "input_image": rawOr(job, "input_image", nil), "input_transform": rawOr(job, "input_transform", nil), "basis": rawOr(job, "basis", "")}
	if _, err = d.store.importMedia(pid, shotText(job, "shot_id"), "sketch", "draw-things.png", int64(len(data)), bytes.NewReader(data), documentRevision(p), "", "", provenance); err != nil {
		return err
	}
	return d.update(jid, ProjectDocument{"status": rawValue("done"), "ended_at": rawValue(stamp()), "message": rawValue("Composition saved unselected. Review before selecting it.")})
}
func (d *DrawingService) Recover(pid, id string) error {
	d.mu.Lock()
	j, ok := d.jobs[id]
	if !ok || shotText(j, "project_id") != pid {
		d.mu.Unlock()
		return fmt.Errorf("choose an existing local job")
	}
	var copy ProjectDocument
	json.Unmarshal(rawValue(j), &copy)
	d.mu.Unlock()
	if shotText(copy, "result_file") == "" {
		return fmt.Errorf("no saved result to import")
	}
	return d.importResult(copy, object(copy, "settings"))
}
func memoryMetrics(meter string) ProjectDocument {
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, meter, "--snapshot").Output()
	var p ProjectDocument
	if err != nil || len(out) > 1<<20 || json.Unmarshal(out, &p) != nil || p == nil {
		return ProjectDocument{"pressure": rawValue("unavailable"), "sampledAt": rawValue(stamp())}
	}
	return p
}
func drawingPreflight(c AppSettings, metrics func() ProjectDocument) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("Draw Things is an optional Mac integration; use imported images or cloud generation on this system")
	}
	if _, err := os.Stat(c.DrawCLI); err != nil {
		return fmt.Errorf("configure Draw Things CLI before local drafting")
	}
	if _, err := os.Stat(filepath.Join(c.ModelsDir, c.Model)); err != nil {
		return fmt.Errorf("configure the local model before drafting")
	}
	if shotText(metrics(), "pressure") != "normal" {
		return fmt.Errorf("local drafting requires verified normal memory pressure from Mini Meter")
	}
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Get("http://127.0.0.1:11434/api/ps")
	if err != nil {
		if errors.Is(err, syscall.ECONNREFUSED) {
			return nil
		}
		return fmt.Errorf("cannot verify local text-model state; finish that work before retrying")
	}
	defer response.Body.Close()
	var doc ProjectDocument
	if response.StatusCode != 200 || json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&doc) != nil {
		return fmt.Errorf("cannot verify local text-model state")
	}
	if len(list(doc, "models")) > 0 {
		return fmt.Errorf("a local text model is loaded; finish and unload it before drafting")
	}
	return nil
}
