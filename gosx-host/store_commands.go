package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

func nullJSON(raw json.RawMessage) bool { return bytes.Equal(bytes.TrimSpace(raw), []byte("null")) }

const maxShots = 600

var shotFields = strings.Fields("title purpose composition action camera sound continuity feedback transition scene story_status dialogue narration audience jorge anomaly start_state end_state screen_direction assets_needed reference_notes blocking local_prompt local_negative_prompt")
var projectTextFields = strings.Fields("title brief look continuity episode_notes lyrics")

func containsField(fields []string, name string) bool {
	for _, field := range fields {
		if field == name {
			return true
		}
	}
	return false
}

func validateText(raw json.RawMessage) error {
	if bytes.Equal(raw, []byte(`""`)) {
		return nil
	}
	var value string
	if nullJSON(raw) || json.Unmarshal(raw, &value) != nil || !utf8.ValidString(value) || utf8.RuneCountInString(value) > 12000 {
		return fmt.Errorf("text is missing or too long")
	}
	return nil
}

func validateNumber(raw json.RawMessage, low, high float64) error {
	var n float64
	if nullJSON(raw) || json.Unmarshal(raw, &n) != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < low || n > high {
		return fmt.Errorf("number must be between %g and %g", low, high)
	}
	return nil
}

func validateAspect(raw json.RawMessage) error {
	var value string
	if json.Unmarshal(raw, &value) != nil || (value != "9:16" && value != "16:9" && value != "1:1") {
		return fmt.Errorf("choose 9:16, 16:9 or 1:1")
	}
	return nil
}

func documentShots(p ProjectDocument) ([]ProjectDocument, error) {
	shots, err := decodeProjectJSON[[]ProjectDocument](p["shots"])
	if err != nil || shots == nil {
		return nil, fmt.Errorf("invalid project shots")
	}
	return shots, nil
}

func validateDocument(p ProjectDocument, id string) error {
	_, err := validateProject(p, id)
	return err
}

func validateProject(p ProjectDocument, id string) (projectSnapshot, error) {
	if !projectIDPattern.MatchString(id) || shotText(p, "id") != id {
		return projectSnapshot{}, fmt.Errorf("project ID does not match directory")
	}
	var revision int
	if len(p["revision"]) == 0 || nullJSON(p["revision"]) || json.Unmarshal(p["revision"], &revision) != nil || revision < 0 {
		return projectSnapshot{}, fmt.Errorf("invalid project revision")
	}
	for _, name := range projectTextFields {
		if raw, ok := p[name]; ok {
			if err := validateText(raw); err != nil {
				return projectSnapshot{}, fmt.Errorf("%s: %w", name, err)
			}
		}
	}
	if raw, ok := p["aspect"]; ok {
		if err := validateAspect(raw); err != nil {
			return projectSnapshot{}, err
		}
	}
	if raw, ok := p["runtime_limit_seconds"]; ok {
		if err := validateNumber(raw, 0, 3600); err != nil {
			return projectSnapshot{}, err
		}
	}
	if raw, ok := p["music_video"]; ok {
		var value bool
		if nullJSON(raw) || json.Unmarshal(raw, &value) != nil {
			return projectSnapshot{}, fmt.Errorf("music video must be on or off")
		}
	}
	shots, err := documentShots(p)
	if err != nil {
		return projectSnapshot{}, err
	}
	if len(shots) < 1 || len(shots) > maxShots {
		return projectSnapshot{}, fmt.Errorf("a storyboard needs 1–%d shots", maxShots)
	}
	ids := map[string]bool{}
	for _, shot := range shots {
		sid := shotText(shot, "id")
		if !projectIDPattern.MatchString(sid) || ids[sid] {
			return projectSnapshot{}, fmt.Errorf("shot IDs must be valid and unique")
		}
		ids[sid] = true
		for _, name := range shotFields {
			if raw, ok := shot[name]; ok {
				if err := validateText(raw); err != nil {
					return projectSnapshot{}, fmt.Errorf("shot %s %s: %w", sid, name, err)
				}
			}
		}
		if raw, ok := shot["duration"]; ok {
			if err := validateNumber(raw, 1, 60); err != nil {
				return projectSnapshot{}, err
			}
		}
		if raw, ok := shot["assets"]; ok {
			// documentShots already checked the JSON syntax of every nested
			// value. Only the array type needs checking here.
			raw = bytes.TrimSpace(raw)
			if len(raw) == 0 || raw[0] != '[' {
				return projectSnapshot{}, fmt.Errorf("invalid shot assets")
			}
		}
	}
	return projectSnapshot{document: p, shots: shots}, nil
}

