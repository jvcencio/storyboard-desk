//go:build browser

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// This opt-in probe measures navigation from an empty browser profile. CDP
// instrumentation lives here, separate from the shipped editor and loaders.
func TestColdWorkspaceStartup(t *testing.T) {
	report := os.Getenv("STORYBOARD_COLD_START_REPORT")
	if report == "" {
		t.Skip("set STORYBOARD_COLD_START_REPORT to collect a cold-start sample")
	}
	var wasmRate int64
	if rate := os.Getenv("STORYBOARD_COLD_START_WASM_BYTES_PER_SECOND"); rate != "" {
		var err error
		wasmRate, err = strconv.ParseInt(rate, 10, 64)
		if err != nil || wasmRate <= 0 {
			t.Fatal("STORYBOARD_COLD_START_WASM_BYTES_PER_SECOND must be a positive integer")
		}
	}
	store, id := workspaceBoardFixture(t)
	project, err := store.snapshot(id)
	if err != nil {
		t.Fatal(err)
	}
	var poster bytes.Buffer
	if err := png.Encode(&poster, image.NewRGBA(image.Rect(0, 0, 320, 180))); err != nil {
		t.Fatal(err)
	}
	for _, shot := range project.shots {
		rel, _ := boardPreview(shot)
		if rel == "" {
			continue
		}
		path := filepath.Join(store.path, id, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, poster.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}

	ctx, origin := editorBrowser(t, &workspaceHandler{store: store, csrf: "benchmark"})
	const instrumentation = `(() => {
		const samples = window.__storyboardBoot = { ready: 0, wasm: [], tasks: [] };
		window.__gosx_bootstrap_features = new Proxy(Object.create(null), {
			set(target, name, factory) {
				if (name === 'engines') samples.engineFeature = performance.now();
				return Reflect.set(target, name, factory);
			},
		});
		const fetch = window.fetch;
		window.fetch = function (input, ...options) {
			const response = fetch.call(this, input, ...options);
			if (typeof input !== 'string' || !input.includes('/client/assets/go-wasm/')) return response;
			const span = samples.wasmFetch = { start: performance.now() };
			return response.then(value => {
				span.headers = performance.now();
				return value;
			});
		};
		const timeCall = (name, fn) => function (...args) {
			const span = samples[name] = { start: performance.now() };
			try { return fn.apply(this, args); }
			finally { span.end = performance.now(); }
		};
		// Observe the pinned GoSX loader without changing the generated scripts.
		const define = Object.defineProperty;
		Object.defineProperty = function (target, name, descriptor) {
			if (target === window && name === '__gosx_standard_go_wasm_ctor') {
				Object.defineProperty = define;
				descriptor = { ...descriptor, value: new Proxy(descriptor.value, {
					construct(ctor, args, newTarget) {
						const go = Reflect.construct(ctor, args, newTarget);
						go.run = timeCall('goRun', go.run);
						return go;
					},
				}) };
			}
			return define(target, name, descriptor);
		};
		const registerName = '__gosx_register_go_wasm_engine_factory';
		define(window, registerName, {
			configurable: true,
			set(register) {
				define(window, registerName, {
					configurable: true, enumerable: true, writable: true,
					value(token, component, factory) {
						return register.call(this, token, component, timeCall('mount', factory));
					},
				});
			},
		});
		const observer = new MutationObserver(() => {
			if (document.body?.getAttribute('data-editor-ready') === 'true') {
				samples.ready = performance.now();
				observer.disconnect();
			}
		});
		observer.observe(document, { subtree: true, attributes: true, attributeFilter: ['data-editor-ready'] });
		new PerformanceObserver(list => {
			for (const task of list.getEntries()) samples.tasks.push({ start: task.startTime, duration: task.duration });
		}).observe({ type: 'longtask', buffered: true });
		for (const method of ['instantiateStreaming', 'instantiate']) {
			const original = WebAssembly[method];
			if (typeof original !== 'function') continue;
			WebAssembly[method] = function (...args) {
				const span = { method, start: performance.now() };
				samples.wasm.push(span);
				return original.apply(this, args).finally(() => { span.end = performance.now(); });
			};
		}
	})()`
	const collect = `(() => {
		const nav = performance.getEntriesByType('navigation')[0];
		return {
			...window.__storyboardBoot,
			cards: document.querySelectorAll('.shot-card').length,
			engineScripts: document.querySelectorAll('script[data-gosx-script="feature-engines"]').length,
			ttfb: nav.responseStart, domComplete: nav.domComplete, dcl: nav.domContentLoadedEventEnd,
			htmlBytes: nav.decodedBodySize,
			resources: performance.getEntriesByType('resource').filter(r => r.name.includes('/client/assets/')).map(r => ({
				name: new URL(r.name).pathname, start: r.startTime, end: r.responseEnd,
				transfer: r.transferSize, encoded: r.encodedBodySize, decoded: r.decodedBodySize,
			})),
		};
	})()`
	var sample struct {
		Ready, EngineFeature float64
		Cards, EngineScripts int
		GoRun, Mount         struct{ Start, End float64 }
	}
	var raw json.RawMessage
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		if wasmRate > 0 {
			if _, err := network.EmulateNetworkConditionsByRule(false, []*network.Conditions{{
				URLPattern: origin + "/client/assets/go-wasm/*", DownloadThroughput: float64(wasmRate), UploadThroughput: -1,
			}}).Do(ctx); err != nil {
				return err
			}
		}
		_, err := page.AddScriptToEvaluateOnNewDocument(instrumentation).Do(ctx)
		return err
	}), chromedp.Navigate(origin+pageURL(id, "board", "", "")), browserReady(),
		chromedp.Evaluate(collect, &raw),
		chromedp.SendKeys(`form[action="/save"] input[name="title"]`, " draft", chromedp.ByQuery),
		browserText("#save-status", "Unsaved changes")); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &sample); err != nil || sample.Cards != 240 || sample.Ready <= 0 ||
		sample.EngineScripts != 1 || sample.EngineFeature <= 0 || sample.GoRun.Start <= 0 || sample.Mount.Start <= 0 {
		t.Fatalf("incomplete cold-start measurement: %s (%v)", raw, err)
	}
	if err := os.WriteFile(report, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceGoClientOffscreenCard(t *testing.T) {
	store, id := workspaceBoardFixture(t)
	before, err := store.snapshot(id)
	if err != nil {
		t.Fatal(err)
	}
	ctx, origin := editorBrowser(t, &workspaceHandler{store: store, csrf: "browser-token"})
	const button = `.shot-card:last-child button[title="Move earlier"]`
	var offscreen bool
	if err := chromedp.Run(ctx, chromedp.Navigate(origin+pageURL(id, "board", "", "")), browserReady(),
		chromedp.Evaluate(`document.querySelector('.shot-card:last-child').getBoundingClientRect().top > innerHeight`, &offscreen)); err != nil {
		t.Fatal(err)
	}
	if !offscreen {
		t.Fatal("the final card should start offscreen")
	}
	if err := chromedp.Run(ctx, chromedp.Focus(button, chromedp.ByQuery), chromedp.Poll(`(() => {
		const button = document.querySelector('.shot-card:last-child button[title="Move earlier"]');
		const bounds = button.getBoundingClientRect();
		return document.activeElement === button && bounds.top >= 0 && bounds.bottom <= innerHeight;
	})()`, nil)); err != nil {
		t.Fatal(err)
	}
	response, err := chromedp.RunResponse(ctx, chromedp.KeyEvent(kb.Enter))
	if err != nil || response == nil || response.Status != 200 {
		t.Fatalf("keyboard reorder response: %+v (%v)", response, err)
	}
	after, err := store.snapshot(id)
	if err != nil {
		t.Fatal(err)
	}
	last := len(before.shots) - 1
	if len(after.shots) != len(before.shots) ||
		documentRevision(after.document) != documentRevision(before.document)+1 ||
		shotText(after.shots[last-1], "id") != shotText(before.shots[last], "id") ||
		shotText(after.shots[last], "id") != shotText(before.shots[last-1], "id") {
		t.Fatal("moving the offscreen card did not persist exactly one reorder")
	}
}
