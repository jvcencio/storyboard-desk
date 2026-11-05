package main

import (
	"crypto/sha256"
	"encoding/hex"
)

// The version covers the brief shown to the editor. Media, order and unknown
// fields can change independently and remain owned by the project store.
func shotBriefVersion(shot ProjectDocument) string {
	values := make([]string, 0, len(shotFields)+1)
	values = append(values, shotText(shot, "id"))
	for _, field := range shotFields {
		values = append(values, shotText(shot, field))
	}
	digest := sha256.Sum256(rawValue(struct {
		Text     []string
		Duration float64
	}{values, number(shot, "duration")}))
	return hex.EncodeToString(digest[:])
}

func (s *ProjectStore) updateSharedShot(id, shotID string, revision int, base string, patch ProjectDocument) (ProjectDocument, error) {
	return s.mutateChecked(id, func(project ProjectDocument) error {
		shot, err := findShot(project, shotID)
		if err != nil || revision < 0 || revision > documentRevision(project) || shotBriefVersion(shot) != base {
			return ErrRevision
		}
		return nil
	}, func(project ProjectDocument) error {
		if err := editShot(shotID, patch)(project); err != nil {
			return err
		}
		shot, err := findShot(project, shotID)
		if err != nil {
			return err
		}
		if shotBriefVersion(shot) == base {
			return errNoProjectChange
		}
		return nil
	})
}