func newDraftShot(title, scene string, music bool) (ProjectDocument, error) {
	id, err := newStorageID()
	if err != nil {
		return nil, err
	}
	shot := ProjectDocument{}
	for _, field := range shotFields {
		shot[field] = rawValue("")
	}
	shot["id"], shot["title"], shot["scene"] = rawValue(id), rawValue(title), rawValue(scene)
	duration := 5
	if music {
		duration = 8
	}
	shot["duration"] = rawValue(duration)
	shot["assets"] = rawValue([]any{})
	for _, field := range []string{"selected_sketch", "selected_keyframe", "selected_clip", "approval"} {
		shot[field] = rawValue(nil)
	}
	return shot, nil
}

// Create publishes the complete initial document by renaming a private staging
// directory. A crash before publication cannot expose a half-created project.
func (s *ProjectStore) Create(title, brief, aspect string) (ProjectDocument, error) {
	return s.CreateTemplate(ProjectDocument{"title": rawValue(title), "brief": rawValue(brief), "aspect": rawValue(aspect),
		"shots": rawValue([]ProjectDocument{{"title": rawValue("The opening"), "purpose": rawValue("Establish the story.")}})})
}

func (s *ProjectStore) CreateTemplate(raw ProjectDocument) (ProjectDocument, error) {
	s.workflowMu.RLock()
	defer s.workflowMu.RUnlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, fmt.Errorf("supply a project object")
	}
	id, err := newStorageID()
	if err != nil {
		return nil, err
	}
	p := ProjectDocument{}
	for _, field := range projectTextFields {
		p[field] = rawValue("")
	}
	p["aspect"], p["music_video"], p["runtime_limit_seconds"] = rawValue("9:16"), rawValue(false), rawValue(0)
	p["references"], p["visual_references"], p["soundtrack_history"] = rawValue([]any{}), rawValue([]any{}), rawValue([]any{})
	p["soundtrack"] = rawValue(nil)
	for name, value := range raw {
		if containsField(projectTextFields, name) || containsField([]string{"aspect", "music_video", "runtime_limit_seconds", "references"}, name) {
			p[name] = value
		}
	}
	if validateText(p["title"]) != nil {
		return nil, fmt.Errorf("invalid title")
	}
	title := strings.TrimSpace(shotText(p, "title"))
	if title == "" {
		title = "Untitled storyboard"
	}
	p["title"] = rawValue(title)
	inputs, err := documentShots(raw)
	if err != nil || len(inputs) < 1 || len(inputs) > maxShots {
		return nil, fmt.Errorf("a storyboard needs 1–%d shots", maxShots)
	}
	shots := []ProjectDocument{}
	for _, input := range inputs {
		shot, err := newDraftShot("", "", false)
		if err != nil {
			return nil, err
		}
		for name, value := range input {
			if containsField(shotFields, name) || name == "duration" {
				if !json.Valid(value) {
					return nil, fmt.Errorf("invalid shot field")
				}
				shot[name] = value
			}
		}
		shots = append(shots, shot)
	}
	p["id"], p["revision"], p["shots"] = rawValue(id), rawValue(1), rawValue(shots)
	p["created_at"] = rawValue(time.Now().UTC().Format(time.RFC3339Nano))
	p["updated_at"] = p["created_at"]
	if err := validateReferences(p["references"]); err != nil {
		return nil, err
	}
	if err := validateDocument(p, id); err != nil {
		return nil, err
	}
	return s.publish(p)
}

