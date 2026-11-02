package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
)

func providerCost(result ProjectDocument) json.RawMessage {
	usage := object(result, "usage")
	value, ok := usage["cost"]
	if !ok || nullJSON(value) {
		return rawValue(nil)
	}
	var cost float64
	if json.Unmarshal(value, &cost) != nil {
		var text string
		if json.Unmarshal(value, &text) != nil {
			return rawValue(nil)
		}
		var err error
		cost, err = strconv.ParseFloat(text, 64)
		if err != nil {
			return rawValue(nil)
		}
	}
	if !finite(cost) || cost < 0 {
		return rawValue(nil)
	}
	return rawValue(cost)
}

// Process advances an existing confirmed job. Only a durably queued job can make
// a new submission; pending/unknown jobs use their existing remote ID or result.
// It runs inside the user action, so shutdown retains ownership until it finishes.
func (c *CloudService) Process(jid string) (ProjectDocument, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.load(); err != nil {
		return nil, err
	}
	j, ok := c.jobs[jid]
	if !ok {
		return nil, fmt.Errorf("choose an existing cloud job")
	}
	err := c.process(j)
	if err != nil {
		state := "blocked"
		if shotText(j, "remote_id") != "" || shotText(j, "result_file") != "" {
			state = "recoverable"
		} else if shotText(j, "status") == "submitting" || shotText(j, "status") == "unknown" {
			state = "unknown"
		}
		patch := ProjectDocument{"status": rawValue(state), "message": rawValue(err.Error()), "ended_at": rawValue(stamp())}
		if e := c.update(j, patch); e != nil {
			return j, fmt.Errorf("job state could not be saved; reconcile provider activity before generating again")
		}
	}
	return j, err
}
func (c *CloudService) process(j ProjectDocument) error {
	if shotText(j, "asset_id") != "" {
		return c.update(j, ProjectDocument{"status": rawValue("done"), "message": rawValue("Take already imported; review before selecting.")})
	}
	jid := shotText(j, "id")
	if !preparedJobID.MatchString(jid) {
		return fmt.Errorf("invalid saved job identifier")
	}
	if shotText(j, "result_file") == "" {
		if shotText(j, "kind") == "image" {
			if shotText(j, "status") != "queued" {
				return fmt.Errorf("no recoverable result; reconcile the existing image request before creating another")
			}
			body, err := c.body(j)
			if err != nil {
				return err
			}
			if err = c.provider.Ready(); err != nil {
				return err
			}
			if err = c.update(j, ProjectDocument{"status": rawValue("submitting"), "message": rawValue("Creating one reference-conditioned image.")}); err != nil {
				return err
			}
			result, err := c.provider.Call("images", body, true)
			if err != nil {
				return err
			}
			if err = c.update(j, ProjectDocument{"actual_cost_usd": providerCost(result)}); err != nil {
				return err
			}
			items := list(result, "data")
			if len(items) != 1 {
				return fmt.Errorf("unexpected image result count; reconcile provider activity")
			}
			mediaType := shotText(items[0], "media_type")
			if mediaType == "" {
				mediaType = "image/png"
			}
			ext := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp"}[mediaType]
			if ext == "" {
				return fmt.Errorf("unsupported generated image format; result needs manual review")
			}
			payload, err := base64.StdEncoding.Strict().DecodeString(shotText(items[0], "b64_json"))
			if err != nil || int64(len(payload)) > maxUpload || !mediaSignature(ext, payload[:min(len(payload), 32)]) {
				return fmt.Errorf("generated image data is invalid; reconcile the existing request")
			}
			rel := "results/" + jid + "/image" + ext
			if err = c.store.writeAux("", "cloud/"+rel, payload); err != nil {
				return err
			}
			if err = c.update(j, ProjectDocument{"result_file": rawValue(rel), "status": rawValue("importing")}); err != nil {
				return err
			}
		} else if shotText(j, "kind") == "video" {
			rid := shotText(j, "remote_id")
			if rid == "" {
				if shotText(j, "status") != "queued" {
					return fmt.Errorf("no recoverable provider ID; reconcile activity before creating another job")
				}
				body, err := c.body(j)
				if err != nil {
					return err
				}
				if err = c.provider.Ready(); err != nil {
					return err
				}
				if err = c.update(j, ProjectDocument{"status": rawValue("submitting"), "message": rawValue("Submitting one silent video take.")}); err != nil {
					return err
				}
				result, err := c.provider.Call("videos", body, true)
				if err != nil {
					return err
				}
				rid = shotText(result, "id")
				if !remoteJobID.MatchString(rid) {
					return fmt.Errorf("provider returned no recoverable job ID; reconcile activity")
				}
				if err = c.update(j, ProjectDocument{"remote_id": rawValue(rid), "status": rawValue("pending"), "message": rawValue("Video is working; check this existing job to continue.")}); err != nil {
					return err
				}
			}
			if !remoteJobID.MatchString(rid) {
				return fmt.Errorf("invalid saved provider job ID")
			}
			result, err := c.provider.Call("videos/"+rid, nil, true)
			if err != nil {
				return err
			}
			status := shotText(result, "status")
			switch status {
			case "pending", "in_progress":
				return c.update(j, ProjectDocument{"status": rawValue(status), "message": rawValue("Video is generating; checking this job does not submit another request.")})
			case "failed":
				return c.update(j, ProjectDocument{"status": rawValue("failed"), "actual_cost_usd": providerCost(result), "message": rawValue("Provider reported failure. Inspect account activity before creating another take."), "ended_at": rawValue(stamp())})
			case "completed":
				if err = c.update(j, ProjectDocument{"status": rawValue("downloading"), "actual_cost_usd": providerCost(result)}); err != nil {
					return err
				}
				payload, err := c.provider.Download(rid)
				if err != nil {
					return err
				}
				if int64(len(payload)) > maxUpload || !mediaSignature(".mp4", payload[:min(len(payload), 32)]) {
					return fmt.Errorf("generated video is invalid; download retained for investigation where available")
				}
				rel := "results/" + jid + "/video.mp4"
				if err = c.store.writeAux("", "cloud/"+rel, payload); err != nil {
					return err
				}
				if err = c.update(j, ProjectDocument{"result_file": rawValue(rel), "status": rawValue("importing")}); err != nil {
					return err
				}
			default:
				return fmt.Errorf("unfamiliar provider status; check this existing job again")
			}
		} else {
			return fmt.Errorf("invalid saved generation kind")
		}
	}
	rel := shotText(j, "result_file")
	if len(rel) < len("results/"+jid+"/") || rel[:len("results/"+jid+"/")] != "results/"+jid+"/" {
		return fmt.Errorf("invalid saved result path")
	}
	data, err := c.store.readAux("", "cloud/"+rel)
	if err != nil {
		return err
	}
	if shotText(j, "kind") == "video" {
		path := filepath.Join(c.store.path, "cloud", rel)
		info, err := probeVideo(path)
		if err != nil {
			return err
		}
		if number(info, "audio_tracks") != 0 || number(info, "duration_seconds") <= 0 || number(info, "duration_seconds") > 7200 {
			return fmt.Errorf("requested silent video contains audio or could not be verified; saved for investigation, not imported")
		}
	}
	p, err := c.store.Load(shotText(j, "project_id"))
	if err != nil {
		return err
	}
	shot, err := findShot(p, shotText(j, "shot_id"))
	if err != nil {
		return err
	}
	var asset ProjectDocument
	for _, a := range list(shot, "assets") {
		if shotText(object(a, "cloud_generation"), "job_id") == jid {
			asset = a
			break
		}
	}
	if asset == nil {
		provenance := ProjectDocument{}
		for _, k := range []string{"model", "kind", "prompt", "refs", "basis", "starter_id", "generate_audio", "duration", "resolution", "actual_cost_usd", "remote_id"} {
			if v, ok := j[k]; ok {
				provenance[k] = v
			}
		}
		provenance["job_id"] = rawValue(jid)
		kind := "keyframe"
		if shotText(j, "kind") == "video" {
			kind = "clip"
		}
		p, err = c.store.importMedia(shotText(p, "id"), shotText(shot, "id"), kind, filepath.Base(rel), int64(len(data)), bytes.NewReader(data), documentRevision(p), "", "", provenance)
		if err != nil {
			return err
		}
		shot, _ = findShot(p, shotText(j, "shot_id"))
		for _, a := range list(shot, "assets") {
			if shotText(object(a, "cloud_generation"), "job_id") == jid {
				asset = a
				break
			}
		}
	}
	if asset == nil {
		return fmt.Errorf("result import could not be verified; retained for recovery")
	}
	return c.update(j, ProjectDocument{"asset_id": asset["id"], "status": rawValue("done"), "ended_at": rawValue(stamp()), "message": rawValue("New take imported unselected. Review identity, motion and timing before use.")})
}
