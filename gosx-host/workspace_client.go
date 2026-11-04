package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/island"
	"m31labs.dev/gosx/server"
)

//go:embed all:clientassets
var clientAssets embed.FS

var clientManifestOnce sync.Once
var clientManifest *buildmanifest.Manifest
var clientManifestError error

func editorManifest() (*buildmanifest.Manifest, error) {
	clientManifestOnce.Do(func() {
		data, err := clientAssets.ReadFile("clientassets/generated/build.json")
		if err != nil {
			clientManifestError = fmt.Errorf("build the Go editor runtime first: go -C gosx-host run ./cmd/client")
			return
		}
		var manifest buildmanifest.Manifest
		if err = json.Unmarshal(data, &manifest); err == nil {
			err = manifest.ValidateGoWASMAssets()
		}
		if err != nil {
			clientManifestError = err
			return
		}
		clientManifest = &manifest
	})
	return clientManifest, clientManifestError
}

func editorRuntime() (*island.Renderer, error) {
	manifest, err := editorManifest()
	if err != nil {
		return nil, err
	}
	renderer := island.NewRenderer("storyboard-workspace")
	if err = renderer.ApplyBuildManifest(manifest, "/client/assets"); err != nil {
		return nil, err
	}
	renderer.RenderEngine(engine.Config{Name: "Workspace", Kind: engine.KindWorker, Runtime: engine.RuntimeGoWASM, WASMPath: manifest.GoWASMURL("/client/assets", "workspace")}, txt(""))
	return renderer, nil
}

func editorDocument(renderer *island.Renderer, w workspacePage, r *http.Request, status int) gosx.Node {
	return workspaceDocument(renderer, r, status, w.render(), attrs("data-native-save", strconv.FormatBool(w.nativeSave)))
}

func workspaceDocument(renderer *island.Renderer, r *http.Request, status int, body gosx.Node, bodyAttrs gosx.AttrList) gosx.Node {
	summary := renderer.Summary()
	return server.HTMLDocument(&server.DocumentContext{
		Request: r, Pattern: "/", Path: r.URL.RequestURI(), PageID: "storyboard-workspace", Status: status, Title: "Storyboard Desk",
		Bootstrap: true, RuntimeActive: true,
		Runtime: server.PageRuntimeSummary{
			Bootstrap: summary.Bootstrap, Runtime: true, BootstrapMode: summary.BootstrapMode, Manifest: summary.Manifest,
			StandardGoWASMExecPath: summary.StandardGoWASMExecPath, BootstrapPath: summary.BootstrapPath,
			BootstrapFeatureEnginesPath: summary.BootstrapFeatureEnginesPath, Engines: summary.Engines,
		},
		Head: gosx.Fragment(el("link", attrs("rel", "stylesheet", "href", "/workspace.css")), renderer.PreloadHints(), renderer.PageHead()),
		Body: body, BodyAttrs: bodyAttrs,
	})
}

func (h *workspaceHandler) clientAsset(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/client/assets/")
	if !fs.ValidPath(name) || (path.Ext(name) != ".js" && path.Ext(name) != ".wasm") {
		http.NotFound(w, r)
		return
	}
	file := "clientassets/generated/assets/" + name
	asset, err := clientAssets.Open(file)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer asset.Close()
	contentType := "text/javascript; charset=utf-8"
	if path.Ext(name) == ".wasm" {
		contentType = "application/wasm"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Vary", "Accept-Encoding")
	for _, encoding := range []string{"br", "gzip"} {
		accepted := false
		for value := range strings.SplitSeq(r.Header.Get("Accept-Encoding"), ",") {
			if strings.TrimSpace(value) == encoding {
				accepted = true
			}
		}
		if accepted {
			suffix := ".gz"
			if encoding == "br" {
				suffix = ".br"
			}
			if compressed, e := clientAssets.Open(file + suffix); e == nil {
				defer compressed.Close()
				asset = compressed
				w.Header().Set("Content-Encoding", encoding)
				break
			}
		}
	}
	if info, err := asset.Stat(); err == nil {
		w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	}
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, asset)
	}
}
