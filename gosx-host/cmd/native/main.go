// Build the optional macOS AVFoundation helpers without a scripting runtime.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func run() error {
	output := flag.String("output-dir", "..", "helper destination (normally the repository root)")
	flag.Parse()
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("native media helpers require macOS; use FFmpeg on Windows/Linux")
	}
	xcrun, err := exec.LookPath("xcrun")
	if err != nil {
		return fmt.Errorf("install Apple Command Line Tools: xcode-select --install")
	}
	sdk, err := exec.Command(xcrun, "--sdk", "macosx", "--show-sdk-path").Output()
	if err != nil {
		return fmt.Errorf("macOS SDK unavailable: %w", err)
	}
	arch := map[string]string{"amd64": "x86_64", "arm64": "arm64"}[runtime.GOARCH]
	if arch == "" {
		return fmt.Errorf("build on an Intel or Apple Silicon Mac")
	}
	module, err := os.Getwd()
	if err != nil {
		return err
	}
	if _, err = os.Stat(filepath.Join(module, "go.mod")); err != nil {
		return fmt.Errorf("run from gosx-host: go -C gosx-host run ./cmd/native")
	}
	destination, err := filepath.Abs(*output)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(destination, 0755); err != nil {
		return err
	}
	// Build all helpers before publishing any output. Temporary compiler output
	// uses the caller's shared TMPDIR, and published files replace atomically.
	tmp, err := os.MkdirTemp("", "sbd-native-build-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	helpers := []string{"media_probe", "export_range", "export_reel"}
	for _, name := range helpers {
		fmt.Println("Building", name)
		cmd := exec.Command(xcrun, "--sdk", "macosx", "swiftc", "-sdk", strings.TrimSpace(string(sdk)), "-target", arch+"-apple-macosx13.0", "-O", filepath.Join(filepath.Dir(module), name+".swift"), "-o", filepath.Join(tmp, name))
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		for _, entry := range os.Environ() {
			if !strings.HasPrefix(entry, "OPENROUTER_API_KEY=") {
				cmd.Env = append(cmd.Env, entry)
			}
		}
		if err = cmd.Run(); err != nil {
			return err
		}
	}
	for _, name := range helpers {
		if err = publishHelper(filepath.Join(tmp, name), filepath.Join(destination, name)); err != nil {
			return err
		}
	}
	fmt.Println("Native media helpers ready.")
	return nil
}

func publishHelper(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.CreateTemp(filepath.Dir(target), ".native-publish-")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Chmod(0755)
	}
	if err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(out.Name(), target)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
