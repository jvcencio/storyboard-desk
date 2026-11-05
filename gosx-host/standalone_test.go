package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func standaloneBinary(t *testing.T) string {
	t.Helper()
	if path := os.Getenv("STORYBOARD_TEST_EXECUTABLE"); path != "" {
		absolute, err := filepath.Abs(path)
		if err != nil {
			t.Fatal(err)
		}
		return absolute
	}
	compiler, err := exec.LookPath("go")
	if err != nil {
		t.Fatal("set STORYBOARD_TEST_EXECUTABLE to test a prebuilt app:", err)
	}
	name := "storyboard-desk-go"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(t.TempDir(), name)
	cmd := exec.Command(compiler, "build", "-trimpath", "-o", binary, ".")
	cmd.Env = childEnvironment()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build app: %s: %v", out, err)
	}
	return binary
}

func withoutToolPath() []string {
	env := childEnvironment()
	for i, entry := range env {
		if strings.EqualFold(strings.SplitN(entry, "=", 2)[0], "PATH") {
			env[i] = "PATH="
		}
	}
	return env
}

func startStandalone(t *testing.T, binary, root, settings string) (string, func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	cmd := exec.CommandContext(ctx, binary, "-no-open", "-port", "0", "-data-dir", root, "-settings", settings)
	cmd.Env = withoutToolPath()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	stop := func() { cancel(); <-done }
	t.Cleanup(func() {
		if stop != nil {
			stop()
		}
	})
	line := make(chan string, 1)
	go func() { s, _ := bufio.NewReader(stdout).ReadString('\n'); line <- strings.TrimSpace(s) }()
	var target string
	select {
	case output := <-line:
		if !strings.HasPrefix(output, "GoSX workspace: ") {
			t.Fatalf("startup failed: %s", output)
		}
		target = strings.TrimPrefix(output, "GoSX workspace: ")
	case <-time.After(15 * time.Second):
		t.Fatal("startup timed out")
	}
	return target, func() {
		stop()
		stop = nil
		if stderr.Len() != 0 {
			t.Log(stderr.String())
		}
	}
}

func TestStandaloneLegacyProjectRoundTripWithoutToolRuntimes(t *testing.T) {
	binary := standaloneBinary(t)
	root, id := legacyFixtureCopy(t)
	settings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settings, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	target, stop := startStandalone(t, binary, root, settings)
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	response, err := client.Get(target + "?project=" + id + "&view=board")
	if err != nil {
		t.Fatal(err)
	}
	page, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || !bytes.Contains(page, []byte("Existing 雪 project")) {
		t.Fatalf("legacy project not visible: HTTP %d", response.StatusCode)
	}
	match := regexp.MustCompile(`name="csrf" value="([^"]+)"`).FindSubmatch(page)
	if len(match) != 2 {
		t.Fatal("missing CSRF control")
	}
	path := filepath.Join(root, id, "project.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var original ProjectDocument
	if err = json.Unmarshal(before, &original); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"csrf": {string(match[1])}, "project": {id}, "shot": {"first"}, "revision": {strconv.Itoa(documentRevision(original))}, "view": {"board"}, "title": {"GoSX <script> edit"}}
	post := func(action, origin string, status int) {
		t.Helper()
		request, err := http.NewRequest("POST", strings.TrimSuffix(target, "/")+action, strings.NewReader(form.Encode()))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Origin", origin)
		r, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		body, _ := io.ReadAll(r.Body)
		if r.StatusCode != status {
			t.Fatalf("%s: HTTP %d, want %d: %s", action, r.StatusCode, status, body)
		}
	}
	origin := strings.TrimSuffix(target, "/")
	post("/save", "https://unrelated.example", 403)
	form.Set("csrf", "wrong")
	post("/save", origin, 403)
	form.Set("csrf", string(match[1]))
	after, _ := os.ReadFile(path)
	if !bytes.Equal(after, before) {
		t.Fatal("rejected request changed project")
	}
	post("/save", origin, 303)
	post("/save", origin, 409)
	form.Set("revision", strconv.Itoa(documentRevision(original)+1))
	form.Set("position", "after")
	post("/insert", origin, 303)
	form.Set("title", "Created in Go")
	form.Set("aspect", "1:1")
	post("/create", origin, 303)
	stop()
	s, err := OpenProjectStore(root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	shots := list(p, "shots")
	if len(shots) != 3 || shotText(shots[0], "title") != "GoSX <script> edit" || shotText(shots[2], "id") != "second" || !imageApproved(p, shots[0]) {
		t.Fatal("legacy edit or insertion lost data")
	}
	for _, key := range []string{"future", "lyrics", "soundtrack", "soundtrack_history", "music_sections"} {
		var a, b any
		json.Unmarshal(p[key], &a)
		json.Unmarshal(original[key], &b)
		if jsonText(a) != jsonText(b) {
			t.Fatalf("legacy field changed: %s", key)
		}
	}
	if !bytes.Contains(p["future"], []byte("900719925474099312345")) {
		t.Fatal("large opaque integer changed")
	}
	projects, err := s.List()
	if err != nil || len(projects) != 2 {
		t.Fatal("Go project not readable after reopen", err)
	}
}

func TestStandaloneBackgroundStartupReusesOnlyMatchingGoStorage(t *testing.T) {
	binary := standaloneBinary(t)
	root := t.TempDir()
	settings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settings, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	launch := func(directory string, wantOK bool) map[string]any {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, "-background", "-no-open", "-data-dir", directory, "-port", fmt.Sprint(port), "-settings", settings)
		cmd.Env = withoutToolPath()
		out, err := cmd.CombinedOutput()
		if (err == nil) != wantOK {
			t.Fatalf("background: %s: %v", out, err)
		}
		var reply map[string]any
		if wantOK && json.Unmarshal(out, &reply) != nil {
			t.Fatalf("bad startup JSON: %s", out)
		}
		return reply
	}
	first := launch(root, true)
	if first["status"] != "started" || first["backend"] != nil || first["pid"] == nil {
		t.Fatalf("bad startup response: %v", first)
	}
	child, err := os.FindProcess(int(first["pid"].(float64)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { child.Kill(); child.Release() })
	second := launch(root, true)
	if second["status"] != "reused" || second["url"] != first["url"] {
		t.Fatal("background did not reuse matching storage")
	}
	launch(t.TempDir(), false)
}
