package main

import (
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestActualMacNativeMediaHelpers(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS AVFoundation execution requires a Mac")
	}
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(root, "media_probe")); err != nil {
		if os.Getenv("STORYBOARD_REQUIRE_NATIVE") == "1" {
			t.Fatal("build native helpers before testing:", err)
		}
		t.Skip("optional native helpers have not been built")
	}
	previous := configuredMediaTools
	configuredMediaTools = map[string]string{"backend": "native", "app_root": root}
	t.Cleanup(func() { configuredMediaTools = previous })
	fixture := t.TempDir()
	cmd := exec.Command("xcrun", "swift", filepath.Join(root, "tests", "make_video_fixture.swift"), fixture)
	cmd.Env = childEnvironment()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("AVFoundation fixture: %s: %v", out, err)
	}
	source := filepath.Join(fixture, "test-1.mp4")
	before, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	info, used, err := nativeProbe(source)
	if err != nil || !used || number(info, "duration_seconds") < 1.9 {
		t.Fatal("native probe", info, used, err)
	}
	if used, err = nativePoster(source, filepath.Join(fixture, "poster.jpg")); err != nil || !used {
		t.Fatal("native poster", err)
	}
	info, used, err = nativeRange(source, filepath.Join(fixture, "range.mp4"), .25, 1.25)
	if err != nil || !used || number(info, "audio_tracks") != 0 {
		t.Fatal("native silent range", info, err)
	}
	info, used, err = nativeReel([]videoSegment{{Path: source, Start: .1, End: .6}, {Path: filepath.Join(fixture, "test-2.mp4"), Start: .2, End: 1.2}}, filepath.Join(fixture, "reel.mp4"), "16:9")
	if err != nil || !used || number(info, "audio_tracks") != 0 {
		t.Fatal("native silent reel", info, err)
	}
	after, err := os.ReadFile(source)
	if err != nil || sha256.Sum256(before) != sha256.Sum256(after) {
		t.Fatal("native processing changed source")
	}
}
