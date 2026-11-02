package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixtureWAV(seconds int) []byte {
	var b bytes.Buffer
	size := seconds * 8000 * 2
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+size))
	b.WriteString("WAVEfmt ")
	binary.Write(&b, binary.LittleEndian, uint32(16))
	binary.Write(&b, binary.LittleEndian, uint16(1))
	binary.Write(&b, binary.LittleEndian, uint16(1))
	binary.Write(&b, binary.LittleEndian, uint32(8000))
	binary.Write(&b, binary.LittleEndian, uint32(16000))
	binary.Write(&b, binary.LittleEndian, uint16(2))
	binary.Write(&b, binary.LittleEndian, uint16(16))
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(size))
	b.Write(make([]byte, size))
	return b.Bytes()
}
func TestMusicPortionsCuesAndUndo(t *testing.T) {
	s := testStore(t)
	p := createTestProject(t, s)
	id := shotText(p, "id")
	wav := fixtureWAV(10)
	p, err := s.ImportMedia(id, "", "soundtrack", "song.wav", int64(len(wav)), bytes.NewReader(wav), 1, "", "")
	if err != nil {
		t.Fatal(err)
	}
	p, err = s.SplitSections(id, 2, 0, 10, 3, "Part", "none")
	if err != nil {
		t.Fatal(err)
	}
	sections := list(p, "music_sections")
	if len(sections) != 4 || number(sections[3], "end_seconds") != 10 || number(sections[3], "start_seconds") != 9 {
		t.Fatal("portion boundary drift")
	}
	if _, err = s.SplitSections(id, 3, 1, 5, 1, "Overlap", "verse"); err == nil {
		t.Fatal("accepted overlapping portions")
	}
	end := 4.5
	p, err = s.MusicCue(id, 3, "Crossing lyric", 2.5, &end, "", false)
	if err != nil {
		t.Fatal(err)
	}
	cue := list(p, "music_cues")[0]
	if number(cue, "end_seconds") != 4.5 {
		t.Fatal("lyric end changed")
	}
	p, err = s.UndoSection(id, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(list(p, "music_sections")) != 0 || len(list(p, "music_cues")) != 1 {
		t.Fatal("undo affected unrelated cues")
	}
	oldTrack := object(p, "soundtrack")
	wav2 := fixtureWAV(8)
	p, err = s.ImportMedia(id, "", "soundtrack", "next.wav", int64(len(wav2)), bytes.NewReader(wav2), 5, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if shotText(list(p, "soundtrack_history")[0], "sha256") != shotText(oldTrack, "sha256") {
		t.Fatal("soundtrack history lost")
	}
	if _, err = s.MusicCue(id, 6, "Too late", 9, nil, "", false); err == nil {
		t.Fatal("accepted cue outside song")
	}
}
func TestActualSectionCoverageReservationsAndExports(t *testing.T) {
	if _, err := exec.LookPath(videoTool("ffmpeg")); err != nil {
		if os.Getenv("STORYBOARD_REQUIRE_FFMPEG") == "1" {
			t.Fatal(err)
		}
		t.Skip("FFmpeg unavailable")
	}
	source := filepath.Join(t.TempDir(), "source.mp4")
	_, err := runMedia(30*time.Second, "ffmpeg", "-v", "error", "-nostdin", "-n", "-f", "lavfi", "-i", "testsrc2=size=160x90:rate=24", "-f", "lavfi", "-i", "sine=frequency=220:sample_rate=8000", "-t", "2", "-c:v", "libx264", "-threads", "2", "-c:a", "aac", source)
	if err != nil {
		t.Fatal(err)
	}
	video, _ := os.ReadFile(source)
	originalHash := sha256.Sum256(video)
	s := testStore(t)
	p := createTestProject(t, s)
	id := shotText(p, "id")
	sid := shotText(list(p, "shots")[0], "id")
	wav := fixtureWAV(4)
	p, err = s.ImportMedia(id, "", "soundtrack", "song.wav", int64(len(wav)), bytes.NewReader(wav), 1, "", "")
	if err != nil {
		t.Fatal(err)
	}
	p, err = s.ImportMedia(id, sid, "clip", "source.mp4", int64(len(video)), bytes.NewReader(video), 2, "", "")
	if err != nil {
		t.Fatal(err)
	}
	asset := list(list(p, "shots")[0], "assets")[0]
	aid := shotText(asset, "id")
	p, err = s.UpdateShot(id, sid, 3, ProjectDocument{"scene": rawValue("Verse")})
	if err != nil {
		t.Fatal(err)
	}
	p, err = s.SaveSection(id, 4, ProjectDocument{"label": rawValue("Verse"), "kind": rawValue("verse"), "start_seconds": rawValue(0), "end_seconds": rawValue(2)})
	if err != nil {
		t.Fatal(err)
	}
	sectionID := shotText(list(p, "music_sections")[0], "id")
	add := ProjectDocument{"operation": rawValue("add"), "source_shot": rawValue(sid), "source_asset": rawValue(aid), "in_seconds": rawValue(.1), "out_seconds": rawValue(1.9)}
	p, err = s.SectionEdit(id, sectionID, 5, add, true)
	if err != nil {
		t.Fatal(err)
	}
	covID := shotText(list(list(p, "music_sections")[0], "coverage")[0], "id")
	p, err = s.SectionEdit(id, sectionID, 6, ProjectDocument{"operation": rawValue("update"), "coverage": rawValue(covID), "exclusive": rawValue(true), "angle": rawValue("Wide"), "include_in_handoff": rawValue(true)}, true)
	if err != nil {
		t.Fatal(err)
	}
	p, err = s.Choose(id, sid, "clip", aid, false, 7)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateShot(id, sid, 8, ProjectDocument{"scene": rawValue("Chorus")}); err == nil {
		t.Fatal("reservation allowed conflicting scene")
	}
	add["in_seconds"], add["out_seconds"] = rawValue(.25), rawValue(1.25)
	p, err = s.SectionEdit(id, sectionID, 8, add, false)
	if err != nil {
		t.Fatal(err)
	}
	end := 2.8
	p, err = s.MusicCue(id, 9, "Crossing lyric", .9, &end, "", false)
	if err != nil {
		t.Fatal(err)
	}
	rel, err := s.ExportReel(id, sectionID, 10, "preferred")
	if err != nil {
		t.Fatal(err)
	}
	info, err := probeVideo(filepath.Join(s.path, id, rel))
	if err != nil || number(info, "audio_tracks") != 0 {
		t.Fatal("reel not silent")
	}
	cutsRaw, err := os.ReadFile(filepath.Join(s.path, id, strings.TrimSuffix(rel, ".mp4")+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var cuts ProjectDocument
	json.Unmarshal(cutsRaw, &cuts)
	if number(cuts, "song_section_seconds") != 2 || number(cuts, "reel_seconds") != 1.8 {
		t.Fatalf("reel was fitted to song: %s", cutsRaw)
	}
	rel, err = s.ExportSection(id, sectionID, 10, true)
	if err != nil {
		t.Fatal(err)
	}
	f, err := s.OpenMedia(id, rel)
	if err != nil {
		t.Fatal(err)
	}
	stat, _ := f.Stat()
	z, err := zip.NewReader(f, stat.Size())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	files := map[string][]byte{}
	for _, entry := range z.File {
		stream, _ := entry.Open()
		files[entry.Name], _ = io.ReadAll(stream)
		stream.Close()
	}
	for _, name := range []string{"section.json", "Final Cut.fcpxml", "soundtrack/section.wav", "soundtrack/original.wav", "coverage.json", "timing.csv", "lyric-cues.csv"} {
		if len(files[name]) == 0 {
			t.Fatalf("missing %s", name)
		}
	}
	var manifest ProjectDocument
	if json.Unmarshal(files["section.json"], &manifest) != nil {
		t.Fatal("bad manifest")
	}
	if number(list(manifest, "cues")[0], "end_seconds") != 2.8 {
		t.Fatal("cross-boundary lyric cue was truncated")
	}
	if number(manifest, "assembly_seconds") != 1 {
		t.Fatal("assembly duration changed")
	}
	decoder := xml.NewDecoder(bytes.NewReader(files["Final Cut.fcpxml"]))
	for {
		_, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	excerpt := filepath.Join(t.TempDir(), "section.wav")
	os.WriteFile(excerpt, files["soundtrack/section.wav"], 0600)
	pcm, err := probePCM(excerpt)
	if err != nil || number(pcm, "duration_seconds") != 2 {
		t.Fatal("wrong audio excerpt")
	}
	after, _ := os.ReadFile(source)
	if sha256.Sum256(after) != originalHash || !bytes.Equal(files["soundtrack/original.wav"], wav) {
		t.Fatal("source bytes changed")
	}
}
