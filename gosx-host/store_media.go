package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxUpload int64 = 256 << 20

func stamp() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func findShot(p ProjectDocument, sid string) (ProjectDocument, error) {
	for _, s := range list(p, "shots") {
		if shotText(s, "id") == sid {
			return s, nil
		}
	}
	return nil, ErrNotFound
}
func findAsset(s ProjectDocument, aid string) (ProjectDocument, error) {
	for _, a := range list(s, "assets") {
		if shotText(a, "id") == aid {
			return a, nil
		}
	}
	return nil, fmt.Errorf("choose an existing take")
}
func editShotDocument(p ProjectDocument, sid string, edit func(ProjectDocument) error) error {
	shots, err := documentShots(p)
	if err != nil {
		return err
	}
	found := false
	for _, s := range shots {
		if shotText(s, "id") == sid {
			found = true
			if err := edit(s); err != nil {
				return err
			}
		}
	}
	if !found {
		return ErrNotFound
	}
	stripDerivedShots(shots)
	p["shots"] = rawValue(shots)
	return nil
}
func editAssetDocument(p ProjectDocument, sid, aid string, edit func(ProjectDocument, ProjectDocument) error) error {
	return editShotDocument(p, sid, func(s ProjectDocument) error {
		assets := list(s, "assets")
		found := false
		for _, a := range assets {
			if shotText(a, "id") == aid {
				found = true
				if err := edit(s, a); err != nil {
					return err
				}
			}
		}
		if !found {
			return fmt.Errorf("choose an existing take")
		}
		s["assets"] = rawValue(assets)
		return nil
	})
}
func safeProjectPath(dir *os.Root, rel string, createParents bool) error {
	if !filepath.IsLocal(rel) || strings.ContainsAny(rel, "\\:\x00") || !(strings.HasPrefix(rel, "media/") || strings.HasPrefix(rel, "exports/") || strings.HasPrefix(rel, "cloud/") || strings.HasPrefix(rel, "jobs/")) {
		return fmt.Errorf("invalid project file path")
	}
	parts := strings.Split(rel, "/")
	current := ""
	for i, part := range parts {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("invalid project file path")
		}
		if current != "" {
			current += "/"
		}
		current += part
		info, err := dir.Lstat(current)
		if os.IsNotExist(err) {
			if createParents && i < len(parts)-1 {
				if err = dir.Mkdir(current, 0700); err != nil {
					return err
				}
				parent, e := dir.OpenRoot(filepath.Dir(current))
				if e != nil {
					return e
				}
				e = syncStorageDirectory(parent)
				parent.Close()
				if e != nil {
					return e
				}
				continue
			}
			if i == len(parts)-1 {
				return nil
			}
			return err
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink project paths are not supported")
		}
		if i < len(parts)-1 && !info.IsDir() {
			return fmt.Errorf("media parent must be a real directory")
		}
		if i == len(parts)-1 && !info.Mode().IsRegular() {
			return fmt.Errorf("media file must be regular")
		}
	}
	return nil
}
func assetExtension(kind, filename string) (string, error) {
	ext := strings.ToLower(filepath.Ext(strings.ReplaceAll(filename, "\\", "/")))
	allowed := []string{".png", ".jpg", ".jpeg", ".webp"}
	if kind == "clip" {
		allowed = []string{".mp4", ".mov", ".webm"}
	} else if kind == "soundtrack" {
		allowed = []string{".wav"}
	} else if kind != "sketch" && kind != "keyframe" && kind != "reference" {
		return "", fmt.Errorf("choose sketch, keyframe, clip or reference")
	}
	if !containsField(allowed, ext) {
		return "", fmt.Errorf("unsupported media extension")
	}
	return ext, nil
}
func mediaSignature(ext string, head []byte) bool {
	s := string(head)
	switch ext {
	case ".png":
		return strings.HasPrefix(s, "\x89PNG\r\n\x1a\n")
	case ".jpg", ".jpeg":
		return strings.HasPrefix(s, "\xff\xd8\xff")
	case ".webp":
		return len(head) >= 12 && s[:4] == "RIFF" && s[8:12] == "WEBP"
	case ".mp4":
		return len(head) >= 8 && s[4:8] == "ftyp"
	case ".mov":
		return len(head) >= 8 && containsField([]string{"ftyp", "wide", "mdat", "moov"}, s[4:8])
	case ".webm":
		return strings.HasPrefix(s, "\x1a\x45\xdf\xa3")
	case ".wav":
		return len(head) >= 12 && s[:4] == "RIFF" && s[8:12] == "WAVE"
	}
	return false
}
func copyImportedFile(dir *os.Root, rel string, src io.Reader, length int64) (string, error) {
	if length <= 0 || length > maxUpload {
		return "", fmt.Errorf("use a media file smaller than 256 MiB")
	}
	if err := safeProjectPath(dir, rel, true); err != nil {
		return "", err
	}
	temp := rel + ".part"
	f, err := dir.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	defer dir.Remove(temp)
	digest := sha256.New()
	_, err = io.CopyN(io.MultiWriter(f, digest), src, length)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return "", fmt.Errorf("upload interrupted; source file is unchanged")
	}
	if closeErr != nil {
		return "", closeErr
	}
	f, err = dir.Open(temp)
	if err != nil {
		return "", err
	}
	head := make([]byte, 32)
	n, _ := f.Read(head)
	f.Close()
	if !mediaSignature(strings.ToLower(filepath.Ext(rel)), head[:n]) {
		return "", fmt.Errorf("file contents do not match its extension")
	}
	if err = replaceStoredFile(dir, temp, rel); err != nil {
		return "", err
	}
	parent, err := dir.OpenRoot(filepath.Dir(rel))
	if err != nil {
		return "", err
	}
	err = syncStorageDirectory(parent)
	parent.Close()
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func (s *ProjectStore) ImportMedia(id, sid, kind, filename string, length int64, src io.Reader, rev int, title, notes string) (ProjectDocument, error) {
	return s.importMedia(id, sid, kind, filename, length, src, rev, title, notes, nil)
}
func (s *ProjectStore) importMedia(id, sid, kind, filename string, length int64, src io.Reader, rev int, title, notes string, generation ProjectDocument) (ProjectDocument, error) {
	ext, err := assetExtension(kind, filename)
	if err != nil {
		return nil, err
	}
	if length <= 0 || length > maxUpload {
		return nil, fmt.Errorf("use a file smaller than 256 MiB")
	}
	if err = validateText(rawValue(title)); err != nil {
		return nil, err
	}
	if err = validateText(rawValue(notes)); err != nil {
		return nil, err
	}
	p, err := s.mutate(id, rev, func(p ProjectDocument) error {
		if generation != nil {
			shot, err := findShot(p, sid)
			if err != nil {
				return err
			}
			for _, asset := range list(shot, "assets") {
				if shotText(object(asset, generationProperty(generation)), "job_id") == shotText(generation, "job_id") {
					return nil
				}
			}
			generation["direction_stale_on_return"] = rawValue(directionBasis(p, shot) != shotText(generation, "basis"))
		}

		if kind != "reference" && kind != "soundtrack" {
			if _, err := findShot(p, sid); err != nil {
				return err
			}
		}
		if free, e := storageFree(s.path); e != nil {
			return e
		} else if free < uint64(length)+(1<<30) {
			return fmt.Errorf("keep at least 1 GiB free on the media drive")
		}
		aid, err := newStorageID()
		if err != nil {
			return err
		}
		folder := sid
		if kind == "reference" {
			folder = "references"
		}
		if kind == "soundtrack" {
			folder = "soundtrack"
		}
		if !projectIDPattern.MatchString(folder) {
			return fmt.Errorf("invalid media folder")
		}
		rel := "media/" + folder + "/" + aid + ext
		dir, err := projectDirectory(s.root, id)
		if err != nil {
			return err
		}
		defer dir.Close()
		hash, err := copyImportedFile(dir, rel, src, length)
		if err != nil {
			return err
		}
		record := ProjectDocument{"id": rawValue(aid), "path": rawValue(rel), "sha256": rawValue(hash), "bytes": rawValue(length), "original_name": rawValue(filepath.Base(strings.ReplaceAll(filename, "\\", "/"))), "at": rawValue(stamp())}
		if kind == "reference" {
			refs := list(p, "visual_references")
			for _, r := range refs {
				if shotText(r, "sha256") == hash {
					dir.Remove(rel)
					return nil
				}
			}
			record["title"], record["notes"] = rawValue(title), rawValue(notes)
			p["visual_references"] = rawValue(append(refs, record))
			return nil
		}
		if kind == "soundtrack" {
			info, err := probePCM(filepath.Join(dir.Name(), rel))
			if err != nil {
				dir.Remove(rel)
				return err
			}
			for k, v := range info {
				record[k] = v
			}
			history := list(p, "soundtrack_history")
			if old := object(p, "soundtrack"); old != nil {
				history = append(history, old)
			}
			p["soundtrack_history"], p["soundtrack"], p["music_video"] = rawValue(history), rawValue(record), rawValue(true)
			return nil
		}
		return editShotDocument(p, sid, func(shot ProjectDocument) error {
			assets := list(shot, "assets")
			take := 1
			for _, a := range assets {
				if shotText(a, "kind") == kind {
					take++
				}
			}
			record["kind"], record["take"], record["provider"] = rawValue(kind), rawValue(take), rawValue("manual")
			record["keyframe_id"] = rawValue(nil)
			if imageApproved(p, shot) {
				record["keyframe_id"] = shot["selected_keyframe"]
			}
			record["prompt_snapshot"] = rawValue(promptPackage(p, shot, "manual"))
			if generation != nil {
				if shotText(generation, "integration") == "local" {
					record["local_generation"] = rawValue(generation)
					record["provider"] = rawValue("Draw Things")
				} else {
					record["cloud_generation"] = rawValue(generation)
					record["provider"] = rawValue("OpenRouter · Google")
					record["keyframe_id"] = rawOr(generation, "starter_id", nil)
					record["prompt_snapshot"] = rawValue(ProjectDocument{"source": rawValue("cloud-job"), "job_id": generation["job_id"], "generation_prompt": generation["prompt"]})
				}
			}
			if kind == "clip" {
				if info, e := probeVideo(filepath.Join(dir.Name(), rel)); e == nil {
					for k, v := range info {
						record[k] = v
					}
					record["duration_source"] = rawValue("ffprobe")
					if generation != nil {
						record["in_seconds"], record["out_seconds"] = rawValue(0), info["duration_seconds"]
					}
					poster := "media/" + sid + "/" + aid + "-poster.jpg"
					if e = videoPoster(filepath.Join(dir.Name(), rel), filepath.Join(dir.Name(), poster)); e == nil {
						record["poster_path"] = rawValue(poster)
						if b, e := dir.ReadFile(poster); e == nil {
							hash := sha256.Sum256(b)
							record["poster_sha256"] = rawValue(hex.EncodeToString(hash[:]))
						}
					}
				} else {
					record["probe_error"] = rawValue(e.Error())
				}
			}
			shot["assets"] = rawValue(append(assets, record))
			return nil
		})
	})
	// Failed commits may leave unreferenced files, which remain recoverable. Never
	// delete a file after an ambiguous durability error that could have committed.
	return p, err
}

