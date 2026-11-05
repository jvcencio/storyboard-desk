package main

import "fmt"

func (s *ProjectStore) PlaceClip(id, sourceShot, sourceAsset, targetShot string, rev int, start, end, measured float64) (ProjectDocument, error) {
	return s.mutate(id, rev, func(p ProjectDocument) error {
		source, err := findShot(p, sourceShot)
		if err != nil {
			return err
		}
		asset, err := findAsset(source, sourceAsset)
		if err != nil {
			return err
		}
		if ref := object(asset, "source_ref"); ref != nil {
			sourceShot = shotText(ref, "shot_id")
			source, err = findShot(p, sourceShot)
			if err != nil {
				return err
			}
			asset, err = findAsset(source, shotText(ref, "asset_id"))
			if err != nil {
				return err
			}
			if object(asset, "source_ref") != nil {
				return fmt.Errorf("restore the original source before placing it again")
			}
		}
		if shotText(asset, "kind") != "clip" || shotText(asset, "binned_at") != "" {
			return fmt.Errorf("choose an active original clip")
		}
		dir, err := projectDirectory(s.root, id)
		if err != nil {
			return err
		}
		defer dir.Close()
		rel := shotText(asset, "path")
		if err = safeProjectPath(dir, rel, false); err != nil {
			return err
		}
		if _, err = dir.Stat(rel); err != nil {
			return fmt.Errorf("source media is unavailable")
		}
		duration := number(asset, "duration_seconds")
		if duration <= 0 {
			duration = measured
		}
		if !finite(start) || !finite(end) || !finite(duration) || start < 0 || end <= start || end > duration || duration > 7200 {
			return fmt.Errorf("placement range must fit inside the source clip")
		}
		err = editShotDocument(p, targetShot, func(target ProjectDocument) error {
			use := ProjectDocument{}
			for _, k := range []string{"path", "original_name", "bytes", "sha256", "provider", "poster_path", "poster_sha256", "source_record", "prompt_snapshot", "keyframe_id"} {
				if v, ok := asset[k]; ok {
					use[k] = v
				}
			}
			aid, err := newStorageID()
			if err != nil {
				return err
			}
			eid, err := newStorageID()
			if err != nil {
				return err
			}
			take := 1
			assets := list(target, "assets")
			for _, a := range assets {
				if shotText(a, "kind") == "clip" {
					take++
				}
			}
			use["id"], use["kind"], use["take"], use["at"] = rawValue(aid), rawValue("clip"), rawValue(take), rawValue(stamp())
			use["duration_seconds"], use["duration_source"], use["in_seconds"], use["out_seconds"] = rawValue(duration), rawOr(asset, "duration_source", "browser-metadata"), rawValue(start), rawValue(end)
			ref := ProjectDocument{"shot_id": rawValue(sourceShot), "asset_id": asset["id"], "sha256": rawOr(asset, "sha256", nil)}
			use["source_ref"] = rawValue(ref)
			event := ProjectDocument{"id": rawValue(eid), "at": rawValue(stamp()), "target_shot": rawValue(targetShot), "previous_selected": rawOr(target, "selected_clip", nil), "asset_id": rawValue(aid), "source_ref": rawValue(ref), "in_seconds": rawValue(start), "out_seconds": rawValue(end)}
			target["assets"], target["selected_clip"] = rawValue(append(assets, use)), rawValue(aid)
			history := append(list(p, "placement_history"), event)
			if len(history) > 100 {
				history = history[len(history)-100:]
			}
			p["placement_history"] = rawValue(history)
			return nil
		})
		if err != nil {
			return err
		}
		return validateCoverageReservations(p)
	})
}
func (s *ProjectStore) UndoPlacement(id string, rev int) (ProjectDocument, error) {
	return s.mutate(id, rev, func(p ProjectDocument) error {
		history := list(p, "placement_history")
		for i := len(history) - 1; i >= 0; i-- {
			event := history[i]
			if shotText(event, "undone_at") != "" {
				continue
			}
			err := editShotDocument(p, shotText(event, "target_shot"), func(target ProjectDocument) error {
				if shotText(target, "selected_clip") != shotText(event, "asset_id") {
					return fmt.Errorf("that shot changed afterward; choose its take manually")
				}
				prior := shotText(event, "previous_selected")
				if prior != "" {
					asset, err := findAsset(target, prior)
					if err != nil || shotText(asset, "binned_at") != "" {
						return fmt.Errorf("restore the earlier take before undoing")
					}
				}
				target["selected_clip"] = rawOr(event, "previous_selected", nil)
				return nil
			})
			if err != nil {
				return err
			}
			event["undone_at"] = rawValue(stamp())
			p["placement_history"] = rawValue(history)
			return validateCoverageReservations(p)
		}
		return fmt.Errorf("there is no library placement to undo")
	})
}
