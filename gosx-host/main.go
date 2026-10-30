// Storyboard Desk's optional native GoSX host. The Python service owns data.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
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
	editor := flag.Bool("editor", false, "serve the experimental GoSX shot editor on loopback")
	flag.Parse()
	target, err := validateURL(*raw)
	if err != nil {
		return err
	}
	if err = checkService(target); err != nil {
		return err
	}
	if *editor {
		client, err := NewServiceClient(target)
		if err != nil {
			return err
		}
		token := make([]byte, 32)
		if _, err := rand.Read(token); err != nil {
			return err
		}
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			return err
		}
		defer listener.Close()
		fmt.Printf("GoSX editor: http://%s/\n", listener.Addr())
		server := &http.Server{Handler: newEditorHandler(client, hex.EncodeToString(token)), ReadHeaderTimeout: 5 * time.Second}
		return server.Serve(listener)
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

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
