package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf16"
)

// Python's saved approval hashes use json.dumps(sort_keys=True), including its
// spaces and ASCII escaping. Keep this format while existing receipts are used.
func pythonJSON(v any) string {
	data, _ := json.Marshal(v)
	var value any
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	_ = d.Decode(&value)
	var out strings.Builder
	var encode func(any)
	quote := func(s string) {
		out.WriteByte('"')
		for _, r := range s {
			switch r {
			case '"', '\\':
				out.WriteByte('\\')
				out.WriteRune(r)
			case '\b':
				out.WriteString(`\b`)
			case '\f':
				out.WriteString(`\f`)
			case '\n':
				out.WriteString(`\n`)
			case '\r':
				out.WriteString(`\r`)
			case '\t':
				out.WriteString(`\t`)
			default:
				if r < 32 || r > 126 {
					if r > 0xffff {
						a, b := utf16.EncodeRune(r)
						fmt.Fprintf(&out, `\u%04x\u%04x`, a, b)
					} else {
						fmt.Fprintf(&out, `\u%04x`, r)
					}
				} else {
					out.WriteRune(r)
				}
			}
		}
		out.WriteByte('"')
	}
	encode = func(v any) {
		switch x := v.(type) {
		case nil:
			out.WriteString("null")
		case bool:
			fmt.Fprint(&out, x)
		case string:
			quote(x)
		case json.Number:
			out.WriteString(string(x))
		case []any:
			out.WriteByte('[')
			for i, e := range x {
				if i > 0 {
					out.WriteString(", ")
				}
				encode(e)
			}
			out.WriteByte(']')
		case map[string]any:
			keys := []string{}
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			out.WriteByte('{')
			for i, k := range keys {
				if i > 0 {
					out.WriteString(", ")
				}
				quote(k)
				out.WriteString(": ")
				encode(x[k])
			}
			out.WriteByte('}')
		}
	}
	encode(value)
	return out.String()
}
func pythonDigest(v any) string {
	sum := sha256.Sum256([]byte(pythonJSON(v)))
	return hex.EncodeToString(sum[:])
}
func rawOr(p ProjectDocument, k string, fallback any) json.RawMessage {
	if v, ok := p[k]; ok {
		return v
	}
	return rawValue(fallback)
}
func imageFingerprint(p, s ProjectDocument) string {
	v := ProjectDocument{"composition": rawOr(s, "composition", ""), "shot_continuity": rawOr(s, "continuity", "")}
	for _, k := range []string{"look", "continuity", "aspect"} {
		v[k] = rawOr(p, k, "")
	}
	v["keyframe"], v["sketch"] = rawOr(s, "selected_keyframe", nil), rawOr(s, "selected_sketch", nil)
	for _, k := range []string{"blocking", "start_state"} {
		if shotText(s, k) != "" {
			v[k] = s[k]
		}
	}
	if refs := list(p, "visual_references"); len(refs) > 0 {
		hashes := []json.RawMessage{}
		for _, r := range refs {
			hashes = append(hashes, r["sha256"])
		}
		v["visual_references"] = rawValue(hashes)
	}
	return pythonDigest(v)
}
func imageApproved(p, s ProjectDocument) bool {
	return shotText(s, "selected_keyframe") != "" && shotText(object(s, "approval"), "fingerprint") == imageFingerprint(p, s)
}
func directionImage(s ProjectDocument) ProjectDocument {
	assets := list(s, "assets")
	for _, key := range []string{"selected_keyframe", "selected_sketch"} {
		for _, a := range assets {
			if shotText(a, "id") == shotText(s, key) && shotText(a, "binned_at") == "" {
				return a
			}
		}
	}
	for i := len(assets) - 1; i >= 0; i-- {
		a := assets[i]
		if shotText(a, "kind") == "sketch" && shotText(a, "binned_at") == "" {
			return a
		}
	}
	return nil
}
func directionBasis(p, s ProjectDocument) string {
	core := ProjectDocument{}
	for _, k := range append(append([]string{}, shotFields[:9]...), "duration") {
		core[k] = rawOr(s, k, "")
	}
	v := ProjectDocument{"shot": rawValue(core), "image": rawValue(nil), "references": rawOr(p, "references", []any{})}
	for _, k := range []string{"brief", "look", "continuity", "aspect"} {
		v[k] = rawOr(p, k, "")
	}
	if a := directionImage(s); a != nil {
		v["image"] = a["sha256"]
	}
	seq := []ProjectDocument{}
	for _, shot := range list(p, "shots") {
		row := ProjectDocument{}
		for _, k := range []string{"id", "duration", "action", "continuity", "transition"} {
			row[k] = rawOr(shot, k, "")
		}
		seq = append(seq, row)
	}
	v["sequence"] = rawValue(seq)
	details := ProjectDocument{}
	for _, k := range shotFields[9:] {
		if k != "local_prompt" && k != "local_negative_prompt" && shotText(s, k) != "" {
			details[k] = s[k]
		}
	}
	if len(details) > 0 {
		v["shot_details"] = rawValue(details)
	}
	if shotText(p, "episode_notes") != "" {
		v["episode_notes"] = p["episode_notes"]
	}
	if refs := list(p, "visual_references"); len(refs) > 0 {
		rows := []ProjectDocument{}
		for _, r := range refs {
			row := ProjectDocument{}
			for _, k := range []string{"sha256", "title", "notes"} {
				row[k] = rawOr(r, k, "")
			}
			rows = append(rows, row)
		}
		v["visual_references"] = rawValue(rows)
	}
	if boolean(p, "music_video") {
		v["music"] = rawValue(ProjectDocument{"lyrics": rawOr(p, "lyrics", ""), "soundtrack": rawOr(object(p, "soundtrack"), "sha256", nil)})
	}
	return pythonDigest(v)
}
func optimizedDirectionReady(p, s ProjectDocument) bool {
	r := object(s, "direction_review")
	a := directionImage(s)
	return boolean(r, "image_inspected") && a != nil && imageApproved(p, s) && shotText(r, "verdict") == "ready_for_review" && shotText(r, "image_id") == shotText(s, "selected_keyframe") && shotText(r, "current_basis") == directionBasis(p, s)
}
func musicTiming(p ProjectDocument) ProjectDocument {
	seconds := number(object(p, "soundtrack"), "duration_seconds")
	elapsed, covered := 0.0, 0.0
	unmeasured := 0
	slots := []ProjectDocument{}
	for _, s := range list(p, "shots") {
		a := selectedAsset(s, "clip")
		available := 0.0
		if a != nil {
			if _, ok := a["out_seconds"]; !ok {
				if _, ok = a["duration_seconds"]; !ok {
					unmeasured++
				}
			}
			available = max(0, rangeEnd(a)-number(a, "in_seconds"))
		}
		used := min(max(0, min(number(s, "duration"), seconds-elapsed)), available)
		slots = append(slots, ProjectDocument{"shot_id": s["id"], "start_seconds": rawValue(roundSeconds(elapsed)), "target_seconds": s["duration"], "available_seconds": rawValue(roundSeconds(available)), "covered_seconds": rawValue(roundSeconds(used))})
		elapsed += number(s, "duration")
		covered += used
	}
	return ProjectDocument{"track_seconds": rawValue(seconds), "planned_seconds": rawValue(roundSeconds(elapsed)), "covered_seconds": rawValue(roundSeconds(covered)), "gap_seconds": rawValue(roundSeconds(max(0, seconds-covered))), "unallocated_seconds": rawValue(roundSeconds(max(0, seconds-elapsed))), "overrun_seconds": rawValue(roundSeconds(max(0, elapsed-seconds))), "unmeasured_clips": rawValue(unmeasured), "slots": rawValue(slots)}
}
func selectedAsset(s ProjectDocument, kinds ...string) ProjectDocument {
	assets := list(s, "assets")
	for _, kind := range kinds {
		id := shotText(s, "selected_"+kind)
		for _, a := range assets {
			if shotText(a, "id") == id && shotText(a, "binned_at") == "" {
				return a
			}
		}
	}
	return nil
}
func rangeEnd(a ProjectDocument) float64 {
	if _, ok := a["out_seconds"]; ok {
		return number(a, "out_seconds")
	}
	return number(a, "duration_seconds")
}
