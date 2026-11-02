package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGoSettingsPreserveOverridesAndRejectCredentials(t *testing.T) {
	root := t.TempDir()
	local := filepath.Join(root, "settings.local.json")
	home := filepath.Join(root, "home")
	env := map[string]string{}
	lookup := func(k string) string { return env[k] }
	settings, err := loadAppSettings(local, home, "linux", lookup)
	if err != nil {
		t.Fatal(err)
	}
	if settings.DataDir != filepath.Join(home, ".local/share/storyboard-desk") || settings.Port != 4492 || !settings.DefaultData {
		t.Fatal("default locations changed")
	}
	os.WriteFile(local, []byte(`{"data_dir":"./projects","port":"4493","ffmpeg":"./tools/ffmpeg","model":"test.ckpt"}`), 0600)
	settings, err = loadAppSettings(local, home, "linux", lookup)
	if err != nil {
		t.Fatal(err)
	}
	if settings.DataDir != filepath.Join(root, "projects") || settings.FFmpeg != filepath.Join(root, "tools/ffmpeg") || settings.Port != 4493 || settings.DefaultData {
		t.Fatal("local overrides lost")
	}
	env["STORYBOARD_DATA_DIR"] = filepath.Join(root, "environment")
	env["STORYBOARD_PORT"] = "4494"
	settings, err = loadAppSettings(local, home, "linux", lookup)
	if err != nil || settings.Port != 4494 || settings.DataDir != env["STORYBOARD_DATA_DIR"] {
		t.Fatal("environment did not override local settings")
	}
	for _, bad := range []string{`{"OPENROUTER_API_KEY":"must-not-be-saved"}`, `{"port":true}`, `{"port":4492.5}`, `{"model":"../escape.ckpt"}`, `{"media_backend":"other"}`} {
		os.WriteFile(local, []byte(bad), 0600)
		delete(env, "STORYBOARD_PORT")
		if _, err = loadAppSettings(local, home, "linux", lookup); err == nil {
			t.Fatalf("accepted invalid settings %s", bad)
		}
	}
	if got := defaultDataDir(home, "darwin", lookup); got != filepath.Join(home, "Library/Application Support/Storyboard Desk") {
		t.Fatal("Mac location changed")
	}
	env["LOCALAPPDATA"] = filepath.Join(root, "local")
	if got := defaultDataDir(home, "windows", lookup); got != filepath.Join(env["LOCALAPPDATA"], "Storyboard Desk") {
		t.Fatal("Windows location changed")
	}
}
func TestChildProcessesDoNotInheritGenerationKey(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "synthetic-key-not-a-credential")
	for _, entry := range childEnvironment() {
		if len(entry) >= len("OPENROUTER_API_KEY=") && entry[:len("OPENROUTER_API_KEY=")] == "OPENROUTER_API_KEY=" {
			t.Fatal("generation key exposed to child process")
		}
	}
}
