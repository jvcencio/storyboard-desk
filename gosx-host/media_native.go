package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

func nativeMediaHelper(name string) (string, error) {
	backend := configuredMediaTools["backend"]
	if backend == "ffmpeg" {
		return "", nil
	}
	if runtime.GOOS == "darwin" {
		folders := []string{configuredMediaTools["app_root"]}
		if executable, err := os.Executable(); err == nil {
			folders = append(folders, filepath.Dir(executable))
		}
		for _, folder := range folders {
			if folder == "" {
				continue
			}
			path := filepath.Join(folder, name)
			if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Mode()&0111 != 0 {
				return path, nil
			}
		}
	}
	if backend == "native" {
		return "", fmt.Errorf("configured native media helpers are unavailable; build the Mac helpers or choose ffmpeg/auto")
	}
	return "", nil
}
func nativeProbe(path string) (ProjectDocument, bool, error) {
	helper, err := nativeMediaHelper("media_probe")
	if err != nil || helper == "" {
		return nil, false, err
	}
	tmp, err := os.MkdirTemp("", "sbd-probe-")
	if err != nil {
		return nil, true, err
	}
	defer os.RemoveAll(tmp)
	raw, err := runProgram(30*time.Second, helper, path, tmp)
	if err != nil {
		return nil, true, err
	}
	var info ProjectDocument
	if json.Unmarshal(raw, &info) != nil || number(info, "duration_seconds") <= 0 || !finite(number(info, "duration_seconds")) {
		return nil, true, fmt.Errorf("native video metadata is invalid")
	}
	delete(info, "poster")
	return info, true, nil
}
func nativePoster(source, target string) (bool, error) {
	helper, err := nativeMediaHelper("media_probe")
	if err != nil || helper == "" {
		return false, err
	}
	tmp, err := os.MkdirTemp("", "sbd-poster-")
	if err != nil {
		return true, err
	}
	defer os.RemoveAll(tmp)
	if _, err = runProgram(30*time.Second, helper, source, tmp); err != nil {
		return true, err
	}
	data, err := os.ReadFile(filepath.Join(tmp, "poster.jpg"))
	if err != nil {
		return true, err
	}
	f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return true, err
	}
	_, err = f.Write(data)
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	return true, err
}
func nativeRange(source, target string, start, end float64) (ProjectDocument, bool, error) {
	helper, err := nativeMediaHelper("export_range")
	if err != nil || helper == "" {
		return nil, false, err
	}
	raw, err := runProgram(120*time.Second, helper, source, target, strconv.FormatFloat(start, 'f', 9, 64), strconv.FormatFloat(end, 'f', 9, 64))
	if err != nil {
		os.Remove(target)
		return nil, true, err
	}
	var info ProjectDocument
	if json.Unmarshal(raw, &info) != nil || number(info, "audio_tracks") != 0 || math.Abs(number(info, "duration_seconds")-(end-start)) > .09 {
		os.Remove(target)
		return nil, true, fmt.Errorf("native range failed the silent-picture/duration check")
	}
	return info, true, nil
}
func nativeReel(segments []videoSegment, target, aspect string) (ProjectDocument, bool, error) {
	helper, err := nativeMediaHelper("export_reel")
	if err != nil || helper == "" {
		return nil, false, err
	}
	sizes := map[string][2]int{"16:9": {1280, 720}, "9:16": {720, 1280}, "1:1": {1080, 1080}}
	size, ok := sizes[aspect]
	if !ok {
		return nil, true, fmt.Errorf("invalid reel aspect")
	}
	tmp, err := os.MkdirTemp("", "sbd-native-reel-")
	if err != nil {
		return nil, true, err
	}
	defer os.RemoveAll(tmp)
	rows := []map[string]any{}
	duration := 0.0
	for _, s := range segments {
		if !finite(s.Start) || !finite(s.End) || s.Start < 0 || s.End <= s.Start {
			return nil, true, fmt.Errorf("invalid source range")
		}
		duration += s.End - s.Start
		rows = append(rows, map[string]any{"path": s.Path, "start": s.Start, "end": s.End})
	}
	request := filepath.Join(tmp, "request.json")
	data, err := json.Marshal(map[string]any{"segments": rows, "width": size[0], "height": size[1]})
	if err != nil {
		return nil, true, err
	}
	if err = os.WriteFile(request, data, 0600); err != nil {
		return nil, true, err
	}
	raw, err := runProgram(240*time.Second, helper, request, target)
	if err != nil {
		os.Remove(target)
		return nil, true, err
	}
	var info ProjectDocument
	if json.Unmarshal(raw, &info) != nil || number(info, "audio_tracks") != 0 || math.Abs(number(info, "duration_seconds")-duration) > .09 {
		os.Remove(target)
		return nil, true, fmt.Errorf("native reel failed the silent-picture/duration check")
	}
	return info, true, nil
}
