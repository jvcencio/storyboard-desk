package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"
)

var runtimeGenerationKey string

func generationKey() (string, error) {
	if runtimeGenerationKey != "" {
		return runtimeGenerationKey, nil
	}
	if key := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY")); key != "" {
		return key, nil
	}
	if runtime.GOOS == "darwin" && os.Getenv("STORYBOARD_OPENROUTER_KEYCHAIN_SERVICE") != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "security", "find-generic-password", "-s", os.Getenv("STORYBOARD_OPENROUTER_KEYCHAIN_SERVICE"), "-w").Output()
		if err == nil && strings.TrimSpace(string(out)) != "" {
			return strings.TrimSpace(string(out)), nil
		}
	}
	return "", fmt.Errorf("start with -ask-openrouter-key or set OPENROUTER_API_KEY in the server environment")
}
func askGenerationKey() (string, error) {
	restore, err := hideConsoleInput()
	if err != nil {
		return "", fmt.Errorf("a terminal is required for hidden key entry; use OPENROUTER_API_KEY for other launch methods")
	}
	defer restore()
	fmt.Fprint(os.Stderr, "OpenRouter API key (hidden; not saved): ")
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	type result struct {
		key string
		err error
	}
	read := make(chan result, 1)
	go func() {
		key, err := bufio.NewReader(io.LimitReader(os.Stdin, 16385)).ReadString('\n')
		read <- result{key, err}
	}()
	var key string
	select {
	case value := <-read:
		key, err = value.key, value.err
	case <-signals:
		return "", fmt.Errorf("key entry cancelled")
	}

	fmt.Fprintln(os.Stderr)
	if err != nil || len(key) > 16384 || strings.TrimSpace(key) == "" {
		return "", fmt.Errorf("no valid key supplied")
	}
	return strings.TrimSpace(key), nil
}
func childEnvironment() []string {
	env := []string{}
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "OPENROUTER_API_KEY=") {
			env = append(env, entry)
		}
	}
	return env
}