func (s *ProjectStore) publish(p ProjectDocument) (ProjectDocument, error) {
	id := shotText(p, "id")
	var err error
	stage := ".new-" + id
	if _, err = s.root.Lstat(id); !os.IsNotExist(err) {
		return nil, fmt.Errorf("project identifier collision")
	}
	if err = s.root.Mkdir(stage, 0700); err != nil {
		return nil, err
	}
	defer func() { _ = s.root.Remove(stage + "/project.json"); _ = s.root.Remove(stage) }()
	dir, err := s.root.OpenRoot(stage)
	if err != nil {
		return nil, err
	}
	err = writeProjectDocument(dir, p)
	closeErr := dir.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if err = s.check(); err != nil {
		return nil, err
	}
	if err = replaceStoredFile(s.root, stage, id); err != nil {
		return nil, err
	}
	if err = syncStorageDirectory(s.root); err != nil {
		return nil, fmt.Errorf("project created but directory sync failed; reload before retrying: %w", err)
	}
	return p, nil
}

func stripDerivedShots(shots []ProjectDocument) {
	for _, shot := range shots {
		delete(shot, "animation_ready")
	}
}

func (s *ProjectStore) UpdateProject(id string, revision int, patch ProjectDocument) (ProjectDocument, error) {
	return s.mutate(id, revision, func(p ProjectDocument) error {
		for name, value := range patch {
			if !containsField(projectTextFields, name) && name != "aspect" && name != "music_video" && name != "runtime_limit_seconds" && name != "references" {
				return fmt.Errorf("unsupported project field %q", name)
			}
			if name == "references" {
				if err := validateReferences(value); err != nil {
					return err
				}
			}
			p[name] = value
		}
		shots, err := documentShots(p)
		if err != nil {
			return err
		}
		stripDerivedShots(shots)
		p["shots"] = rawValue(shots)
		return nil
	})
}

func (s *ProjectStore) UpdateShot(id, shotID string, revision int, patch ProjectDocument) (ProjectDocument, error) {
	return s.mutate(id, revision, editShot(shotID, patch))
}

func editShot(shotID string, patch ProjectDocument) func(ProjectDocument) error {
	return func(p ProjectDocument) error {
		shots, err := documentShots(p)
		if err != nil {
			return err
		}
		found := false
		for _, shot := range shots {
			if shotText(shot, "id") != shotID {
				continue
			}
			found = true
			for name, value := range patch {
				if !containsField(shotFields, name) && name != "duration" {
					return fmt.Errorf("unsupported shot field %q", name)
				}
				if name == "duration" {
					if err := validateNumber(value, 1, 60); err != nil {
						return err
					}
				} else if err := validateText(value); err != nil {
					return err
				}
				shot[name] = value
			}
		}
		if !found {
			return ErrNotFound
		}
		stripDerivedShots(shots)
		p["shots"] = rawValue(shots)
		// Scene changes can conflict with a source reserved to a song section.
		if _, ok := patch["scene"]; ok {
			return validateCoverageReservations(p)
		}
		return nil
	}
}

func (s *ProjectStore) Insert(id, anchor, position string, revision int) (ProjectDocument, error) {
	if position != "before" && position != "after" {
		return nil, fmt.Errorf("choose before or after")
	}
	return s.mutate(id, revision, func(p ProjectDocument) error {
		shots, err := documentShots(p)
		if err != nil {
			return err
		}
		if len(shots) >= maxShots {
			return fmt.Errorf("a storyboard supports at most %d shots", maxShots)
		}
		for i, shot := range shots {
			if shotText(shot, "id") != anchor {
				continue
			}
			var music bool
			_ = json.Unmarshal(p["music_video"], &music)
			draft, err := newDraftShot("A new beat", shotText(shot, "scene"), music)
			if err != nil {
				return err
			}
			draft["story_status"] = rawValue("proposed")
			if position == "after" {
				i++
			}
			shots = append(shots, nil)
			copy(shots[i+1:], shots[i:])
			shots[i] = draft
			stripDerivedShots(shots)
			p["shots"] = rawValue(shots)
			return nil
		}
		return ErrNotFound
	})
}

