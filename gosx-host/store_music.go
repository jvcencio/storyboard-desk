package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
)

var musicKinds = []string{"none", "verse", "pre-chorus", "chorus", "bridge", "intro", "outro", "instrumental", "other"}

func soundtrack(p, section ProjectDocument) (ProjectDocument, error) {
	track := object(p, "soundtrack")
	if !boolean(p, "music_video") || track == nil {
		return nil, fmt.Errorf("attach the original song before creating cues or sections")
	}
	if section != nil && shotText(section, "track_sha256") != shotText(track, "sha256") {
		return nil, fmt.Errorf("this section belongs to an older soundtrack")
	}
	return track, nil
}
func (s *ProjectStore) MusicCue(id string, rev int, label string, start float64, end *float64, cueID string, remove bool) (ProjectDocument, error) {
	return s.mutate(id, rev, func(p ProjectDocument) error {
		track, err := soundtrack(p, nil)
		if err != nil {
			return err
		}
		cues := list(p, "music_cues")
		if remove {
			found := false
			kept := []ProjectDocument{}
			for _, c := range cues {
				if shotText(c, "id") == cueID && shotText(c, "track_sha256") == shotText(track, "sha256") {
					found = true
				} else {
					kept = append(kept, c)
				}
			}
			if !found {
				return fmt.Errorf("choose a cue on the current soundtrack")
			}
			cues = kept
		} else {
			label = strings.TrimSpace(label)
			if len([]rune(label)) < 1 || len([]rune(label)) > 100 || !finite(start) || start < 0 || start > number(track, "duration_seconds") {
				return fmt.Errorf("name the cue and place it within the current song")
			}
			uid, err := newStorageID()
			if err != nil {
				return err
			}
			cue := ProjectDocument{"id": rawValue(uid), "label": rawValue(label), "seconds": rawValue(roundSeconds(start)), "track_sha256": track["sha256"], "source": rawValue("user-timed"), "at": rawValue(stamp())}
			if end != nil {
				if !finite(*end) || *end <= start || *end > number(track, "duration_seconds") {
					return fmt.Errorf("cue end must follow its start and fit within the song")
				}
				cue["end_seconds"] = rawValue(roundSeconds(*end))
			}
			cues = append(cues, cue)
			sort.SliceStable(cues, func(i, j int) bool { return number(cues[i], "seconds") < number(cues[j], "seconds") })
		}
		p["music_cues"] = rawValue(cues)
		return nil
	})
}
func coverageKey(e ProjectDocument) string {
	if hash := shotText(e, "source_sha256"); hash != "" {
		return "sha:" + hash
	}
	return "asset:" + shotText(e, "source_shot") + "/" + shotText(e, "source_asset")
}
func coverageID(key string) string {
	sum := sha256.Sum256([]byte(key))
	return "cov-" + hex.EncodeToString(sum[:])[:12]
}
func prepareCoverage(p ProjectDocument) {
	sections := list(p, "music_sections")
	for _, section := range sections {
		if _, ok := section["story_scene"]; !ok {
			section["story_scene"] = rawOr(section, "label", "")
		}
		coverage := list(section, "coverage")
		if coverage == nil {
			coverage = []ProjectDocument{}
		}
		seen := map[string]bool{}
		for _, e := range coverage {
			seen[coverageKey(e)] = true
		}
		for _, e := range list(section, "clips") {
			key := coverageKey(e)
			if seen[key] {
				continue
			}
			seen[key] = true
			row := ProjectDocument{}
			for k, v := range e {
				row[k] = v
			}
			row["id"] = rawValue(coverageID(key))
			row["in_seconds"], row["out_seconds"] = rawValue(0), rawOr(e, "source_duration_seconds", 0)
			row["angle"], row["note"], row["exclusive"], row["include_in_handoff"] = rawValue(""), rawValue(""), rawValue(false), rawValue(true)
			coverage = append(coverage, row)
		}
		section["coverage"] = rawValue(coverage)
	}
	p["music_sections"] = rawValue(sections)
}
func sectionHistory(p ProjectDocument) {
	history := append(list(p, "section_history"), ProjectDocument{"at": rawValue(stamp()), "before": rawOr(p, "music_sections", []any{})})
	if len(history) > 50 {
		history = history[len(history)-50:]
	}
	p["section_history"] = rawValue(history)
}
func findSection(p ProjectDocument, sid string) (ProjectDocument, error) {
	for _, s := range list(p, "music_sections") {
		if shotText(s, "id") == sid {
			return s, nil
		}
	}
	return nil, fmt.Errorf("choose an existing song section")
}
func replaceSection(p, section ProjectDocument) {
	sections := list(p, "music_sections")
	found := false
	for i, s := range sections {
		if shotText(s, "id") == shotText(section, "id") {
			sections[i] = section
			found = true
		}
	}
	if !found {
		sections = append(sections, section)
	}
	p["music_sections"] = rawValue(sections)
}
func sectionRange(a ProjectDocument, start, end, measured float64) (float64, error) {
	duration := number(a, "duration_seconds")
	if duration <= 0 {
		duration = measured
	}
	if !finite(start) || !finite(end) || !finite(duration) || start < 0 || end <= start || end > duration || duration > 7200 {
		return 0, fmt.Errorf("choose an in/out range within the source clip")
	}
	return duration, nil
}
func (s *ProjectStore) sectionSource(p ProjectDocument, sid, aid string) (string, ProjectDocument, string, error) {
	seen := map[string]bool{}
	for count := 0; count < maxShots; count++ {
		key := sid + "/" + aid
		if seen[key] {
			return "", nil, "", fmt.Errorf("cyclic source reference")
		}
		seen[key] = true
		shot, err := findShot(p, sid)
		if err != nil {
			return "", nil, "", err
		}
		a, err := findAsset(shot, aid)
		if err != nil {
			return "", nil, "", err
		}
		if shotText(a, "kind") != "clip" || shotText(a, "binned_at") != "" {
			return "", nil, "", fmt.Errorf("choose an active source clip")
		}
		if ref := object(a, "source_ref"); ref != nil {
			sid, aid = shotText(ref, "shot_id"), shotText(ref, "asset_id")
			continue
		}
		dir, err := projectDirectory(s.root, shotText(p, "id"))
		if err != nil {
			return "", nil, "", err
		}
		rel := shotText(a, "path")
		if err = safeProjectPath(dir, rel, false); err != nil {
			dir.Close()
			return "", nil, "", err
		}
		file, err := dir.Open(rel)
		path := dir.Name() + "/" + rel
		if err != nil {
			dir.Close()
			return "", nil, "", fmt.Errorf("source media is unavailable")
		}
		digest := sha256.New()
		_, err = io.Copy(digest, file)
		file.Close()
		dir.Close()
		if err != nil {
			return "", nil, "", err
		}
		if expected := shotText(a, "sha256"); expected != "" && expected != hex.EncodeToString(digest.Sum(nil)) {
			return "", nil, "", fmt.Errorf("source bytes changed; restore or re-import the clip")
		}
		return sid, a, path, nil
	}
	return "", nil, "", fmt.Errorf("source reference is too deep")
}
func (s *ProjectStore) SaveSection(id string, rev int, raw ProjectDocument) (ProjectDocument, error) {
	return s.mutate(id, rev, func(p ProjectDocument) error {
		prepareCoverage(p)
		track, err := soundtrack(p, nil)
		if err != nil {
			return err
		}
		name := strings.TrimSpace(shotText(raw, "label"))
		for _, key := range []string{"start_seconds", "end_seconds"} {
			if err := validateNumber(raw[key], 0, number(track, "duration_seconds")); err != nil {
				return fmt.Errorf("invalid section %s: %w", key, err)
			}
		}
		if value, ok := raw["note"]; ok {
			if err := validateText(value); err != nil {
				return err
			}
		}
		start, end := number(raw, "start_seconds"), number(raw, "end_seconds")
		kind := shotText(raw, "kind")
		if len([]rune(name)) < 1 || len([]rune(name)) > 100 || !finite(start) || !finite(end) || start < 0 || end <= start || end > number(track, "duration_seconds") || !containsField(musicKinds, kind) {
			return fmt.Errorf("name the section and choose a type and range within the current song")
		}
		sid := shotText(raw, "id")
		var row ProjectDocument
		if sid != "" {
			row, err = findSection(p, sid)
			if err != nil {
				return err
			}
			if _, err = soundtrack(p, row); err != nil {
				return err
			}
		} else {
			sid, err = newStorageID()
			if err != nil {
				return err
			}
			row = ProjectDocument{"id": rawValue(sid), "track_sha256": track["sha256"], "clips": rawValue([]any{}), "coverage": rawValue([]any{}), "created_at": rawValue(stamp())}
		}
		for _, other := range list(p, "music_sections") {
			if shotText(other, "id") != sid && shotText(other, "track_sha256") == shotText(track, "sha256") && strings.EqualFold(shotText(other, "label"), name) {
				return fmt.Errorf("use a distinct section name")
			}
		}
		note := shotText(raw, "note")
		if len([]rune(note)) > 2000 {
			return fmt.Errorf("section note is too long")
		}
		sectionHistory(p)
		row["label"], row["kind"], row["start_seconds"], row["end_seconds"], row["note"], row["updated_at"] = rawValue(name), rawValue(kind), rawValue(start), rawValue(end), rawValue(note), rawValue(stamp())
		for _, key := range []string{"reel_scope", "story_scene"} {
			if v, ok := raw[key]; ok {
				if key == "reel_scope" && !containsField([]string{"coverage", "preferred", "assembly"}, shotText(raw, key)) {
					return fmt.Errorf("choose a valid reel scope")
				}
				if err := validateText(v); err != nil {
					return err
				}
				row[key] = v
			}
		}
		replaceSection(p, row)
		return validateCoverageReservations(p)
	})
}
func (s *ProjectStore) SplitSections(id string, rev int, start, end, span float64, prefix, kind string) (ProjectDocument, error) {
	return s.mutate(id, rev, func(p ProjectDocument) error {
		prepareCoverage(p)
		track, err := soundtrack(p, nil)
		if err != nil {
			return err
		}
		prefix = strings.TrimSpace(prefix)
		if len([]rune(prefix)) < 1 || len([]rune(prefix)) > 95 || !finite(start) || !finite(end) || !finite(span) || start < 0 || end <= start || end > number(track, "duration_seconds") || span <= 0 || math.Ceil((end-start)/span) > 100 || !containsField(musicKinds, kind) {
			return fmt.Errorf("choose a range within the song and a size producing at most 100 portions")
		}
		sections := list(p, "music_sections")
		for _, other := range sections {
			if shotText(other, "track_sha256") == shotText(track, "sha256") && max(start, number(other, "start_seconds")) < min(end, number(other, "end_seconds")) {
				return fmt.Errorf("range overlaps %s", shotText(other, "label"))
			}
		}
		rows := []ProjectDocument{}
		batch, err := newStorageID()
		if err != nil {
			return err
		}
		cursor := start
		for i := 0; cursor < end; i++ {
			stop := min(end, math.RoundToEven((start+float64(i+1)*span)*1e6)/1e6)
			label := fmt.Sprintf("%s %02d", prefix, i+1)
			if stop <= cursor {
				return fmt.Errorf("choose a larger portion size")
			}
			for _, other := range sections {
				if shotText(other, "track_sha256") == shotText(track, "sha256") && strings.EqualFold(shotText(other, "label"), label) {
					return fmt.Errorf("section name already exists")
				}
			}
			sid, err := newStorageID()
			if err != nil {
				return err
			}
			rows = append(rows, ProjectDocument{"id": rawValue(sid), "label": rawValue(label), "kind": rawValue(kind), "start_seconds": rawValue(cursor), "end_seconds": rawValue(stop), "track_sha256": track["sha256"], "clips": rawValue([]any{}), "coverage": rawValue([]any{}), "note": rawValue("Fixed-time working portion. Musical labels can be added later; this boundary is not an inferred verse or beat."), "created_at": rawValue(stamp()), "updated_at": rawValue(stamp()), "split_batch": rawValue(batch), "reel_scope": rawValue("preferred")})
			cursor = stop
		}
		sectionHistory(p)
		sections = append(sections, rows...)
		sort.SliceStable(sections, func(i, j int) bool {
			return number(sections[i], "start_seconds") < number(sections[j], "start_seconds")
		})
		p["music_sections"] = rawValue(sections)
		return nil
	})
}
func (s *ProjectStore) SectionEdit(id, sid string, rev int, raw ProjectDocument, coverage bool) (ProjectDocument, error) {
	return s.mutate(id, rev, func(p ProjectDocument) error {
		prepareCoverage(p)
		section, err := findSection(p, sid)
		if err != nil {
			return err
		}
		operation := shotText(raw, "operation")
		if !coverage || operation == "add" || (operation == "update" && boolean(raw, "exclusive")) {
			if _, err = soundtrack(p, section); err != nil {
				return err
			}
		}
		key := "clips"
		if coverage {
			key = "coverage"
		}
		rows := list(section, key)
		sectionHistory(p)
		if operation == "add" {
			limit := 200
			if coverage {
				limit = 300
			}
			if len(rows) >= limit {
				return fmt.Errorf("too many source ranges in this section")
			}
			ss, a, _, err := s.sectionSource(p, shotText(raw, "source_shot"), shotText(raw, "source_asset"))
			if err != nil {
				return err
			}
			start, end := number(raw, "in_seconds"), number(raw, "out_seconds")
			for _, key := range []string{"in_seconds", "out_seconds"} {
				if err := validateNumber(raw[key], 0, 7200); err != nil {
					return err
				}
			}
			for _, key := range []string{"duration_seconds"} {
				if v, ok := raw[key]; ok {
					if err := validateNumber(v, 0, 7200); err != nil {
						return err
					}
				}
			}
			duration, err := sectionRange(a, start, end, number(raw, "duration_seconds"))
			if err != nil {
				return err
			}
			e := ProjectDocument{"source_shot": rawValue(ss), "source_asset": a["id"], "source_sha256": rawOr(a, "sha256", nil), "source_duration_seconds": rawValue(duration), "in_seconds": rawValue(start), "out_seconds": rawValue(end), "added_at": rawValue(stamp())}
			uid, err := newStorageID()
			if err != nil {
				return err
			}
			e["id"] = rawValue(uid)
			if coverage {
				for _, key := range []string{"angle", "note", "music_tag"} {
					if v, ok := raw[key]; ok {
						if err := validateText(v); err != nil {
							return err
						}
					}
				}
				key := coverageKey(e)
				for _, row := range rows {
					if coverageKey(row) == key {
						return errNoProjectChange
					}
				}
				e["id"] = rawValue(coverageID(key))
				tag := shotText(raw, "music_tag")
				if tag == "" {
					tag = "none"
				}
				if !containsField(musicKinds, tag) || len([]rune(shotText(raw, "angle"))) > 100 || len([]rune(shotText(raw, "note"))) > 2000 {
					return fmt.Errorf("invalid coverage tag or notes")
				}
				e["music_tag"], e["angle"], e["note"], e["exclusive"], e["include_in_handoff"] = rawValue(tag), rawValue(shotText(raw, "angle")), rawValue(shotText(raw, "note")), rawValue(false), rawValue(true)
			}
			index := len(rows)
			position := shotText(raw, "position")
			if !coverage && position != "" && position != "end" {
				if position != "before" && position != "after" {
					return fmt.Errorf("choose before, after or end")
				}
				index = -1
				for i, row := range rows {
					if shotText(row, "id") == shotText(raw, "anchor") {
						index = i
						if position == "after" {
							index++
						}
						break
					}
				}
				if index < 0 {
					return fmt.Errorf("insertion target changed")
				}
			}
			rows = append(rows, nil)
			copy(rows[index+1:], rows[index:])
			rows[index] = e
		} else {
			entry := shotText(raw, "entry")
			if coverage {
				entry = shotText(raw, "coverage")
			}
			index := -1
			for i, row := range rows {
				if shotText(row, "id") == entry {
					index = i
					break
				}
			}
			if index < 0 {
				return fmt.Errorf("choose a source range in this section")
			}
			row := rows[index]
			switch operation {
			case "remove":
				if coverage {
					for _, clip := range list(section, "clips") {
						if coverageKey(clip) == coverageKey(row) {
							return fmt.Errorf("remove this source's assembly ranges first")
						}
					}
				}
				rows = append(rows[:index], rows[index+1:]...)
			case "move":
				if err := validateNumber(raw["delta"], -1, 1); err != nil {
					return err
				}
				n := number(raw, "delta")
				if n != 1 && n != -1 {
					return fmt.Errorf("choose an earlier or later position")
				}
				delta := int(n)
				if (delta != 1 && delta != -1) || index+delta < 0 || index+delta >= len(rows) {
					return fmt.Errorf("choose an earlier or later position")
				}
				rows[index], rows[index+delta] = rows[index+delta], rows[index]
			case "update":
				if !coverage {
					if _, _, _, err = s.sectionSource(p, shotText(row, "source_shot"), shotText(row, "source_asset")); err != nil {
						return err
					}
				}
				for _, k := range []string{"angle", "note", "music_tag"} {
					if v, ok := raw[k]; ok {
						if err := validateText(v); err != nil {
							return err
						}
						row[k] = v
					}
				}
				if len([]rune(shotText(row, "angle"))) > 100 || len([]rune(shotText(row, "note"))) > 2000 {
					return fmt.Errorf("coverage notes are too long")
				}
				if tag := shotText(row, "music_tag"); tag != "" && !containsField(musicKinds, tag) {
					return fmt.Errorf("invalid musical tag")
				}
				for _, k := range []string{"exclusive", "include_in_handoff"} {
					if v, ok := raw[k]; ok {
						var b bool
						if nullJSON(v) || json.Unmarshal(v, &b) != nil {
							return fmt.Errorf("use a checkbox for coverage options")
						}
						row[k] = v
					}
				}
				_, hasStart := raw["in_seconds"]
				_, hasEnd := raw["out_seconds"]
				if !coverage || hasStart || hasEnd {
					start, end := number(row, "in_seconds"), number(row, "out_seconds")
					if v, ok := raw["in_seconds"]; ok {
						if err := validateNumber(v, 0, 7200); err != nil {
							return err
						}
						start = number(raw, "in_seconds")
					}
					if v, ok := raw["out_seconds"]; ok {
						if err := validateNumber(v, 0, 7200); err != nil {
							return err
						}
						end = number(raw, "out_seconds")
					}
					_, a, _, err := s.sectionSource(p, shotText(row, "source_shot"), shotText(row, "source_asset"))
					if err != nil {
						return err
					}
					if _, err = sectionRange(a, start, end, number(row, "source_duration_seconds")); err != nil {
						return err
					}
					row["in_seconds"], row["out_seconds"] = rawValue(start), rawValue(end)
				}
			default:
				return fmt.Errorf("unknown section operation")
			}
		}
		section[key], section["updated_at"] = rawValue(rows), rawValue(stamp())
		replaceSection(p, section)
		prepareCoverage(p)
		return validateCoverageReservations(p)
	})
}
func (s *ProjectStore) UndoSection(id string, rev int) (ProjectDocument, error) {
	return s.mutate(id, rev, func(p ProjectDocument) error {
		history := list(p, "section_history")
		if len(history) == 0 {
			return fmt.Errorf("no section edit to undo")
		}
		p["music_sections"] = history[len(history)-1]["before"]
		p["section_history"] = rawValue(history[:len(history)-1])
		prepareCoverage(p)
		return validateCoverageReservations(p)
	})
}
