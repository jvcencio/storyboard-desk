package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"m31labs.dev/gosx/desktop"
)

func serveWorkspace(store *ProjectStore, port int, open, native bool, label string) error {
	if _, err := store.State(); err != nil {
		return err
	}
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return err
	}
	listener, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return err
	}
	defer listener.Close()
	target := "http://" + listener.Addr().String() + "/"
	fmt.Printf("%s: %s\n", label, target)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	server := &http.Server{Handler: &workspaceHandler{store: store, csrf: hex.EncodeToString(token)}, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	defer server.Shutdown(context.Background())
	if native {
		cache, err := os.UserCacheDir()
		if err != nil {
			return err
		}
		app, err := desktop.New(desktop.Options{Title: "Storyboard Desk · GoSX", AppID: "com.gosx.storyboarddesk", Version: "0.12.0", Width: 1440, Height: 960, URL: target, UserDataDir: filepath.Join(cache, "Storyboard Desk", "gosx-webview"), BackgroundColor: "#f4f3ed", NativeBridge: false})
		if err != nil {
			server.Close()
			return fmt.Errorf("GoSX native window unavailable: %w; use -browser for browser mode", err)
		}
		err = app.Run()
		stop()
		if err != nil {
			server.Shutdown(context.Background())
			return err
		}
	} else if open {
		if err := openWorkspaceBrowser(target); err != nil {
			fmt.Fprintln(os.Stderr, "Open the printed loopback address in a browser.")
		}
	}
	select {
	case <-ctx.Done():
		err = server.Shutdown(context.Background())
		serveErr := <-served
		if serveErr != http.ErrServerClosed && err == nil {
			err = serveErr
		}
	case err = <-served:
		stop()
		server.Shutdown(context.Background())
		if err == http.ErrServerClosed {
			err = nil
		}
	}
	return err
}
func openWorkspaceBrowser(target string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	case "darwin":
		command = exec.Command("open", target)
	default:
		if path, err := exec.LookPath("wslview"); err == nil {
			command = exec.Command(path, target)
		} else {
			command = exec.Command("xdg-open", target)
		}
	}
	command.Env = childEnvironment()
	if err := command.Start(); err != nil {
		return err
	}
	go command.Wait()
	return nil
}
func startWorkspace(data string, initialize bool, port int, open, native bool, config AppSettings) error {
	store, err := OpenProjectStore(data, initialize)
	if err != nil {
		return err
	}
	defer store.Close()
	store.settings = &config
	configuredMediaTools = map[string]string{"ffmpeg": config.FFmpeg, "ffprobe": config.FFprobe, "backend": config.MediaBackend, "app_root": config.AppDir}
	return serveWorkspace(store, port, open, native, "GoSX workspace")
}
