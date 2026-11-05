package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func roundSeconds(n float64) float64 { return math.Round(n*1000) / 1000 }
func finite(n float64) bool          { return !math.IsNaN(n) && !math.IsInf(n, 0) }
func videoTool(name string) string {
	if p := os.Getenv("STORYBOARD_" + strings.ToUpper(name)); p != "" {
		return p
	}
	if path := configuredMediaTools[name]; path != "" {
		return path
	}
	return name
}
func runMedia(timeout time.Duration, name string, args ...string) ([]byte, error) {
	return runProgram(timeout, videoTool(name), args...)
}
func runProgram(timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var out, stderr bytes.Buffer
	cmd.Env = childEnvironment()
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("local video processing failed or timed out; check the media and FFmpeg/ffprobe configuration; source files are retained")
	}
	return out.Bytes(), nil
}
func probeVideo(path string) (ProjectDocument, error) {
	if info, handled, err := nativeProbe(path); handled || err != nil {
		return info, err
	}
	raw, err := runMedia(30*time.Second, "ffprobe", "-v", "error", "-show_format", "-show_streams", "-of", "json", path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Streams []struct {
			Type          string `json:"codec_type"`
			Duration      string `json:"duration"`
			Width, Height int
			Tags          map[string]string
			Side          []struct{ Rotation float64 } `json:"side_data_list"`
		}
		Format struct{ Duration string }
	}
	if json.Unmarshal(raw, &doc) != nil {
		return nil, fmt.Errorf("invalid video metadata")
	}
	audio := 0
	var info ProjectDocument
	for _, s := range doc.Streams {
		if s.Type == "audio" {
			audio++
		}
		if s.Type != "video" || info != nil {
			continue
		}
		duration, _ := strconv.ParseFloat(s.Duration, 64)
		if duration <= 0 {
			duration, _ = strconv.ParseFloat(doc.Format.Duration, 64)
		}
		width, height := s.Width, s.Height
		rotation, _ := strconv.ParseFloat(s.Tags["rotate"], 64)
		for _, side := range s.Side {
			rotation = side.Rotation
		}
		if math.Mod(math.Abs(rotation), 180) == 90 {
			width, height = height, width
		}
		if !finite(duration) || duration <= 0 || width <= 0 || height <= 0 {
			return nil, fmt.Errorf("video duration or dimensions unavailable")
		}
		info = ProjectDocument{"duration_seconds": rawValue(duration), "width": rawValue(width), "height": rawValue(height)}
	}
	if info == nil {
		return nil, fmt.Errorf("source has no readable video track")
	}
	info["audio_tracks"] = rawValue(audio)
	return info, nil
}
func videoPoster(source, target string) error {
	if handled, err := nativePoster(source, target); handled || err != nil {
		return err
	}
	info, err := probeVideo(source)
	if err != nil {
		return err
	}
	_, err = runMedia(30*time.Second, "ffmpeg", "-v", "error", "-nostdin", "-n", "-ss", fmt.Sprint(min(.1, number(info, "duration_seconds")/2)), "-i", source, "-map", "0:v:0", "-frames:v", "1", "-vf", "scale=640:640:force_original_aspect_ratio=decrease", "-threads", "2", target)
	return err
}
func verifySilent(path string, duration float64) (ProjectDocument, error) {
	info, err := probeVideo(path)
	if err != nil {
		return nil, err
	}
	if number(info, "audio_tracks") != 0 || math.Abs(number(info, "duration_seconds")-duration) > .09 {
		return nil, fmt.Errorf("export failed the silent-picture or duration check")
	}
	return info, nil
}
func encodeRange(source, target string, start, end float64, frames int, size []int, timeout time.Duration) error {
	if !finite(start) || !finite(end) || start < 0 || end <= start || frames < 1 {
		return fmt.Errorf("choose a valid source range contributing at least one frame")
	}
	filters := []string{fmt.Sprintf("trim=duration=%.9f", end-start), "setpts=PTS-STARTPTS", "fps=24"}
	if len(size) == 2 {
		filters = append(filters, fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease:force_divisible_by=2", size[0], size[1]), fmt.Sprintf("pad=%d:%d:(ow-iw)/2:(oh-ih)/2", size[0], size[1]))
	} else {
		filters = append(filters, "scale=trunc(iw/2)*2:trunc(ih/2)*2")
	}
	filters = append(filters, "setsar=1", "tpad=stop_mode=clone:stop_duration=1")
	_, err := runMedia(timeout, "ffmpeg", "-v", "error", "-nostdin", "-n", "-ss", fmt.Sprintf("%.9f", start), "-i", source, "-map", "0:v:0", "-an", "-sn", "-dn", "-vf", strings.Join(filters, ","), "-frames:v", strconv.Itoa(frames), "-c:v", "libx264", "-preset", "veryfast", "-crf", "18", "-pix_fmt", "yuv420p", "-threads", "2", "-filter_threads", "1", "-movflags", "+faststart", target)
	return err
}
func renderVideoRange(source, target string, start, end float64) (ProjectDocument, error) {
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		return nil, fmt.Errorf("choose a new export filename")
	}
	if !finite(start) || !finite(end) || start < 0 || end <= start {
		return nil, fmt.Errorf("choose a valid source range")
	}
	if info, handled, err := nativeRange(source, target, start, end); handled || err != nil {
		return info, err
	}
	info, err := probeVideo(source)
	if err != nil {
		return nil, err
	}
	if end > number(info, "duration_seconds")+.001 {
		return nil, fmt.Errorf("range exceeds source duration")
	}
	if err = encodeRange(source, target, start, end, int(math.RoundToEven((end-start)*24)), nil, 120*time.Second); err != nil {
		os.Remove(target)
		return nil, err
	}
	info, err = verifySilent(target, end-start)
	if err != nil {
		os.Remove(target)
	}
	return info, err
}

