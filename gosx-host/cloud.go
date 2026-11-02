package main

import (
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed cloud_catalog.json
var cachedCatalog []byte

type CloudService struct {
	mu                sync.Mutex
	store             *ProjectStore
	provider          generationProvider
	catalog           ProjectDocument
	jobs              map[string]ProjectDocument
	loaded, connected bool
	message           string
}

func (s *ProjectStore) Cloud() (*CloudService, error) {
	s.featuresMu.Lock()
	defer s.featuresMu.Unlock()
	if s.cloud == nil {
		c, err := newCloudService(s, newOpenRouterProvider())
		if err != nil {
			return nil, err
		}
		s.cloud = c
	}
	return s.cloud, nil
}
func newCloudService(s *ProjectStore, provider generationProvider) (*CloudService, error) {
	var catalog ProjectDocument
	if err := json.Unmarshal(cachedCatalog, &catalog); err != nil {
		return nil, err
	}
	saved, err := s.auxJSON("", "cloud/catalog.json", catalog)
	if err != nil {
		return nil, err
	}
	return &CloudService{store: s, provider: provider, catalog: saved, jobs: map[string]ProjectDocument{}, message: "Check OpenRouter before generating. Every paid request needs a reviewed confirmation."}, nil
}
func (c *CloudService) load() error {
	if c.loaded {
		return nil
	}
	jobs, err := c.store.auxDocuments("cloud/jobs")
	if err != nil {
		return err
	}
	for _, j := range jobs {
		id := shotText(j, "id")
		var reserved float64
		if json.Unmarshal(j["reserved_usd"], &reserved) != nil || nullJSON(j["reserved_usd"]) || !finite(reserved) || reserved < 0 {
			return fmt.Errorf("invalid saved reservation; reconcile jobs before generating")
		}
		if v, ok := j["actual_cost_usd"]; ok && !nullJSON(v) {
			var cost float64
			if json.Unmarshal(v, &cost) != nil || !finite(cost) || cost < 0 {
				return fmt.Errorf("invalid saved charge; reconcile jobs before generating")
			}
		}
		if !preparedJobID.MatchString(id) || !projectIDPattern.MatchString(shotText(j, "project_id")) || !finite(number(j, "reserved_usd")) || number(j, "reserved_usd") < 0 {
			return fmt.Errorf("unrecognized generation receipt; reconcile saved jobs before generating")
		}
		if shotText(j, "status") == "submitting" && shotText(j, "remote_id") == "" && shotText(j, "result_file") == "" {
			j["status"], j["message"] = rawValue("unknown"), rawValue("Interrupted during submission. Reconcile provider activity before creating another request.")
			if err = c.save(j); err != nil {
				return err
			}
		}
		c.jobs[id] = j
	}
	c.loaded = true
	return nil
}
func (c *CloudService) save(j ProjectDocument) error {
	return c.store.writeAuxJSON("", "cloud/jobs/"+shotText(j, "id")+".json", j)
}
func (c *CloudService) update(j ProjectDocument, patch ProjectDocument) error {
	for k, v := range patch {
		j[k] = v
	}
	return c.save(j)
}
func (c *CloudService) spent(id string) float64 {
	sum := 0.0
	for _, j := range c.jobs {
		if shotText(j, "project_id") != id || shotText(j, "status") == "cancelled" {
			continue
		}
		cost := number(j, "reserved_usd")
		if v, ok := j["actual_cost_usd"]; ok && !nullJSON(v) && finite(number(j, "actual_cost_usd")) && number(j, "actual_cost_usd") >= 0 {
			cost = number(j, "actual_cost_usd")
		}
		sum += cost
	}
	return sum
}
func (c *CloudService) brief(id string) (ProjectDocument, error) {
	return c.store.auxJSON(id, "cloud/production-brief.json", nil)
}
func (c *CloudService) config(id string) (ProjectDocument, error) {
	return c.store.auxJSON(id, "cloud/config.json", ProjectDocument{"allowance_usd": rawValue(0)})
}
func (c *CloudService) status(id string) (ProjectDocument, error) {
	if err := c.load(); err != nil {
		return nil, err
	}
	if _, err := c.store.Load(id); err != nil {
		return nil, err
	}
	config, err := c.config(id)
	if err != nil {
		return nil, err
	}
	brief, err := c.brief(id)
	if err != nil {
		return nil, err
	}
	jobs := []ProjectDocument{}
	for _, j := range c.jobs {
		if shotText(j, "project_id") == id {
			jobs = append(jobs, j)
		}
	}
	sort.Slice(jobs, func(i, j int) bool { return shotText(jobs[i], "created_at") > shotText(jobs[j], "created_at") })
	images, videos := []ProjectDocument{}, []ProjectDocument{}
	for _, m := range list(c.catalog, "images") {
		if containsField(imageModels, shotText(m, "id")) {
			images = append(images, m)
		}
	}
	for _, m := range list(c.catalog, "videos") {
		if containsField(videoModels, shotText(m, "id")) {
			videos = append(videos, m)
		}
	}
	return ProjectDocument{"connected": rawValue(c.connected), "connection_message": rawValue(c.message), "catalog_observed_at": rawOr(c.catalog, "observed_at", ""), "image_models": rawValue(images), "video_models": rawValue(videos), "allowance_usd": rawOr(config, "allowance_usd", 0), "committed_usd": rawValue(c.spent(id)), "jobs": rawValue(jobs), "brief": rawValue(brief)}, nil
}
func (c *CloudService) Status(id string) (ProjectDocument, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	state, err := c.status(id)
	if err != nil {
		return nil, err
	}
	var copy ProjectDocument
	json.Unmarshal(rawValue(state), &copy)
	return copy, nil
}
func (c *CloudService) Connect(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connected = false
	c.message = "Connection is not verified. Check the server key and provider account before generating."
	if _, err := c.store.Load(id); err != nil {
		return err
	}
	if err := c.provider.Ready(); err != nil {
		return err
	}
	if _, err := c.provider.Call("auth/key", nil, true); err != nil {
		return err
	}
	images, err := c.provider.Call("images/models", nil, false)
	if err != nil {
		return err
	}
	videos, err := c.provider.Call("videos/models", nil, false)
	if err != nil {
		return err
	}
	catalog := ProjectDocument{"observed_at": rawValue(stamp()), "images": rawOr(images, "data", []any{}), "videos": rawOr(videos, "data", []any{})}
	if err = c.store.writeAuxJSON("", "cloud/catalog.json", catalog); err != nil {
		return err
	}
	c.catalog, c.connected, c.message = catalog, true, "OpenRouter connected. Model catalog refreshed; generation still requires a reviewed confirmation."
	return nil
}
func (c *CloudService) Allowance(id string, value float64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.load(); err != nil {
		return err
	}
	if _, err := c.store.Load(id); err != nil {
		return err
	}
	if !finite(value) || value < 0 || value > 1000 || value+1e-6 < c.spent(id) {
		return fmt.Errorf("allowance must be between existing charges/reservations and $1,000")
	}
	return c.store.writeAuxJSON(id, "cloud/config.json", ProjectDocument{"allowance_usd": rawValue(value), "at": rawValue(stamp())})
}
func (c *CloudService) CreateBrief(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, err := c.store.Load(id)
	if err != nil {
		return err
	}
	shots := []ProjectDocument{}
	for _, s := range list(p, "shots") {
		prompt := promptPackage(p, s, "grok")
		shots = append(shots, ProjectDocument{"shot_id": s["id"], "title": rawOr(s, "title", ""), "image_prompt": prompt["composition"], "video_prompt": prompt["animation"]})
	}
	content := ProjectDocument{"title": p["title"], "brief": rawOr(p, "brief", ""), "aspect": p["aspect"], "look": rawOr(p, "look", ""), "continuity": rawOr(p, "continuity", ""), "shots": rawValue(shots)}
	brief := ProjectDocument{"content": rawValue(content), "sha256": rawValue(pythonDigest(content)), "created_at": rawValue(stamp())}
	return c.store.writeAuxJSON(id, "cloud/production-brief.json", brief)
}
func (c *CloudService) ApproveBrief(id, hash string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	brief, err := c.brief(id)
	if err != nil {
		return err
	}
	if brief == nil || pythonDigest(object(brief, "content")) != hash {
		return fmt.Errorf("the production brief changed; review it again")
	}
	brief["approved_at"], brief["approved_sha256"] = rawValue(stamp()), rawValue(hash)
	return c.store.writeAuxJSON(id, "cloud/production-brief.json", brief)
}
func cloudImages(p ProjectDocument) []ProjectDocument {
	rows := []ProjectDocument{}
	for _, ref := range list(p, "visual_references") {
		r := ProjectDocument{}
		for k, v := range ref {
			r[k] = v
		}
		r["label"] = rawValue("Reference · " + shotText(ref, "title"))
		rows = append(rows, r)
	}
	for _, s := range list(p, "shots") {
		for _, a := range list(s, "assets") {
			if shotText(a, "binned_at") != "" {
				continue
			}
			kind := shotText(a, "kind")
			if kind == "sketch" || kind == "keyframe" {
				r := ProjectDocument{}
				for k, v := range a {
					r[k] = v
				}
				r["label"] = rawValue(fmt.Sprintf("%s · %s take %g", shotText(s, "title"), kind, number(a, "take")))
				rows = append(rows, r)
			} else if kind == "clip" && shotText(a, "poster_path") != "" && object(a, "source_ref") == nil {
				rows = append(rows, ProjectDocument{"id": rawValue("poster-" + shotText(a, "id")), "path": a["poster_path"], "sha256": rawOr(a, "poster_sha256", nil), "label": rawValue(fmt.Sprintf("%s · clip %g poster (layout reference)", shotText(s, "title"), number(a, "take")))})
			}
		}
	}
	return rows
}
func (c *CloudService) input(p ProjectDocument, iid string) (ProjectDocument, []byte, error) {
	for _, row := range cloudImages(p) {
		if shotText(row, "id") != iid {
			continue
		}
		rel := shotText(row, "path")
		if _, err := assetExtension("reference", rel); err != nil {
			return nil, nil, err
		}
		f, err := c.store.OpenMedia(shotText(p, "id"), rel)
		if err != nil {
			return nil, nil, err
		}
		info, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, nil, err
		}
		if info.Size() <= 0 || info.Size() > 10<<20 {
			f.Close()
			return nil, nil, fmt.Errorf("use reference images smaller than 10 MiB")
		}
		data, err := io.ReadAll(io.LimitReader(f, 10<<20+1))
		f.Close()
		if err != nil {
			return nil, nil, err
		}
		sum := sha256.Sum256(data)
		hash := hex.EncodeToString(sum[:])
		if old := shotText(row, "sha256"); old != "" && old != hash {
			return nil, nil, fmt.Errorf("reference bytes changed; import the image again")
		}
		return ProjectDocument{"id": rawValue(iid), "path": rawValue(rel), "sha256": rawValue(hash), "title": rawOr(row, "label", "")}, data, nil
	}
	return nil, nil, fmt.Errorf("choose an active saved image from this project")
}
func stringsList(p ProjectDocument, key string) []string {
	var v []string
	json.Unmarshal(p[key], &v)
	return v
}
func (c *CloudService) Prepare(id, sid string, rev int, raw ProjectDocument) (ProjectDocument, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, err := c.store.Load(id)
	if err != nil {
		return nil, err
	}
	if documentRevision(p) != rev {
		return nil, ErrRevision
	}
	s, err := findShot(p, sid)
	if err != nil {
		return nil, err
	}
	kind, model, prompt := shotText(raw, "kind"), shotText(raw, "model"), strings.TrimSpace(shotText(raw, "prompt"))
	if len([]rune(prompt)) < 1 || len([]rune(prompt)) > 12000 {
		return nil, fmt.Errorf("write a prompt of 1–12,000 characters")
	}
	group := "images"
	allowed := imageModels
	if kind == "video" {
		group = "videos"
		allowed = videoModels
	} else if kind != "image" {
		return nil, fmt.Errorf("choose image or video")
	}
	if !containsField(allowed, model) {
		return nil, fmt.Errorf("choose a supported Google model")
	}
	var m ProjectDocument
	for _, row := range list(c.catalog, group) {
		if shotText(row, "id") == model {
			m = row
		}
	}
	if m == nil {
		return nil, fmt.Errorf("model absent from the catalog; check connection to refresh it")
	}
	qid, err := newStorageID()
	if err != nil {
		return nil, err
	}
	q := ProjectDocument{"id": rawValue(qid), "project_id": rawValue(id), "shot_id": rawValue(sid), "revision": rawValue(rev), "kind": rawValue(kind), "model": rawValue(model), "prompt": rawValue(prompt), "aspect": p["aspect"], "basis": rawValue(directionBasis(p, s)), "created_at": rawValue(stamp()), "starter_id": rawValue(nil), "catalog_sha256": rawValue(pythonDigest(m))}
	refs := []ProjectDocument{}
	if kind == "image" {
		ids := []string{}
		if v, ok := raw["reference_ids"]; ok {
			if nullJSON(v) || json.Unmarshal(v, &ids) != nil {
				return nil, fmt.Errorf("reference IDs must be an array")
			}
		}
		if len(ids) > 6 {
			return nil, fmt.Errorf("choose up to six distinct images")
		}
		seen := map[string]bool{}
		for _, iid := range ids {
			if seen[iid] {
				return nil, fmt.Errorf("choose distinct references")
			}
			seen[iid] = true
			r, _, err := c.input(p, iid)
			if err != nil {
				return nil, err
			}
			refs = append(refs, r)
		}
		params := object(m, "supported_parameters")
		if !containsField(stringsList(object(params, "aspect_ratio"), "values"), shotText(p, "aspect")) || !containsField(stringsList(object(params, "resolution"), "values"), "1K") {
			return nil, fmt.Errorf("image model does not advertise this frame and resolution")
		}
		q["resolution"], q["reserved_usd"], q["price_note"] = rawValue("1K"), rawValue(1.0), rawValue("Conservative $1 image reservation. Actual token-based cost is reported afterward; this is not a provider-enforced maximum charge.")
	} else {
		if !imageApproved(p, s) {
			return nil, fmt.Errorf("select and approve a finished starter image first")
		}
		duration := number(raw, "duration")
		resolution := shotText(raw, "resolution")
		var durations []float64
		json.Unmarshal(m["supported_durations"], &durations)
		supported := false
		for _, d := range durations {
			if d == duration {
				supported = true
			}
		}
		if !supported || !containsField(stringsList(m, "supported_resolutions"), resolution) || !containsField(stringsList(m, "supported_aspect_ratios"), shotText(p, "aspect")) || !boolean(m, "generate_audio") || !containsField(stringsList(m, "supported_frame_images"), "first_frame") {
			return nil, fmt.Errorf("model does not advertise this silent frame/duration combination")
		}
		skus := object(m, "pricing_skus")
		suffix := ""
		if resolution == "720p" {
			suffix = "_720p"
		} else if resolution == "4K" {
			suffix = "_4k"
		}
		key := "duration_seconds_without_audio" + suffix
		if _, ok := skus[key]; !ok {
			key = "duration_seconds_without_audio"
		}
		price, err := strconv.ParseFloat(shotText(skus, key), 64)
		if err != nil || !finite(price) || price <= 0 {
			return nil, fmt.Errorf("silent-video pricing unavailable; refresh catalog")
		}
		ref, _, err := c.input(p, shotText(s, "selected_keyframe"))
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
		q["duration"], q["resolution"], q["reserved_usd"], q["generate_audio"], q["starter_id"], q["price_note"] = rawValue(duration), rawValue(resolution), rawValue(float64(int(price*duration*1e4+.5))/1e4), rawValue(false), s["selected_keyframe"], rawValue("Listed silent-video estimate. Actual cost is reconciled when reported.")
	}
	brief, err := c.brief(id)
	if err != nil {
		return nil, err
	}
	q["brief_sha256"] = rawValue(nil)
	if brief != nil {
		q["brief_sha256"] = rawValue(pythonDigest(object(brief, "content")))
	}
	if len(refs) > 0 {
		prompt += "\n\nINPUT IMAGE GUIDE"
		for i, r := range refs {
			prompt += fmt.Sprintf("\nImage %d: %s", i+1, shotText(r, "title"))
		}
		q["prompt"] = rawValue(prompt)
	}
	q["refs"] = rawValue(refs)
	if err = c.store.writeAuxJSON("", "cloud/quotes/"+qid+".json", q); err != nil {
		return nil, err
	}
	return q, nil
}
func (c *CloudService) Submit(qid string, confirmed bool) (ProjectDocument, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store.workflowMu.Lock()
	defer c.store.workflowMu.Unlock()
	if !confirmed {
		return nil, fmt.Errorf("review the exact request and confirm generation explicitly")
	}
	if !preparedJobID.MatchString(qid) {
		return nil, fmt.Errorf("invalid prepared request")
	}
	if err := c.load(); err != nil {
		return nil, err
	}
	if !c.connected {
		return nil, fmt.Errorf("check the OpenRouter connection first")
	}
	if job, ok := c.jobs[qid]; ok {
		return job, nil
	}
	q, err := c.store.auxJSON("", "cloud/quotes/"+qid+".json", nil)
	if err != nil || q == nil {
		return nil, fmt.Errorf("prepared request unavailable")
	}
	created, err := time.Parse(time.RFC3339Nano, shotText(q, "created_at"))
	if err != nil || time.Since(created) < 0 || time.Since(created) > 15*time.Minute {
		return nil, fmt.Errorf("prepared request expired")
	}
	id, sid := shotText(q, "project_id"), shotText(q, "shot_id")
	p, err := c.store.Load(id)
	if err != nil {
		return nil, err
	}
	s, err := findShot(p, sid)
	if err != nil {
		return nil, err
	}
	if documentRevision(p) != int(number(q, "revision")) || directionBasis(p, s) != shotText(q, "basis") {
		return nil, fmt.Errorf("project or starter changed; prepare a fresh request")
	}
	group := "images"
	if shotText(q, "kind") == "video" {
		group = "videos"
	}
	var model ProjectDocument
	for _, m := range list(c.catalog, group) {
		if shotText(m, "id") == shotText(q, "model") {
			model = m
		}
	}
	if model == nil || pythonDigest(model) != shotText(q, "catalog_sha256") {
		return nil, fmt.Errorf("model capabilities or prices changed; prepare a fresh request")
	}
	refs := list(q, "refs")
	snapshots := [][]byte{}
	for _, ref := range refs {
		current, data, err := c.input(p, shotText(ref, "id"))
		if err != nil {
			return nil, err
		}
		if shotText(current, "sha256") != shotText(ref, "sha256") {
			return nil, fmt.Errorf("an input changed; prepare again")
		}
		snapshots = append(snapshots, data)
	}
	brief, err := c.brief(id)
	if err != nil {
		return nil, err
	}
	hash := shotText(q, "brief_sha256")
	if hash == "" || brief == nil || shotText(brief, "approved_sha256") != hash || pythonDigest(object(brief, "content")) != hash {
		return nil, fmt.Errorf("review and approve the production brief before generating")
	}
	config, err := c.config(id)
	if err != nil {
		return nil, err
	}
	reserved := number(q, "reserved_usd")
	if !finite(reserved) || reserved <= 0 || reserved+c.spent(id) > number(config, "allowance_usd")+1e-6 {
		return nil, fmt.Errorf("request exceeds the remaining project allowance")
	}
	for _, j := range c.jobs {
		if shotText(j, "project_id") == id && shotText(j, "shot_id") == sid && containsField([]string{"queued", "submitting", "pending", "in_progress", "downloading", "importing"}, shotText(j, "status")) {
			return nil, fmt.Errorf("this shot already has an active cloud job")
		}
	}
	if err = c.provider.Ready(); err != nil {
		return nil, err
	}
	for i, ref := range refs {
		snapshot := fmt.Sprintf("inputs/%s/%d%s", qid, i, filepath.Ext(shotText(ref, "path")))
		if err = c.store.writeAux("", "cloud/"+snapshot, snapshots[i]); err != nil {
			return nil, err
		}
		ref["snapshot"] = rawValue(snapshot)
	}
	q["refs"] = rawValue(refs)
	q["status"], q["message"], q["actual_cost_usd"] = rawValue("queued"), rawValue("Confirmed request queued for one provider submission."), rawValue(nil)
	if err = c.save(q); err != nil {
		return nil, err
	}
	c.jobs[qid] = q
	return q, nil
}
func (c *CloudService) body(job ProjectDocument) (ProjectDocument, error) {
	images := []ProjectDocument{}
	for _, ref := range list(job, "refs") {
		snapshot := shotText(ref, "snapshot")
		data, err := c.store.readAux("", "cloud/"+snapshot)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != shotText(ref, "sha256") {
			return nil, fmt.Errorf("saved generation input changed")
		}
		mediaType := mime.TypeByExtension(filepath.Ext(snapshot))
		images = append(images, ProjectDocument{"type": rawValue("image_url"), "image_url": rawValue(ProjectDocument{"url": rawValue("data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data))})})
	}
	body := ProjectDocument{}
	for _, k := range []string{"model", "prompt", "aspect", "resolution"} {
		name := k
		if k == "aspect" {
			name = "aspect_ratio"
		}
		body[name] = job[k]
	}
	if shotText(job, "kind") == "image" {
		body["n"], body["input_references"] = rawValue(1), rawValue(images)
	} else {
		if len(images) != 1 {
			return nil, fmt.Errorf("video needs one saved starter")
		}
		images[0]["frame_type"] = rawValue("first_frame")
		body["duration"], body["generate_audio"], body["frame_images"] = job["duration"], rawValue(false), rawValue(images)
	}
	return body, nil
}
