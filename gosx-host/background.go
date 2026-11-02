package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func storageIdentity(path string) string {
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	digest := sha256.Sum256([]byte(filepath.Clean(path)))
	return hex.EncodeToString(digest[:])
}

// A matching health response identifies both the Go backend and its storage.
// Never reuse an unrelated listener, redirect, or workspace for another folder.
func backgroundReady(target, identity string) (bool, error) {
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	response, err := client.Get(target + "api/health")
	if err != nil {
		return false, nil
	}
	defer response.Body.Close()
	var health struct {
		App, Backend string
		Storage      string `json:"storage"`
	}
	if response.StatusCode != 200 || json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&health) != nil || health.App != "Storyboard Desk" || health.Backend != "go" || health.Storage != identity {
		return false, fmt.Errorf("the configured port belongs to another app or project directory; choose a different port")
	}
	return true, nil
}

func launchBackground(data string, initialize bool, port int, settings string, open bool) error {
	// Validate missing-volume, ownership and root-identity guards before writing
	// launch state. The child reacquires the same project ownership lock.
	store, err := OpenProjectStore(data, initialize)
	var path string
	if err != nil {
		// A concurrent background launch may own storage before its HTTP server
		// is ready. Join the launch lock below before deciding whether to reuse.
		if !strings.Contains(err.Error(), "another Storyboard Desk process owns this data directory") {
			return err
		}
		resolved, e := filepath.EvalSymlinks(data)
		if e != nil {
			return err
		}
		path, e = filepath.Abs(resolved)
		if e != nil {
			return err
		}
	} else {
		path = store.path
		store.Close()
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return err
	}
	defer root.Close()
	if info, e := root.Lstat("runtime"); e == nil && !info.IsDir() {
		return fmt.Errorf("runtime must be a real directory")
	} else if e != nil && !os.IsNotExist(e) {
		return e
	}
	if err = root.Mkdir("runtime", 0700); err != nil && !os.IsExist(err) {
		return err
	}
	for _, name := range []string{"runtime/launch.lock", "runtime/server.log"} {
		if info, e := root.Lstat(name); e == nil && !info.Mode().IsRegular() {
			return fmt.Errorf("launch state must use regular files")
		} else if e != nil && !os.IsNotExist(e) {
			return e
		}
	}
	lock, err := root.OpenFile("runtime/launch.lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	deadline := time.Now().Add(20 * time.Second)
	for lockStorage(lock) != nil {
		if time.Now().After(deadline) {
			return fmt.Errorf("Storyboard Desk is still starting; try again shortly")
		}
		time.Sleep(100 * time.Millisecond)
	}
	target := fmt.Sprintf("http://127.0.0.1:%d/", port)
	ready, err := backgroundReady(target, storageIdentity(path))
	if err != nil {
		return err
	}
	if ready {
		return reportBackground(target, "reused", 0, open)
	}
	// Recheck ownership after the startup decision; a foreground app or older
	// worker must still prevent a detached writer from being launched.
	store, err = OpenProjectStore(path, false)
	if err != nil {
		return err
	}
	store.Close()
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"-no-open", "-native=false", "-data-dir", path, "-port", fmt.Sprint(port)}
	if settings != "" {
		settings, err = filepath.Abs(settings)
		if err != nil {
			return err
		}
		args = append(args, "-settings", settings)
	}
	log, err := root.OpenFile("runtime/server.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer log.Close()
	child := exec.Command(executable, args...)
	child.Stdout, child.Stderr = log, log
	configureBackgroundCommand(child)
	if err = child.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-done:
			return fmt.Errorf("Storyboard Desk could not start; check runtime/server.log")
		default:
		}
		ready, err = backgroundReady(target, storageIdentity(path))
		if err != nil {
			break
		}
		if ready {
			return reportBackground(target, "started", child.Process.Pid, open)
		}
		time.Sleep(100 * time.Millisecond)
	}
	child.Process.Kill()
	<-done
	if err != nil {
		return err
	}
	return fmt.Errorf("Storyboard Desk did not become ready; check runtime/server.log")
}

func reportBackground(target, status string, pid int, open bool) error {
	if open {
		if err := openWorkspaceBrowser(target); err != nil {
			fmt.Fprintln(os.Stderr, "Open the returned loopback address in a browser.")
		}
	}
	return json.NewEncoder(os.Stdout).Encode(struct {
		App    string `json:"app"`
		URL    string `json:"url"`
		Status string `json:"status"`
		PID    int    `json:"pid,omitempty"`
	}{"Storyboard Desk", target, status, pid})
}
