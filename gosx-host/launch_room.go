package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func startStoryboardRoom(data, projectID string, port int, origin *url.URL) error {
	if _, err := editorManifest(); err != nil {
		return err
	}
	store, err := OpenProjectStore(data, false)
	if err != nil {
		return err
	}
	defer store.Close()
	listener, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return err
	}
	defer listener.Close()
	if origin == nil {
		origin = &url.URL{Scheme: "http", Host: listener.Addr().String()}
	}
	room, err := newStoryboardRoom(store, projectID, origin)
	if err != nil {
		return err
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = room.hub.Close(ctx)
	}()
	server := &http.Server{Handler: room, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Printf("Storyboard room: %s/\nRoom code: %s\nListening on %s\n", origin, room.invite, listener.Addr())
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	select {
	case <-ctx.Done():
		return server.Shutdown(context.Background())
	case err = <-served:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