func (s *ProjectStore) Reorder(id string, revision int, order []string) (ProjectDocument, error) {
	return s.mutate(id, revision, func(p ProjectDocument) error {
		shots, err := documentShots(p)
		if err != nil {
			return err
		}
		if len(order) != len(shots) {
			return fmt.Errorf("reordering must retain every shot")
		}
		byID := map[string]ProjectDocument{}
		for _, shot := range shots {
			byID[shotText(shot, "id")] = shot
		}
		ordered := make([]ProjectDocument, 0, len(order))
		for _, id := range order {
			shot, ok := byID[id]
			if !ok {
				return fmt.Errorf("reordering must contain each shot exactly once")
			}
			ordered = append(ordered, shot)
			delete(byID, id)
		}
		stripDerivedShots(ordered)
		p["shots"] = rawValue(ordered)
		return nil
	})
}

// This narrow validation protects existing exclusive coverage when editing a
// shot's scene. Coverage itself remains opaque and is never normalized or saved.
func validateCoverageReservations(p ProjectDocument) error {
	type entry struct {
		SourceSHA string `json:"source_sha256"`
		Shot      string `json:"source_shot"`
		Asset     string `json:"source_asset"`
		Exclusive bool   `json:"exclusive"`
	}
	type section struct {
		ID       string  `json:"id"`
		Label    string  `json:"label"`
		Scene    *string `json:"story_scene"`
		Coverage []entry `json:"coverage"`
		Clips    []entry `json:"clips"`
	}
	var sections []section
	if raw, ok := p["music_sections"]; ok {
		if err := json.Unmarshal(raw, &sections); err != nil {
			return fmt.Errorf("invalid music sections: %w", err)
		}
	}
	key := func(e entry) string {
		if e.SourceSHA != "" {
			return "sha:" + e.SourceSHA
		}
		return "asset:" + e.Shot + "/" + e.Asset
	}
	owners := map[string]string{}
	shots, _ := documentShots(p)
	for _, owner := range sections {
		for _, row := range owner.Coverage {
			if !row.Exclusive {
				continue
			}
			k := key(row)
			if other, ok := owners[k]; ok && other != owner.ID {
				return fmt.Errorf("source is reserved by multiple sections")
			}
			owners[k] = owner.ID
			for _, other := range sections {
				if other.ID == owner.ID {
					continue
				}
				for _, e := range append(other.Coverage, other.Clips...) {
					if key(e) == k {
						return fmt.Errorf("source is reserved for %s", owner.Label)
					}
				}
			}
			scene := owner.Label
			if owner.Scene != nil {
				scene = *owner.Scene
			}
			for _, shot := range shots {
				chosen := shotText(shot, "selected_clip")
				if chosen == "" {
					continue
				}
				var assets []ProjectDocument
				if err := json.Unmarshal(shot["assets"], &assets); err != nil {
					return err
				}
				for _, asset := range assets {
					if shotText(asset, "id") != chosen {
						continue
					}
					var ref ProjectDocument
					_ = json.Unmarshal(asset["source_ref"], &ref)
					digest := shotText(asset, "sha256")
					if digest == "" {
						digest = shotText(ref, "sha256")
					}
					assetKey := "asset:" + shotText(shot, "id") + "/" + chosen
					if len(ref) > 0 {
						assetKey = "asset:" + shotText(ref, "shot_id") + "/" + shotText(ref, "asset_id")
					}
					if digest != "" {
						assetKey = "sha:" + digest
					}
					if assetKey == k && !strings.EqualFold(shotText(shot, "scene"), scene) {
						return fmt.Errorf("source is reserved for %s; retain its story scene", owner.Label)
					}
				}
			}
		}
	}
	return nil
}