type videoSegment struct {
	Path       string
	Start, End float64
}

func renderVideoReel(segments []videoSegment, target, aspect string) (ProjectDocument, error) {
	sizes := map[string][]int{"16:9": {1280, 720}, "9:16": {720, 1280}, "1:1": {1080, 1080}}
	size, ok := sizes[aspect]
	if !ok || len(segments) == 0 {
		return nil, fmt.Errorf("choose footage and a valid aspect")
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		return nil, fmt.Errorf("choose a new export filename")
	}
	if info, handled, err := nativeReel(segments, target, aspect); handled || err != nil {
		return info, err
	}
	tmp, err := os.MkdirTemp("", "sbd-reel-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	deadline := time.Now().Add(240 * time.Second)
	cumulative := 0.0
	used := 0
	files := []string{}
	metadata := map[string]ProjectDocument{}
	for i, s := range segments {
		info, ok := metadata[s.Path]
		if !ok {
			info, err = probeVideo(s.Path)
			if err != nil {
				return nil, err
			}
			metadata[s.Path] = info
		}
		if !finite(s.Start) || !finite(s.End) || s.Start < 0 || s.End <= s.Start || s.End > number(info, "duration_seconds")+.001 {
			return nil, fmt.Errorf("invalid source range")
		}
		cumulative += s.End - s.Start
		frames := int(math.RoundToEven(cumulative*24)) - used
		used += frames
		name := fmt.Sprintf("clip-%04d.mp4", i)
		if err = encodeRange(s.Path, filepath.Join(tmp, name), s.Start, s.End, frames, size, time.Until(deadline)); err != nil {
			return nil, err
		}
		files = append(files, "file '"+name+"'")
	}
	manifest := filepath.Join(tmp, "concat.txt")
	if err = os.WriteFile(manifest, []byte(strings.Join(files, "\n")+"\n"), 0600); err != nil {
		return nil, err
	}
	_, err = runMedia(time.Until(deadline), "ffmpeg", "-v", "error", "-nostdin", "-n", "-f", "concat", "-safe", "1", "-i", manifest, "-map", "0:v:0", "-an", "-c:v", "copy", "-movflags", "+faststart", target)
	if err != nil {
		os.Remove(target)
		return nil, err
	}
	info, err := verifySilent(target, cumulative)
	if err != nil {
		os.Remove(target)
	}
	return info, err
}
