package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

func decodeDocument(raw string) (ProjectDocument, error) {
	var p ProjectDocument
	if len(raw) > 1<<20 || json.Unmarshal([]byte(raw), &p) != nil || p == nil {
		return nil, fmt.Errorf("supply a JSON object smaller than 1 MiB")
	}
	return p, nil
}

func validateReferences(raw json.RawMessage) error {
	var refs []ProjectDocument
	if json.Unmarshal(raw, &refs) != nil || refs == nil || len(refs) > 40 {
		return fmt.Errorf("use at most 40 reference notes")
	}
	for _, ref := range refs {
		u := shotText(ref, "url")
		if !(strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://")) || utf8.RuneCountInString(u) > 2000 {
			return fmt.Errorf("reference URLs must use HTTP or HTTPS and fit within 2000 characters")
		}
		for _, field := range []string{"title", "notes"} {
			if v, ok := ref[field]; ok {
				if err := validateText(v); err != nil {
					return err
				}
			}
		}
		if utf8.RuneCountInString(shotText(ref, "title")) > 500 {
			return fmt.Errorf("reference title is too long")
		}
	}
	return nil
}

func projectPlan(p ProjectDocument) ProjectDocument {
	out := ProjectDocument{}
	for _, k := range append(append([]string{}, projectTextFields...), "aspect", "runtime_limit_seconds", "music_video", "references") {
		if v, ok := p[k]; ok {
			out[k] = v
		}
	}
	shots, _ := documentShots(p)
	plan := []ProjectDocument{}
	for _, s := range shots {
		row := ProjectDocument{}
		for _, k := range append(append([]string{}, shotFields...), "id", "duration") {
			if v, ok := s[k]; ok {
				row[k] = v
			}
		}
		plan = append(plan, row)
	}
	out["shots"] = rawValue(plan)
	return out
}

// ApplyPlan refines existing shot identities. Media and opaque fields are never
// reconstructed from a planning tool's response.
func (s *ProjectStore) ApplyPlan(id string, rev int, plan ProjectDocument) (ProjectDocument, error) {
	return s.mutate(id, rev, func(p ProjectDocument) error {
		source := ProjectDocument{}
		for k, v := range p {
			source[k] = v
		}
		reviews := map[string]ProjectDocument{}
		for _, k := range append(append([]string{}, projectTextFields...), "aspect", "runtime_limit_seconds", "references") {
			if v, ok := plan[k]; ok {
				if k == "references" {
					if err := validateReferences(v); err != nil {
						return err
					}
				}
				p[k] = v
			}
		}
		shots, err := documentShots(p)
		if err != nil {
			return err
		}
		byID := map[string]ProjectDocument{}
		for _, s := range shots {
			byID[shotText(s, "id")] = s
		}
		var patches []ProjectDocument
		if v, ok := plan["shots"]; ok {
			if json.Unmarshal(v, &patches) != nil || patches == nil {
				return fmt.Errorf("plan shots must be an array")
			}
		}
		seen := map[string]bool{}
		for _, patch := range patches {
			sid := shotText(patch, "id")
			shot, ok := byID[sid]
			if !ok || seen[sid] {
				return fmt.Errorf("plan imports refine each existing shot ID at most once; insert new shots on the board")
			}
			seen[sid] = true
			if _, ok := patch["grok_prompt"]; ok {
				if shotText(patch, "basis") != directionBasis(source, shot) {
					return fmt.Errorf("direction was prepared for an older image or brief; prepare a fresh handoff")
				}
				verdict := shotText(patch, "verdict")
				prompt := strings.TrimSpace(shotText(patch, "grok_prompt"))
				if !containsField([]string{"ready_for_review", "needs_image_fix"}, verdict) || len([]rune(prompt)) < 1 || len([]rune(prompt)) > 6000 {
					return fmt.Errorf("supply a valid direction verdict and a nonempty prompt under 6000 characters")
				}
				inspected := boolean(patch, "image_inspected") && directionImage(shot) != nil
				review := object(shot, "direction_review")
				if review == nil {
					review = ProjectDocument{}
				}
				review["source"], review["at"], review["input_basis"], review["image_inspected"], review["returned_verdict"], review["grok_prompt"] = rawValue("manual-cloud-handoff"), rawValue(stamp()), patch["basis"], rawValue(inspected), rawValue(verdict), rawValue(prompt)
				review["image_id"] = rawValue(nil)
				if a := directionImage(shot); a != nil {
					review["image_id"] = a["id"]
				}
				if !inspected {
					verdict = "needs_image_fix"
				}
				review["verdict"] = rawValue(verdict)
				for _, k := range []string{"image_fixes", "reference_application"} {
					v := rawOr(patch, k, "")
					if err := validateText(v); err != nil {
						return err
					}
					review[k] = v
				}
				reviews[sid] = review
			}
			for _, k := range append(append([]string{}, shotFields...), "duration") {
				if v, ok := patch[k]; ok {
					if !json.Valid(v) {
						return fmt.Errorf("invalid shot field")
					}
					shot[k] = v
				}
			}
		}
		stripDerivedShots(shots)
		p["shots"] = rawValue(shots)
		for _, shot := range shots {
			if review, ok := reviews[shotText(shot, "id")]; ok {
				review["current_basis"] = rawValue(directionBasis(p, shot))
				shot["direction_review"] = rawValue(review)
			}
		}
		p["shots"] = rawValue(shots)
		if err := validateDocument(p, id); err != nil {
			return err
		}
		return validateCoverageReservations(p)
	})
}
