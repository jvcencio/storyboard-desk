// Storyboard Desk's native host and standalone GoSX project editor.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"m31labs.dev/gosx/desktop"
)

func validateURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.User != nil || u.Fragment != "" {
		return "", fmt.Errorf("use this app's http://127.0.0.1:PORT/ address")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 || (u.Path != "" && u.Path != "/") {
		return "", fmt.Errorf("use this app's loopback root URL with an explicit port")
	}
	u.Path = "/"
	return u.String(), nil
}

func checkService(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	u.Path = "/api/health"
	u.RawQuery = ""
	client := &http.Client{
		Timeout:       3 * time.Second,
		Transport:     &http.Transport{Proxy: nil},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	response, err := client.Get(u.String())
	if err != nil {
		return fmt.Errorf("start Storyboard Desk first: %w", err)
	}
	defer response.Body.Close()
	var health struct {
		App string `json:"app"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&health) != nil || health.App != "Storyboard Desk" {
		return fmt.Errorf("the local listener is not Storyboard Desk")
	}
	return nil
}

func run() error {
	raw := flag.String("url", "http://127.0.0.1:4492/", "existing local Storyboard Desk URL")
	check := flag.Bool("check", false, "validate the backend without opening a native window")
	board := flag.Bool("board-preview", false, "write a read-only GoSX storyboard preview to standard output")
	editor := flag.Bool("editor", false, "serve the standalone GoSX shot editor on loopback")
	dataDir := flag.String("data-dir", "", "project directory (or STORYBOARD_DATA_DIR)")
	initData := flag.Bool("init-data-dir", false, "allow creating a new project directory")
	workspace := flag.Bool("workspace", false, "explicitly select the GoSX workspace (the default launch mode)")
	native := flag.Bool("native", runtime.GOOS == "windows", "open a native window (default on Windows; opt in on Mac)")
	browser := flag.Bool("browser", false, "open the GoSX workspace in a browser")
	noOpen := flag.Bool("no-open", false, "print the workspace URL without opening a browser")
	port := flag.Int("port", -1, "workspace port (0 chooses an available loopback port)")
	settingsFile := flag.String("settings", "", "optional settings JSON file")
	askKey := flag.Bool("ask-openrouter-key", false, "read a hidden OpenRouter key for this process only")
	flag.Parse()
	explicit := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { explicit[f.Name] = true })
	legacyMode := explicit["url"] || *check || *board || *editor
	if *workspace && legacyMode {
		return fmt.Errorf("choose the workspace or an explicit host/preview/editor mode")
	}
	if *workspace || !legacyMode {
		if *browser && explicit["native"] && *native {
			return fmt.Errorf("choose -browser or -native")
		}
		var config AppSettings
		var err error
		if *settingsFile != "" {
			home, e := os.UserHomeDir()
			if e != nil {
				return e
			}
			path, e := filepath.Abs(*settingsFile)
			if e != nil {
				return e
			}
			config, err = loadAppSettings(path, home, runtime.GOOS, os.Getenv)
		} else {
			config, err = runtimeSettings()
		}
		if err != nil {
			return err
		}
		data := *dataDir
		initialize := *initData
		if data == "" {
			data = config.DataDir
			initialize = initialize || config.DefaultData
		}
		selectedPort := config.Port
		if *port >= 0 {
			selectedPort = *port
		}
		if selectedPort > 65535 || (explicit["port"] && *port < 0) {
			return fmt.Errorf("choose a loopback port from 0 to 65535")
		}
		runtimeGenerationKey = strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
		if *askKey {
			runtimeGenerationKey, err = askGenerationKey()
			if err != nil {
				return err
			}
		}
		os.Unsetenv("OPENROUTER_API_KEY")
		useNative := *native && !*browser && (!*noOpen || explicit["native"])
		return startWorkspace(data, initialize, selectedPort, !*noOpen, useNative, config)
	}
	if explicit["native"] || *browser || *noOpen || *askKey || *settingsFile != "" || *port != -1 {
		return fmt.Errorf("workspace launch options cannot be combined with host/preview/editor modes")
	}

	if *editor && *dataDir == "" {
		*dataDir = os.Getenv("STORYBOARD_DATA_DIR")
	}
	if *initData && *dataDir == "" {
		return fmt.Errorf("-init-data-dir requires -data-dir")
	}
	if *editor || (*board && *dataDir != "") {
		store, err := OpenProjectStore(*dataDir, *initData)
		if err != nil {
			return err
		}
		defer store.Close()
		if *editor {
			return serveEditor(store)
		}
		state, err := store.State()
		if err != nil {
			return err
		}
		fmt.Println(RenderBoard(state))
		return nil
	}
	if *dataDir != "" || *initData {
		return fmt.Errorf("-data-dir is used with -editor or -board-preview")
	}
	target, err := validateURL(*raw)
	if err != nil {
		return err
	}
	if err = checkService(target); err != nil {
		return err
	}
	if *board {
		client, err := NewServiceClient(target)
		if err != nil {
			return err
		}
		state, err := client.State()
		if err != nil {
			return err
		}
		fmt.Println(RenderBoard(state))
		return nil
	}
	if *check {
		fmt.Println("Storyboard Desk backend verified")
		return nil
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	app, err := desktop.New(desktop.Options{
		Title: "Storyboard Desk", AppID: "com.gosx.storyboarddesk", Version: "0.11.0",
		Width: 1440, Height: 960, URL: target,
		UserDataDir:     filepath.Join(cache, "Storyboard Desk", "webview"),
		BackgroundColor: "#161b1e",
		// SBD uses same-origin HTTP; no native filesystem or shell bridge is needed.
		NativeBridge: false,
	})
	if err != nil {
		return fmt.Errorf("GoSX desktop: %w (see docs/PORTABILITY.md; browser mode is available)", err)
	}
	return app.Run()
}

func serveEditor(service EditorService) error {
	return serveWorkspace(service, 0, false, false, "GoSX editor")
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
