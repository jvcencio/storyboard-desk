// Build the GoSX workspace and optional native bundle without Python.
package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/jvcencio/storyboard-desk/gosx-host/internal/clientbuild"
)

const sdkVersion = "1.0.4314.35"
const sdkSHA = "90778deb1b77e36292828b4b5833c9255c27533f40e0ddd55891ae718944edc4"

func installLoader(folder, arch string) error {
	cache := filepath.Join(folder, "webview2-sdk.nupkg")
	data, err := os.ReadFile(cache)
	digest := sha256.Sum256(data)
	if err != nil || hex.EncodeToString(digest[:]) != sdkSHA {
		client := &http.Client{Timeout: 60 * time.Second}
		response, err := client.Get("https://api.nuget.org/v3-flatcontainer/microsoft.web.webview2/" + sdkVersion + "/microsoft.web.webview2." + sdkVersion + ".nupkg")
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			return fmt.Errorf("WebView2 SDK download HTTP %d", response.StatusCode)
		}
		data, err = io.ReadAll(io.LimitReader(response.Body, (32<<20)+1))
		if err != nil {
			return err
		}
		digest = sha256.Sum256(data)
		if len(data) > 32<<20 || hex.EncodeToString(digest[:]) != sdkSHA {
			return fmt.Errorf("WebView2 SDK checksum mismatch; loader was not installed")
		}
		if err = os.WriteFile(cache, data, 0600); err != nil {
			return err
		}
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	native := "x64"
	if arch == "arm64" {
		native = "arm64"
	}
	for source, target := range map[string]string{"build/native/" + native + "/WebView2Loader.dll": "WebView2Loader.dll", "LICENSE.txt": "WebView2-LICENSE.txt"} {
		found := false
		for _, f := range archive.File {
			if f.Name != source {
				continue
			}
			found = true
			stream, err := f.Open()
			if err != nil {
				return err
			}
			contents, err := io.ReadAll(io.LimitReader(stream, 8<<20))
			stream.Close()
			if err != nil {
				return err
			}
			if err = os.WriteFile(filepath.Join(folder, target), contents, 0644); err != nil {
				return err
			}
		}
		if !found {
			return fmt.Errorf("SDK is missing %s", source)
		}
	}
	return nil
}
func run() error {
	target := flag.String("target", runtime.GOOS+"-"+runtime.GOARCH, "linux/darwin/windows-amd64 or arm64")
	output := flag.String("output-dir", "../build/gosx", "output directory")
	flag.Parse()
	parts := strings.Split(*target, "-")
	if len(parts) != 2 || !contains([]string{"windows", "darwin", "linux"}, parts[0]) || !contains([]string{"amd64", "arm64"}, parts[1]) {
		return fmt.Errorf("choose a supported OS and amd64/arm64 target")
	}
	folder, err := filepath.Abs(*output)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(folder, 0755); err != nil {
		return err
	}
	name := "storyboard-desk-go"
	if parts[0] == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(folder, name)
	module, err := os.Getwd()
	if err != nil {
		return err
	}
	for {
		if _, err = os.Stat(filepath.Join(module, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(module)
		if parent == module {
			return fmt.Errorf("run the packager inside the gosx-host module")
		}
		module = parent
	}
	if err = clientbuild.Build(module); err != nil {
		return err
	}
	cmd := exec.Command("go", "build", "-trimpath", "-o", binary, ".")
	cmd.Dir = module
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	env := []string{}
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GOOS=") && !strings.HasPrefix(entry, "GOARCH=") && !strings.HasPrefix(entry, "CGO_ENABLED=") && !strings.HasPrefix(entry, "OPENROUTER_API_KEY=") {
			env = append(env, entry)
		}
	}
	cmd.Env = append(env, "GOOS="+parts[0], "GOARCH="+parts[1], "CGO_ENABLED=0")
	if err = cmd.Run(); err != nil {
		return err
	}
	switch parts[0] {
	case "windows":
		if err = installLoader(folder, parts[1]); err != nil {
			return err
		}
		launcher := "@echo off\r\n\"%~dp0storyboard-desk-go.exe\" %*\r\nif errorlevel 1 pause\r\n"
		if err = os.WriteFile(filepath.Join(folder, "Start Storyboard Desk.bat"), []byte(launcher), 0644); err != nil {
			return err
		}
	case "darwin":
		bundle := filepath.Join(folder, "Storyboard Desk.app", "Contents")
		macos := filepath.Join(bundle, "MacOS")
		if err = os.MkdirAll(macos, 0755); err != nil {
			return err
		}
		data, err := os.ReadFile(binary)
		if err != nil {
			return err
		}
		if err = os.WriteFile(filepath.Join(macos, "storyboard-desk-go"), data, 0755); err != nil {
			return err
		}
		if runtime.GOOS == "darwin" && runtime.GOARCH == parts[1] {
			for _, name := range []string{"media_probe", "export_range", "export_reel"} {
				data, e := os.ReadFile(filepath.Join(filepath.Dir(module), name))
				if os.IsNotExist(e) {
					continue
				}
				if e != nil {
					return e
				}
				if err = os.WriteFile(filepath.Join(macos, name), data, 0755); err != nil {
					return err
				}
			}
		}
		script := "#!/bin/sh\nexec \"$(dirname \"$0\")/storyboard-desk-go\" -native \"$@\"\n"
		if err = os.WriteFile(filepath.Join(macos, "start-workspace"), []byte(script), 0755); err != nil {
			return err
		}
		plist := `<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>com.gosx.storyboarddesk</string><key>CFBundleName</key><string>Storyboard Desk</string><key>CFBundleExecutable</key><string>start-workspace</string><key>CFBundlePackageType</key><string>APPL</string><key>CFBundleShortVersionString</key><string>0.12.0</string><key>LSMinimumSystemVersion</key><string>13.0</string><key>NSHighResolutionCapable</key><true/><key>NSAppTransportSecurity</key><dict><key>NSAllowsLocalNetworking</key><true/></dict></dict></plist>`
		if err = os.WriteFile(filepath.Join(bundle, "Info.plist"), []byte(plist), 0644); err != nil {
			return err
		}
	}
	fmt.Printf("Built %s\nStart with %s (Windows uses a native window; -browser selects browser mode).\n", binary, binary)
	return nil
}
func contains(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
