package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

const clientAssetsURL = "/client/assets/"

func workspaceWASMURL(t testing.TB) string {
	t.Helper()
	manifest, err := editorManifest()
	if err != nil {
		t.Fatal(err)
	}
	return manifest.GoWASMURL(strings.TrimSuffix(clientAssetsURL, "/"), "workspace")
}

func TestWorkspaceClientAssetRepresentations(t *testing.T) {
	assetURL := workspaceWASMURL(t)
	file := "clientassets/generated/assets/" + strings.TrimPrefix(assetURL, clientAssetsURL)
	h := &workspaceHandler{}
	for _, variant := range []struct{ accept, encoding, suffix string }{
		{"", "", ""}, {"gzip", "gzip", ".gz"}, {"gzip, br", "br", ".br"},
	} {
		want, err := clientAssets.ReadFile(file + variant.suffix)
		if err != nil {
			t.Fatal(err)
		}
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			r := httptest.NewRequest(method, "http://127.0.0.1:4444"+assetURL, nil)
			r.Header.Set("Accept-Encoding", variant.accept)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 200 || w.Header().Get("Content-Encoding") != variant.encoding ||
				w.Header().Get("Content-Type") != "application/wasm" || w.Header().Get("Vary") != "Accept-Encoding" ||
				w.Header().Get("Content-Length") != strconv.Itoa(len(want)) {
				t.Fatalf("%s %q returned status %d and headers %v", method, variant.accept, w.Code, w.Header())
			}
			if method == http.MethodHead && w.Body.Len() != 0 || method == http.MethodGet && !bytes.Equal(w.Body.Bytes(), want) {
				t.Fatalf("%s %q returned an unexpected body", method, variant.accept)
			}
		}
	}
}

type benchmarkResponseWriter struct{ header http.Header }

func (w benchmarkResponseWriter) Header() http.Header         { return w.header }
func (w benchmarkResponseWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w benchmarkResponseWriter) WriteHeader(int)             {}

func BenchmarkWorkspaceClientAsset(b *testing.B) {
	url := "http://127.0.0.1:4444" + workspaceWASMURL(b)
	h := &workspaceHandler{}
	for _, scenario := range []struct{ name, method, encoding string }{
		{"brotli", http.MethodGet, "gzip, br"}, {"gzip", http.MethodGet, "gzip"},
		{"identity", http.MethodGet, ""}, {"head", http.MethodHead, "gzip, br"},
	} {
		b.Run(scenario.name, func(b *testing.B) {
			r := httptest.NewRequest(scenario.method, url, nil)
			r.Header.Set("Accept-Encoding", scenario.encoding)
			w := benchmarkResponseWriter{header: make(http.Header)}
			b.ReportAllocs()
			for b.Loop() {
				clear(w.header)
				h.ServeHTTP(w, r)
			}
		})
	}
}