func (s *ProjectStore) Choose(id, sid, kind, aid string, approve bool, rev int) (ProjectDocument, error) {
	return s.mutate(id, rev, func(p ProjectDocument) error {
		if !containsField([]string{"sketch", "keyframe", "clip"}, kind) {
			return fmt.Errorf("invalid asset kind")
		}
		err := editShotDocument(p, sid, func(shot ProjectDocument) error {
			a, err := findAsset(shot, aid)
			if err != nil || shotText(a, "kind") != kind || shotText(a, "binned_at") != "" {
				return fmt.Errorf("choose an active take of the selected kind")
			}
			shot["selected_"+kind] = rawValue(aid)
			if approve {
				if kind != "keyframe" {
					return fmt.Errorf("only a finished image can be approved")
				}
				shot["approval"] = rawValue(ProjectDocument{"at": rawValue(stamp()), "asset_id": rawValue(aid), "fingerprint": rawValue(imageFingerprint(p, shot))})
			}
			return nil
		})
		if err != nil {
			return err
		}
		return validateCoverageReservations(p)
	})
}
func (s *ProjectStore) Bin(id, sid, aid, reason string, restore bool, rev int) (ProjectDocument, error) {
	if len([]rune(reason)) > 1000 {
		return nil, fmt.Errorf("bin reason is too long")
	}
	return s.mutate(id, rev, func(p ProjectDocument) error {
		return editAssetDocument(p, sid, aid, func(shot, a ProjectDocument) error {
			events := list(a, "bin_history")
			action := "bin"
			if restore {
				action = "restore"
			}
			events = append(events, ProjectDocument{"action": rawValue(action), "at": rawValue(stamp()), "reason": rawValue(reason)})
			a["bin_history"] = rawValue(events)
			if restore {
				delete(a, "binned_at")
				delete(a, "bin_reason")
			} else {
				a["binned_at"], a["bin_reason"] = rawValue(stamp()), rawValue(reason)
				key := "selected_" + shotText(a, "kind")
				if shotText(shot, key) == aid {
					shot[key] = rawValue(nil)
					if shotText(a, "kind") == "keyframe" {
						shot["approval"] = rawValue(nil)
					}
				}
			}
			return nil
		})
	})
}
func (s *ProjectStore) TrimClip(id, sid, aid string, rev int, start, end, measured float64) (ProjectDocument, error) {
	if !finite(start) || !finite(end) || start < 0 || end <= start || end > 7200 || (!finite(measured) || measured < 0 || measured > 7200) {
		return nil, fmt.Errorf("enter a valid clip range")
	}
	return s.mutate(id, rev, func(p ProjectDocument) error {
		return editAssetDocument(p, sid, aid, func(_, a ProjectDocument) error {
			if shotText(a, "kind") != "clip" {
				return fmt.Errorf("choose a clip")
			}
			duration := number(a, "duration_seconds")
			if duration <= 0 {
				duration = measured
			}
			if duration <= 0 || end > duration+.05 {
				return fmt.Errorf("range must fit within the source duration")
			}
			if number(a, "duration_seconds") == 0 {
				a["duration_seconds"], a["duration_source"] = rawValue(duration), rawValue("browser-metadata")
			}
			a["in_seconds"], a["out_seconds"] = rawValue(start), rawValue(min(end, duration))
			return nil
		})
	})
}
func (s *ProjectStore) ReviewClip(id, sid, aid string, rev int, status, setup, phase, note string) (ProjectDocument, error) {
	if !containsField([]string{"pending", "candidate", "avoid"}, status) || len([]rune(setup)) > 100 || len([]rune(phase)) > 100 || len([]rune(note)) > 2000 {
		return nil, fmt.Errorf("invalid clip review")
	}
	return s.mutate(id, rev, func(p ProjectDocument) error {
		return editAssetDocument(p, sid, aid, func(_, a ProjectDocument) error {
			if shotText(a, "kind") != "clip" || object(a, "source_ref") != nil || shotText(a, "binned_at") != "" {
				return fmt.Errorf("choose an active original clip")
			}
			a["editorial_review"] = rawValue(ProjectDocument{"status": rawValue(status), "setup": rawValue(strings.TrimSpace(setup)), "phase": rawValue(strings.TrimSpace(phase)), "note": rawValue(note), "at": rawValue(stamp())})
			return nil
		})
	})
}

func generationProperty(p ProjectDocument) string {
	if shotText(p, "integration") == "local" {
		return "local_generation"
	}
	return "cloud_generation"
}
