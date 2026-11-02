package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"
)

var providerPath = regexp.MustCompile(`^(auth/key|images(/models)?|videos(/models|/[A-Za-z0-9_-]{1,200}(/content)?)?)$`)
var remoteJobID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,200}$`)
var preparedJobID = regexp.MustCompile(`^[a-f0-9]{12}$`)
var imageModels = []string{"google/gemini-3-pro-image", "google/gemini-3.1-flash-image", "google/gemini-nano-banana-2.1"}
var videoModels = []string{"google/veo-3.1-fast", "google/veo-3.1-lite", "google/veo-3.1"}

type generationProvider interface {
	Ready() error
	Call(string, any, bool) (ProjectDocument, error)
	Download(string) ([]byte, error)
}
type openRouterProvider struct {
	http *http.Client
	key  func() (string, error)
}

func newOpenRouterProvider() *openRouterProvider {
	return &openRouterProvider{http: &http.Client{Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, key: generationKey}
}
func (p *openRouterProvider) Ready() error { _, err := p.key(); return err }
func (p *openRouterProvider) request(path string, body any, auth bool) ([]byte, error) {
	if !providerPath.MatchString(path) {
		return nil, fmt.Errorf("unsupported provider operation")
	}
	var input io.Reader
	method := "GET"
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		input = bytes.NewReader(data)
		method = "POST"
	}
	timeout := 60 * time.Second
	if path == "images" {
		timeout = 240 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, "https://openrouter.ai/api/v1/"+path, input)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "StoryboardDesk/GoSX")
	if auth {
		key, err := p.key()
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+key)
	}
	response, err := p.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("provider connection interrupted; the submission outcome may be unknown, so do not resubmit blindly")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("OpenRouter HTTP %d; check account access and model availability; no automatic resubmission", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxUpload+1))
	if err != nil {
		return nil, fmt.Errorf("provider result interrupted; reconcile the existing request")
	}
	if int64(len(raw)) > maxUpload {
		return nil, fmt.Errorf("provider result exceeds the local media limit")
	}
	return raw, nil
}
func (p *openRouterProvider) Call(path string, body any, auth bool) (ProjectDocument, error) {
	raw, err := p.request(path, body, auth)
	if err != nil {
		return nil, err
	}
	var doc ProjectDocument
	if json.Unmarshal(raw, &doc) != nil || doc == nil {
		return nil, fmt.Errorf("provider returned an unfamiliar response; reconcile the existing request")
	}
	return doc, nil
}
func (p *openRouterProvider) Download(id string) ([]byte, error) {
	if !remoteJobID.MatchString(id) {
		return nil, fmt.Errorf("invalid provider job ID")
	}
	return p.request("videos/"+id+"/content", nil, true)
}
