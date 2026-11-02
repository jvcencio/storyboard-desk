package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type AppSettings struct {
	AppDir                                                                                   string
	DataDir, DrawCLI, ModelsDir, Model, Meter, ReferenceCache, FFmpeg, FFprobe, MediaBackend string
	Port                                                                                     int
	DefaultData                                                                              bool
}

var configuredMediaTools = map[string]string{}

func defaultDataDir(home, system string, env func(string) string) string {
	switch system {
	case "windows":
		base := env("LOCALAPPDATA")
		if base == "" {
			base = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(base, "Storyboard Desk")
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Storyboard Desk")
	default:
		base := env("XDG_DATA_HOME")
		if base == "" {
			base = filepath.Join(home, ".local", "share")
		}
		return filepath.Join(base, "storyboard-desk")
	}
}
func expandLocalPath(path, home, base string) (string, error) {
	if path == "~" {
		path = home
	} else if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~\\") {
		path = filepath.Join(home, path[2:])
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	return filepath.Abs(path)
}
func loadAppSettings(local, home, system string, env func(string) string) (AppSettings, error) {
	data := defaultDataDir(home, system, env)
	settings := AppSettings{AppDir: filepath.Dir(local), DataDir: data, DrawCLI: "/opt/homebrew/bin/draw-things-cli", ModelsDir: filepath.Join(data, "models"), Model: "juggernaut_xl_v9_lightning_q6p_q8p.ckpt", Meter: filepath.Join(home, "Applications", "Mini Meter.app", "Contents", "MacOS", "MiniMeter"), ReferenceCache: filepath.Join(filepath.Dir(local), "references"), FFmpeg: "ffmpeg", FFprobe: "ffprobe", MediaBackend: "auto", Port: 4492, DefaultData: true}
	names := map[string]string{"data_dir": "STORYBOARD_DATA_DIR", "port": "STORYBOARD_PORT", "draw_cli": "STORYBOARD_DRAW_CLI", "models_dir": "STORYBOARD_MODELS_DIR", "model": "STORYBOARD_DRAW_MODEL", "meter": "STORYBOARD_METER", "reference_cache": "STORYBOARD_REFERENCE_CACHE", "ffmpeg": "STORYBOARD_FFMPEG", "ffprobe": "STORYBOARD_FFPROBE", "media_backend": "STORYBOARD_MEDIA_BACKEND"}
	values := map[string]json.RawMessage{}
	if bytes, err := os.ReadFile(local); err == nil {
		if json.Unmarshal(bytes, &values) != nil || values == nil {
			return settings, fmt.Errorf("local settings must be a JSON object")
		}
		for key := range values {
			if _, ok := names[key]; !ok {
				return settings, fmt.Errorf("use documented settings only; credentials belong in the server environment")
			}
		}
	} else if !os.IsNotExist(err) {
		return settings, err
	}
	for key, name := range names {
		if v := env(name); v != "" {
			if key == "port" {
				values[key] = []byte(v)
			} else {
				values[key] = rawValue(v)
			}
		}
	}
	if _, ok := values["data_dir"]; ok {
		settings.DefaultData = false
	}
	if raw, ok := values["port"]; ok {
		var port int
		if json.Unmarshal(raw, &port) != nil {
			var text string
			if json.Unmarshal(raw, &text) != nil {
				return settings, fmt.Errorf("port must be an integer")
			}
			n, err := strconv.Atoi(text)
			if err != nil || strconv.Itoa(n) != text {
				return settings, fmt.Errorf("invalid port")
			}
			port = n
		}
		if port < 1 || port > 65535 {
			return settings, fmt.Errorf("port must be between 1 and 65535")
		}
		settings.Port = port
	}

	fields := map[string]*string{"data_dir": &settings.DataDir, "draw_cli": &settings.DrawCLI, "models_dir": &settings.ModelsDir, "model": &settings.Model, "meter": &settings.Meter, "reference_cache": &settings.ReferenceCache, "ffmpeg": &settings.FFmpeg, "ffprobe": &settings.FFprobe, "media_backend": &settings.MediaBackend}
	for key, target := range fields {
		if raw, ok := values[key]; ok {
			if nullJSON(raw) || json.Unmarshal(raw, target) != nil || strings.TrimSpace(*target) == "" {
				return settings, fmt.Errorf("choose a nonempty value for %s", key)
			}
		}
	}
	if filepath.Base(settings.Model) != settings.Model || strings.ContainsAny(settings.Model, "/\\:") {
		return settings, fmt.Errorf("choose a model filename inside the model directory")
	}
	if !containsField([]string{"auto", "ffmpeg", "native"}, settings.MediaBackend) {
		return settings, fmt.Errorf("choose auto, ffmpeg or native media backend")
	}
	for _, target := range []*string{&settings.DataDir, &settings.ModelsDir, &settings.Meter, &settings.ReferenceCache} {
		path, err := expandLocalPath(*target, home, filepath.Dir(local))
		if err != nil {
			return settings, err
		}
		*target = path
	}
	for _, target := range []*string{&settings.DrawCLI, &settings.FFmpeg, &settings.FFprobe} {
		if strings.ContainsAny(*target, "/\\") || strings.HasPrefix(*target, "~") {
			path, err := expandLocalPath(*target, home, filepath.Dir(local))
			if err != nil {
				return settings, err
			}
			*target = path
		}
	}
	return settings, nil
}
func runtimeSettings() (AppSettings, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return AppSettings{}, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return AppSettings{}, err
	}
	base := cwd
	if filepath.Base(cwd) == "gosx-host" {
		base = filepath.Dir(cwd)
	}
	if executable, err := os.Executable(); err == nil {
		folder := filepath.Dir(executable)
		if _, err = os.Stat(filepath.Join(folder, "settings.local.json")); err == nil {
			base = folder
		}
	}
	return loadAppSettings(filepath.Join(base, "settings.local.json"), home, runtime.GOOS, os.Getenv)
}
