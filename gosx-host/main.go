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
		App     string `json:"app"`
		Backend string `json:"backend"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&health) != nil || (health.App != "Storyboard Desk" || health.Backend != "go") {
		return fmt.Errorf("the local listener is not Storyboard Desk")
	}
	return nil
}

func run() error {
	raw := flag.String("url", "http://127.0.0.1:4492/", "Go workspace URL for -check")
	check := flag.Bool("check", false, "check the running Go workspace without opening a window")
	board := flag.Bool("board-preview", false, "write a read-only GoSX storyboard preview")
	editor := flag.Bool("editor", false, "serve the GoSX workspace on an available port without opening it")
	dataDir := flag.String("data-dir", "", "project directory (or STORYBOARD_DATA_DIR)")
	initData := flag.Bool("init-data-dir", false, "allow creating a new project directory")
	flag.Bool("workspace", false, "select the GoSX workspace (the default)")
	native := flag.Bool("native", runtime.GOOS == "windows", "open a native window (default on Windows)")
	browser := flag.Bool("browser", false, "open the GoSX workspace in a browser")
	noOpen := flag.Bool("no-open", false, "print the address without opening a browser or window")
	background := flag.Bool("background", false, "start or reuse a detached Go workspace and print startup JSON")
	port := flag.Int("port", -1, "workspace port (0 chooses an available loopback port)")
	settingsFile := flag.String("settings", "", "optional settings JSON file")
	askKey := flag.Bool("ask-openrouter-key", false, "read a hidden OpenRouter key for this process only")
	shareProject := flag.String("share-project", "", "host an invite-only browser room for an existing project ID")
	shareOrigin := flag.String("share-origin", "", "room HTTPS origin when using a loopback reverse proxy")
	flag.Parse()
	explicit := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { explicit[f.Name] = true })
	if flag.NArg() != 0 {
		return fmt.Errorf("unexpected argument %q", flag.Arg(0))
	}
	var roomOrigin *url.URL
	if explicit["share-project"] {
		if !projectIDPattern.MatchString(*shareProject) {
			return fmt.Errorf("choose an existing project ID for -share-project")
		}
		if *check || *board || *editor || *background || *askKey || *initData || (explicit["native"] && *native) {
			return fmt.Errorf("-share-project starts a foreground browser room for an existing project")
		}
	}
	if explicit["share-origin"] {
		if *shareProject == "" {
			return fmt.Errorf("-share-origin requires -share-project")
		}
		var err error
		if roomOrigin, err = parseShareOrigin(*shareOrigin); err != nil {
			return err
		}
	}
	if *check {
		if *board || *editor || *background || *askKey || *dataDir != "" {
			return fmt.Errorf("-check uses only the running Go workspace URL")
		}
		target, err := validateURL(*raw)
		if err != nil {
			return err
		}
		if err = checkService(target); err != nil {
			return err
		}
		fmt.Println("Storyboard Desk Go workspace verified")
		return nil
	}
	if explicit["url"] {
		return fmt.Errorf("-url is used with -check; launch the Go workspace directly")
	}
	if *browser && explicit["native"] && *native {
		return fmt.Errorf("choose -browser or -native")
	}
	if *background && (*board || *editor || (explicit["native"] && *native)) {
		return fmt.Errorf("-background starts the browser workspace without a native window")
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
	data, initialize := *dataDir, *initData
	if data == "" {
		data = config.DataDir
		initialize = initialize || config.DefaultData
	}
	selectedPort := config.Port
	if explicit["port"] {
		selectedPort = *port
	}
	if selectedPort < 0 || selectedPort > 65535 {
		return fmt.Errorf("choose a loopback port from 0 to 65535")
	}
	if *shareProject != "" {
		return startStoryboardRoom(data, *shareProject, selectedPort, roomOrigin)
	}
	if *board {
		store, err := OpenProjectStore(data, initialize)
		if err != nil {
			return err
		}
		defer store.Close()
		state, err := store.State()
		if err != nil {
			return err
		}
		fmt.Println(RenderBoard(state))
		return nil
	}
	if *background {
		if selectedPort == 0 {
			return fmt.Errorf("-background requires a fixed port for safe reuse")
		}
		if *askKey {
			return fmt.Errorf("start a foreground session with -ask-openrouter-key; background sessions use the server environment")
		}
		return launchBackground(data, initialize, selectedPort, *settingsFile, !*noOpen)
	}
	runtimeGenerationKey = strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	if *askKey {
		runtimeGenerationKey, err = askGenerationKey()
		if err != nil {
			return err
		}
	}
	os.Unsetenv("OPENROUTER_API_KEY")
	if *editor {
		selectedPort = 0
		*noOpen = true
	}
	useNative := *native && !*browser && !*editor && (!*noOpen || explicit["native"])
	return startWorkspace(data, initialize, selectedPort, !*noOpen, useNative, config)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
