package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"html"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type sectionEntry struct {
	row, asset              ProjectDocument
	path, title, sourceShot string
}

func (s *ProjectStore) sectionEntries(p, section ProjectDocument, rows []ProjectDocument) ([]sectionEntry, error) {
	entries := []sectionEntry{}
	for _, row := range rows {
		ss, a, path, err := s.sectionSource(p, shotText(row, "source_shot"), shotText(row, "source_asset"))
		if err != nil {
			return nil, err
		}
		if hash := shotText(row, "source_sha256"); hash != "" && hash != shotText(a, "sha256") {
			return nil, fmt.Errorf("a selected source changed; review the footage")
		}
		duration, err := sectionRange(a, number(row, "in_seconds"), number(row, "out_seconds"), number(row, "source_duration_seconds"))
		if err != nil {
			return nil, err
		}
		a["duration_seconds"] = rawValue(duration)
		shot, _ := findShot(p, ss)
		title := shotText(shot, "title")
		if shotText(row, "angle") != "" {
			title = shotText(row, "angle")
		}
		entries = append(entries, sectionEntry{row, a, path, fmt.Sprintf("%s · Take %g", title, number(a, "take")), ss})
	}
	return entries, nil
}
func (s *ProjectStore) ExportReel(id, sid string, rev int, scope string) (string, error) {
	s.mu.Lock()
	locked := true
	defer func() {
		if locked {
			s.mu.Unlock()
		}
	}()
	p, err := s.load(id)
	if err != nil {
		return "", err
	}
	if documentRevision(p) != rev {
		return "", ErrRevision
	}
	prepareCoverage(p)
	section, err := findSection(p, sid)
	if err != nil {
		return "", err
	}
	if _, err = soundtrack(p, section); err != nil {
		return "", err
	}
	if err = validateCoverageReservations(p); err != nil {
		return "", err
	}
	if !containsField([]string{"coverage", "preferred", "assembly"}, scope) {
		return "", fmt.Errorf("choose full options, preferred ranges or assembly")
	}
	rows := list(section, "coverage")
	if scope == "assembly" {
		rows = list(section, "clips")
	} else {
		included := []ProjectDocument{}
		for _, row := range rows {
			if !hasFalse(row, "include_in_handoff") {
				included = append(included, row)
			}
		}
		rows = included
	}
	if len(rows) == 0 {
		return "", fmt.Errorf("choose footage for the selects reel")
	}
	entries, err := s.sectionEntries(p, section, rows)
	if err != nil {
		return "", err
	}
	dir, err := projectDirectory(s.root, id)
	if err != nil {
		return "", err
	}
	defer dir.Close()
	uid, err := newStorageID()
	if err != nil {
		return "", err
	}
	stem := "exports/selects-" + uid
	if err = safeProjectPath(dir, stem+".mp4", true); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(filepath.Join(dir.Name(), "exports"), ".reel-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	segments := []videoSegment{}
	cuts := []ProjectDocument{}
	elapsed := 0.0
	for i, e := range entries {
		start, end := number(e.row, "in_seconds"), number(e.row, "out_seconds")
		if scope == "coverage" {
			start, end = 0, number(e.asset, "duration_seconds")
		}
		segments = append(segments, videoSegment{e.path, start, end})
		cut := ProjectDocument{"order": rawValue(i + 1), "entry_id": e.row["id"], "title": rawValue(e.title), "take": rawOr(e.asset, "take", 1), "source_shot": rawValue(e.sourceShot), "source_asset": e.asset["id"], "source_sha256": rawOr(e.asset, "sha256", nil), "source_in_seconds": rawValue(start), "source_out_seconds": rawValue(end), "reel_in_seconds": rawValue(math.RoundToEven(elapsed*1e6) / 1e6), "reel_out_seconds": rawValue(math.RoundToEven((elapsed+end-start)*1e6) / 1e6), "angle": rawOr(e.row, "angle", ""), "note": rawOr(e.row, "note", ""), "music_tag": rawOr(e.row, "music_tag", "none"), "exclusive": rawOr(e.row, "exclusive", false)}
		cuts = append(cuts, cut)
		elapsed += end - start
	}
	s.mu.Unlock()
	locked = false
	info, err := renderVideoReel(segments, filepath.Join(tmp, "reel.mp4"), shotText(p, "aspect"))
	if err != nil {
		return "", err
	}
	manifest := ProjectDocument{"schema": rawValue("sbd-selects-reel/1"), "project_id": rawValue(id), "project_revision": rawValue(rev), "section_id": rawValue(sid), "section_label": section["label"], "song_start_seconds": section["start_seconds"], "song_end_seconds": section["end_seconds"], "song_section_seconds": rawValue(number(section, "end_seconds") - number(section, "start_seconds")), "reel_seconds": rawValue(math.RoundToEven(elapsed*1e6) / 1e6), "scope": rawValue(scope), "cuts": rawValue(cuts), "export_metadata": rawValue(info), "exported_at": rawValue(stamp()), "editing_note": rawValue("Overlength silent picture reel. Split, trim and rearrange in your editor. Song timing is a target only; no footage is stretched or cut to fit. Audio remains separate.")}
	if err = os.WriteFile(filepath.Join(tmp, "cuts.json"), []byte(jsonText(manifest)+"\n"), 0600); err != nil {
		return "", err
	}
	if err = os.WriteFile(filepath.Join(tmp, "cuts.csv"), []byte(documentsCSV(cuts)), 0600); err != nil {
		return "", err
	}
	s.mu.Lock()
	locked = true
	if err = s.check(); err != nil {
		return "", err
	}
	// Publish maps before the MP4 so a visible movie always has its source maps.
	for _, pair := range [][2]string{{"cuts.json", ".json"}, {"cuts.csv", ".csv"}, {"reel.mp4", ".mp4"}} {
		source := filepath.Join(tmp, pair[0])
		f, err := os.OpenFile(source, os.O_RDWR, 0600)
		if err != nil {
			return "", err
		}
		err = f.Sync()
		f.Close()
		if err != nil {
			return "", err
		}
		if err = replaceStoredFile(dir, filepath.Join("exports", filepath.Base(tmp), pair[0]), stem+pair[1]); err != nil {
			return "", err
		}
	}
	parent, err := dir.OpenRoot("exports")
	if err != nil {
		return "", err
	}
	err = syncStorageDirectory(parent)
	parent.Close()
	return stem + ".mp4", err
}
func documentsCSV(rows []ProjectDocument) string {
	var b strings.Builder
	w := csv.NewWriter(&b)
	if len(rows) > 0 {
		keys := []string{}
		for k := range rows[0] {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		w.Write(keys)
		for _, row := range rows {
			values := []string{}
			for _, k := range keys {
				value := shotText(row, k)
				if value == "" {
					if raw, ok := row[k]; ok && !nullJSON(raw) {
						value = string(raw)
					}
				}
				values = append(values, value)
			}
			w.Write(values)
		}
	}
	w.Flush()
	return b.String()
}
func archiveID(id string) string {
	if projectIDPattern.MatchString(id) {
		return id
	}
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:6])
}
func (s *ProjectStore) ExportSection(id, sid string, rev int, includeCoverage bool) (string, error) {
	return s.exportPackage(id, "section", func(z *zip.Writer, dir *os.Root, p ProjectDocument) error {
		if documentRevision(p) != rev {
			return ErrRevision
		}
		prepareCoverage(p)
		section, err := findSection(p, sid)
		if err != nil {
			return err
		}
		track, err := soundtrack(p, section)
		if err != nil {
			return err
		}
		if err = validateCoverageReservations(p); err != nil {
			return err
		}
		entries, err := s.sectionEntries(p, section, list(section, "clips"))
		if err != nil {
			return err
		}
		rows := []ProjectDocument{}
		if includeCoverage {
			for _, row := range list(section, "coverage") {
				if !hasFalse(row, "include_in_handoff") {
					rows = append(rows, row)
				}
			}
		}
		options, err := s.sectionEntries(p, section, rows)
		if err != nil {
			return err
		}
		if len(entries)+len(options) == 0 {
			return fmt.Errorf("add assembly ranges or coverage options before exporting")
		}
		trackRel := shotText(track, "path")
		if err = safeProjectPath(dir, trackRel, false); err != nil {
			return err
		}
		f, err := dir.Open(trackRel)
		if err != nil {
			return err
		}
		hash := sha256.New()
		_, err = io.Copy(hash, f)
		f.Close()
		if err != nil {
			return err
		}
		if hex.EncodeToString(hash.Sum(nil)) != shotText(section, "track_sha256") {
			return fmt.Errorf("original soundtrack bytes changed; export stopped")
		}
		tmp, err := os.MkdirTemp("", "sbd-section-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		ranges, coverage := []ProjectDocument{}, []ProjectDocument{}
		sources := map[string]string{}
		sourcePaths := map[string]string{}
		elapsed := 0.0
		for group, selected := range [][]sectionEntry{entries, options} {
			for i, e := range selected {
				key := e.sourceShot + "/" + shotText(e.asset, "id")
				name := "sources/" + archiveID(e.sourceShot) + "-" + archiveID(shotText(e.asset, "id")) + filepath.Ext(e.path)
				sources[key] = name
				sourcePaths[name] = shotText(e.asset, "path")
				folder := "mp4-ranges"
				start, end := number(e.row, "in_seconds"), number(e.row, "out_seconds")
				if group == 1 {
					folder = "coverage-options"
					start, end = 0, number(e.asset, "duration_seconds")
				}
				filename := fmt.Sprintf("%s/%03d_%s.mp4", folder, i+1, archiveID(shotText(e.asset, "id")))
				target := filepath.Join(tmp, fmt.Sprintf("%d-%03d.mp4", group, i))
				meta, err := renderVideoRange(e.path, target, start, end)
				if err != nil {
					return err
				}
				src, err := os.Open(target)
				if err != nil {
					return err
				}
				dst, err := z.Create(filename)
				if err != nil {
					src.Close()
					return err
				}
				_, err = io.Copy(dst, src)
				src.Close()
				if err != nil {
					return err
				}
				row := ProjectDocument{"title": rawValue(e.title), "mp4": rawValue(filename), "source": rawValue(name), "export_metadata": rawValue(meta)}
				if group == 0 {
					row["order"], row["entry_id"], row["source_in_seconds"], row["source_out_seconds"], row["assembly_start_seconds"], row["song_start_seconds"] = rawValue(i+1), e.row["id"], e.row["in_seconds"], e.row["out_seconds"], rawValue(elapsed), rawValue(number(section, "start_seconds")+elapsed)
					elapsed += end - start
					ranges = append(ranges, row)
				} else {
					row["coverage_id"], row["source_shot"], row["source_asset"], row["suggested_in_seconds"], row["suggested_out_seconds"] = e.row["id"], rawValue(e.sourceShot), e.asset["id"], e.row["in_seconds"], e.row["out_seconds"]
					for _, k := range []string{"angle", "note", "music_tag", "exclusive"} {
						row[k] = rawOr(e.row, k, nil)
					}
					coverage = append(coverage, row)
				}
			}
		}
		excerpt, err := pcmExcerpt(filepath.Join(dir.Name(), trackRel), number(section, "start_seconds"), number(section, "end_seconds"))
		if err != nil {
			return err
		}
		dst, err := z.Create("soundtrack/section.wav")
		if err != nil {
			return err
		}
		if _, err = dst.Write(excerpt); err != nil {
			return err
		}
		if err = zipProjectFile(z, dir, trackRel, "soundtrack/original.wav"); err != nil {
			return err
		}
		for name, rel := range sourcePaths {
			if err = zipProjectFile(z, dir, rel, name); err != nil {
				return err
			}
		}
		cues := []ProjectDocument{}
		for _, c := range list(p, "music_cues") {
			if shotText(c, "track_sha256") != shotText(section, "track_sha256") {
				continue
			}
			start, end := number(c, "seconds"), number(c, "end_seconds")
			if _, ok := c["end_seconds"]; !ok {
				end = start
			}
			if start < number(section, "end_seconds") && (end > number(section, "start_seconds") || start >= number(section, "start_seconds")) {
				cues = append(cues, c)
			}
		}
		manifest := ProjectDocument{"schema": rawValue("sbd-section-handoff/1"), "project_id": rawValue(id), "project_revision": rawValue(rev), "section": rawValue(section), "assembly_seconds": rawValue(math.RoundToEven(elapsed*1e6) / 1e6), "song_section_seconds": rawValue(number(section, "end_seconds") - number(section, "start_seconds")), "range_files": rawValue(ranges), "coverage_options": rawValue(coverage), "include_coverage": rawValue(includeCoverage), "cues": rawValue(cues), "clip_audio": rawValue("excluded from numbered MP4 ranges and FCPXML video clips; full sources retain original tracks"), "timeline_clock": rawValue("local 0 corresponds to original-song section start"), "frame_rate": rawValue(24), "exported_at": rawValue(stamp())}
		docs := map[string]string{"section.json": jsonText(manifest), "coverage.json": jsonText(ProjectDocument{"included": rawValue(includeCoverage), "options": rawValue(coverage), "all_candidates": rawOr(section, "coverage", []any{})}), "timing.csv": documentsCSV(ranges), "lyric-cues.csv": documentsCSV(cues), "Final Cut.fcpxml": sectionXML(p, section, entries, options, sources), "README.txt": fmt.Sprintf("%s · original song %.3f–%.3f seconds\n\nExtract this ZIP before importing. Numbered mp4-ranges/ are separate silent source ranges. coverage-options/ contains full-length silent alternatives with suggested ranges in coverage.json. Full sources retain edit handles and original audio; soundtrack/section.wav supplies the section excerpt.\n\niMovie: import numbered ranges and the song excerpt. Final Cut: import Final Cut.fcpxml and relink relative sources/ and soundtrack/ paths if necessary. Its section-local timeline starts at zero, with original-song audio from %.3f seconds. XML boundaries use a 24fps clock; actual editor import is platform verification work.\n\nSection %.3fs; assembly %.3fs. No footage was stretched or automatically fitted to the song. Full cue timings may cross a section boundary; XML markers are bounded to this section. Source media and project files remain intact.\n", shotText(section, "label"), number(section, "start_seconds"), number(section, "end_seconds"), number(section, "start_seconds"), number(section, "end_seconds")-number(section, "start_seconds"), elapsed)}
		for name, text := range docs {
			if err = zipText(z, name, text); err != nil {
				return err
			}
		}
		return nil
	})
}
func pcmExcerpt(path string, start, end float64) ([]byte, error) {
	if _, err := probePCM(path); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, _ := f.Stat()
	var format []byte
	offset, dataSize := int64(0), int64(0)
	for pos := int64(12); pos+8 <= info.Size(); {
		head := make([]byte, 8)
		if _, err = f.ReadAt(head, pos); err != nil {
			return nil, err
		}
		size := int64(binary.LittleEndian.Uint32(head[4:]))
		pos += 8
		if string(head[:4]) == "fmt " {
			format = make([]byte, size)
			if _, err = f.ReadAt(format, pos); err != nil {
				return nil, err
			}
		}
		if string(head[:4]) == "data" {
			offset, dataSize = pos, size
			break
		}
		pos += size + size%2
	}
	if len(format) < 16 || offset == 0 {
		return nil, fmt.Errorf("invalid WAV source")
	}
	rate := binary.LittleEndian.Uint32(format[4:])
	align := int64(binary.LittleEndian.Uint16(format[12:]))
	first := int64(math.RoundToEven(start*float64(rate))) * align
	count := int64(math.RoundToEven((end-start)*float64(rate))) * align
	if first < 0 || count <= 0 || first+count > dataSize {
		return nil, fmt.Errorf("section audio range exceeds the recording")
	}
	data := make([]byte, count)
	if _, err = f.ReadAt(data, offset+first); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+len(data)+len(data)%2))
	b.WriteString("WAVEfmt ")
	binary.Write(&b, binary.LittleEndian, uint32(16))
	pcmFormat := append([]byte(nil), format[:16]...)
	binary.LittleEndian.PutUint16(pcmFormat, 1)
	b.Write(pcmFormat)
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(len(data)))
	b.Write(data)
	if len(data)%2 == 1 {
		b.WriteByte(0)
	}
	return b.Bytes(), nil
}
func frameTime(seconds float64) string {
	frames := int64(math.RoundToEven(seconds * 24))
	a, b := frames, int64(24)
	for b != 0 {
		a, b = b, a%b
	}
	if a < 0 {
		a = -a
	}
	if a == 0 {
		return "0s"
	}
	n, d := frames/a, int64(24)/a
	if d == 1 {
		return fmt.Sprintf("%ds", n)
	}
	return fmt.Sprintf("%d/%ds", n, d)
}
func xmlTag(name string, attributes map[string]string, inner string) string {
	keys := []string{}
	for k := range attributes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("<" + name)
	for _, k := range keys {
		b.WriteString(" " + k + `="` + html.EscapeString(attributes[k]) + `"`)
	}
	if inner == "" {
		b.WriteString("/>")
	} else {
		b.WriteString(">" + inner + "</" + name + ">")
	}
	return b.String()
}
func sectionXML(p, section ProjectDocument, entries, options []sectionEntry, sources map[string]string) string {
	sizes := map[string][2]string{"16:9": {"1280", "720"}, "9:16": {"720", "1280"}, "1:1": {"1080", "1080"}}
	size := sizes[shotText(p, "aspect")]
	resources := xmlTag("format", map[string]string{"id": "r1", "name": "SBD 24 fps", "frameDuration": "1/24s", "width": size[0], "height": size[1], "colorSpace": "1-1-1"}, "")
	refs := map[string]string{}
	for _, e := range append(append([]sectionEntry{}, entries...), options...) {
		key := e.sourceShot + "/" + shotText(e.asset, "id")
		if refs[key] != "" {
			continue
		}
		rid := "r" + strconv.Itoa(len(refs)+2)
		refs[key] = rid
		escaped := []string{}
		for _, part := range strings.Split(sources[key], "/") {
			escaped = append(escaped, url.PathEscape(part))
		}
		audio := "1"
		if _, ok := e.asset["audio_tracks"]; ok && number(e.asset, "audio_tracks") == 0 {
			audio = "0"
		}
		resources += xmlTag("asset", map[string]string{"id": rid, "name": e.title, "start": "0s", "duration": frameTime(number(e.asset, "duration_seconds")), "hasVideo": "1", "hasAudio": audio, "format": "r1"}, xmlTag("media-rep", map[string]string{"kind": "original-media", "src": strings.Join(escaped, "/")}, ""))
	}
	track := object(p, "soundtrack")
	resources += xmlTag("asset", map[string]string{"id": "music", "name": shotText(track, "original_name"), "start": "0s", "duration": frameTime(number(track, "duration_seconds")), "hasVideo": "0", "hasAudio": "1", "audioSources": "1", "audioChannels": fmt.Sprint(int(number(track, "channels"))), "audioRate": fmt.Sprint(int(number(track, "sample_rate")))}, xmlTag("media-rep", map[string]string{"kind": "original-media", "src": "soundtrack/original.wav"}, ""))
	elapsed := 0.0
	gap := ""
	for _, e := range entries {
		length := number(e.row, "out_seconds") - number(e.row, "in_seconds")
		gap += xmlTag("asset-clip", map[string]string{"name": e.title, "ref": refs[e.sourceShot+"/"+shotText(e.asset, "id")], "lane": "1", "offset": frameTime(elapsed), "start": frameTime(number(e.row, "in_seconds")), "duration": frameTime(length), "srcEnable": "video"}, "")
		elapsed += length
	}
	sectionLength := number(section, "end_seconds") - number(section, "start_seconds")
	length := max(sectionLength, elapsed)
	gap += xmlTag("asset-clip", map[string]string{"name": shotText(track, "original_name"), "ref": "music", "lane": "-1", "offset": "0s", "start": frameTime(number(section, "start_seconds")), "duration": frameTime(sectionLength), "srcEnable": "audio", "audioRole": "music"}, "")
	for _, c := range list(p, "music_cues") {
		if shotText(c, "track_sha256") != shotText(section, "track_sha256") {
			continue
		}
		st := max(number(section, "start_seconds"), number(c, "seconds"))
		en := min(number(section, "end_seconds"), max(number(c, "seconds"), number(c, "end_seconds")))
		if (number(c, "seconds") >= number(section, "start_seconds") && number(c, "seconds") < number(section, "end_seconds")) || en > st {
			gap += xmlTag("marker", map[string]string{"start": frameTime(st - number(section, "start_seconds")), "duration": frameTime(max(1.0/24, en-st)), "value": shotText(c, "label")}, "")
		}
	}
	gap += xmlTag("marker", map[string]string{"start": frameTime(max(0, sectionLength-1.0/24)), "duration": "1/24s", "value": shotText(section, "label") + " ends", "note": "Owner section boundary; assembly may have extra coverage beyond this point."}, "")
	timeline := xmlTag("project", map[string]string{"name": shotText(section, "label") + " · SBD assembly"}, xmlTag("sequence", map[string]string{"format": "r1", "duration": frameTime(length), "tcStart": "0s", "tcFormat": "NDF", "audioLayout": "stereo", "audioRate": "48k"}, xmlTag("spine", nil, xmlTag("gap", map[string]string{"name": shotText(section, "label"), "offset": "0s", "start": "0s", "duration": frameTime(length)}, gap))))
	browser := ""
	for _, e := range options {
		duration := frameTime(number(e.asset, "duration_seconds"))
		browser += xmlTag("asset-clip", map[string]string{"name": e.title + " · Coverage option", "ref": refs[e.sourceShot+"/"+shotText(e.asset, "id")], "start": "0s", "duration": duration, "srcEnable": "video"}, xmlTag("keyword", map[string]string{"start": "0s", "duration": duration, "value": shotText(section, "label") + " · Coverage options"}, ""))
	}
	return "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!DOCTYPE fcpxml>\n" + xmlTag("fcpxml", map[string]string{"version": "1.10"}, xmlTag("resources", nil, resources)+xmlTag("library", nil, xmlTag("event", map[string]string{"name": shotText(p, "title")}, timeline+browser))) + "\n"
}
