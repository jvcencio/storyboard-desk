//go:build browser

package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

func TestWorkspaceGoClientBlocking(t *testing.T) {
	store := testStore(t)
	project, err := store.Create("Blocking study", "Two people meet on an empty platform.", "16:9")
	if err != nil {
		t.Fatal(err)
	}
	id := shotText(project, "id")
	sid := shotText(list(project, "shots")[0], "id")
	server := httptest.NewServer(&workspaceHandler{store: store, csrf: "blocking-test"})
	t.Cleanup(server.Close)
	origin := server.URL
	ctx := editorBrowserContext(t, chromedp.Flag("use-angle", "swiftshader"), chromedp.Flag("enable-unsafe-swiftshader", true))
	step := func(name string, actions ...chromedp.Action) {
		t.Helper()
		if err := chromedp.Run(ctx, actions...); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	// Set the viewport here, not only on the launcher: a harness that attaches
	// to a running browser ignores the window size, and the Plan click below
	// would then miss the canvas.
	step("open plan", chromedp.EmulateViewport(1440, 960), chromedp.Navigate(origin+pageURL(id, "blocking", sid, "")), browserReady(),
		chromedp.Poll(`document.querySelector('[data-blocking-editor]').dataset.blockingReady === 'true'`, nil))
	proof := os.Getenv("STORYBOARD_BLOCKING_PROOF_DIR")
	capture := func(name string) {
		if proof == "" {
			return
		}
		var data []byte
		step("capture "+name, chromedp.CaptureScreenshot(&data))
		if err := os.WriteFile(filepath.Join(proof, name+".png"), data, 0644); err != nil {
			t.Fatal(err)
		}
		var diagnostics string
		step("inspect "+name, chromedp.Evaluate(`JSON.stringify({scene:window.__gosx_scene3d_debug.inspect(),canvas:{width:document.querySelector('.blocking-surface canvas').width,height:document.querySelector('.blocking-surface canvas').height}})`, &diagnostics))
		if err := os.WriteFile(filepath.Join(proof, name+".json"), []byte(diagnostics), 0644); err != nil {
			t.Fatal(err)
		}
	}
	capture("plan")
	var planZ float64
	step("plan origin", chromedp.Evaluate(`window.__gosx_scene3d_debug.inspect().camera.z`, &planZ))
	if planZ != 0 {
		t.Fatal("Plan camera did not retain its zero coordinate", planZ)
	}
	step("place actor", chromedp.SetValue(`input[name="actor.0.x"]`, "-2.25", chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelector('input[name="actor.0.x"]').dispatchEvent(new Event('input',{bubbles:true}))`, nil),
		browserText("#save-status", "Unsaved changes"), browserText("#blocking-status", "Preview ready"))
	var target struct{ X, Y float64 }
	step("placement target", chromedp.Evaluate(`(() => {const b=document.querySelector('.blocking-surface canvas').getBoundingClientRect(); return {X:b.left+b.width*0.375,Y:b.top+b.height*0.65}})()`, &target))
	step("place from Plan", chromedp.MouseClickXY(target.X, target.Y), browserText("#blocking-status", "Preview ready"))
	response, err := chromedp.RunResponse(ctx, chromedp.Click(`form[action="/blocking/save"] button[type="submit"]`, chromedp.ByQuery))
	if err != nil || response.Status != 200 {
		t.Fatalf("save: %+v %v", response, err)
	}
	step("saved plan", browserReady(), chromedp.Poll(`document.querySelector('[data-blocking-editor]').dataset.blockingReady === 'true'`, nil))
	step("camera", chromedp.Click(`[data-blocking-view="camera"]`, chromedp.ByQuery), browserText("#blocking-status", "Preview ready"))
	capture("camera-before-poster")
	// The frame is captured through the production Go-WASM button and stored by
	// the actual project store. The test never injects image or renderer data.
	step("poster", chromedp.Click(`[data-blocking-capture]`, chromedp.ByQuery), browserText("#blocking-status", "Camera poster saved"))
	capture("camera")
	project, err = store.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	shot, _ := findShot(project, sid)
	spec, err := readBlockingScene(shot)
	if err != nil || spec.Actors[0].X != -2 || spec.Actors[0].Z != 1.5 {
		t.Fatal("saved actor position", spec, err)
	}
	poster := object(shot, "blocking_poster")
	if shotText(poster, "scene_version") != blockingVersion(shot, "16:9") {
		t.Fatal("poster did not bind the saved scene")
	}
	file, err := store.OpenMedia(id, shotText(poster, "path"))
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	var diagnostics string
	step("renderer evidence", chromedp.Evaluate(`JSON.stringify(window.__gosx_scene3d_debug.inspect())`, &diagnostics))
	t.Logf("renderer %s; poster %s", diagnostics, shotText(poster, "path"))
	step("narrow plan", chromedp.EmulateViewport(390, 844), chromedp.Click(`[data-blocking-view="plan"]`, chromedp.ByQuery), browserText("#blocking-status", "Preview ready"),
		chromedp.Poll(`window.__gosx_scene3d_debug.inspect().camera.kind === 'orthographic'`, nil),
		chromedp.Poll(`document.documentElement.scrollWidth <= innerWidth`, nil))
	capture("mobile")
	step("board preview", chromedp.Navigate(origin+pageURL(id, "board", sid, "")), browserReady(),
		chromedp.WaitVisible(`img[alt^="Blocking study for"]`, chromedp.ByQuery))
	var sceneRequests int
	step("ordinary board stays lean", chromedp.Evaluate(`performance.getEntriesByType('resource').filter(r => r.name.includes('scene3d')).length`, &sceneRequests))
	if sceneRequests != 0 {
		t.Fatal("ordinary board fetched the 3D runtime", sceneRequests)
	}
	ready := chromedp.Poll(`document.querySelector('[data-blocking-editor]').dataset.blockingReady === 'true'`, nil)
	step("reopen blocking", chromedp.EmulateViewport(1440, 960), chromedp.Navigate(origin+pageURL(id, "blocking", sid, "")), browserReady(), ready,
		chromedp.WaitVisible(`[data-blocking-study-offer] button`, chromedp.ByQuery))
	step("use poster as sketch", chromedp.Click(`[data-blocking-study-offer] button`, chromedp.ByQuery),
		chromedp.WaitVisible(`[data-blocking-study-note]`, chromedp.ByQuery), browserReady(), ready)
	project, _ = store.Load(id)
	shot, _ = findShot(project, sid)
	study := blockingStudy(shot, blockingStudyJob(object(shot, "blocking_poster")))
	if study == nil || shotText(shot, "selected_sketch") != shotText(study, "id") || shotText(study, "kind") != "sketch" {
		t.Fatal("the poster did not become the selected sketch take", jsonText(study))
	}
	if legend := blockingLegend(shot); !strings.Contains(legend, "the orange figure is Actor A") {
		t.Fatal("the prompt legend does not describe the study", legend)
	}
	step("add actor", chromedp.Click(`button[name="op"][value="add-actor"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`input[name="actor.2.x"]`, chromedp.ByQuery), browserReady(), ready)
	var offerHidden bool
	step("stale poster offer", chromedp.Evaluate(`document.querySelector('[data-blocking-study-offer]').hidden`, &offerHidden))
	project, _ = store.Load(id)
	shot, _ = findShot(project, sid)
	if spec, err = readBlockingScene(shot); err != nil || len(spec.Actors) != 3 || !offerHidden {
		t.Fatal("add actor", spec, err, offerHidden)
	}
	if _, state := blockingPoster(project, shot); state != blockingPosterStale {
		t.Fatal("a poster of two actors still counts as current for three", state)
	}
	// Pose the three figures and move the key light through the page's own
	// controls, then save.
	step("pose the figures", chromedp.Click(`[data-blocking-view="camera"]`, chromedp.ByQuery), browserText("#blocking-status", "Preview ready"),
		chromedp.Evaluate(`for (const [name, value] of [["actor.0.pose", "sit"], ["actor.0.arms", "crossed"], ["actor.1.pose", "walk"], ["actor.1.arms", "raise"],
				["actor.2.pose", "kneel"], ["actor.2.look", "left"], ["light", "key-right"]]) {
				const field = document.querySelector('[name="' + name + '"]');
				field.value = value;
				field.dispatchEvent(new Event('change', {bubbles: true}));
			}`, nil),
		browserText("#save-status", "Unsaved changes"), browserText("#blocking-status", "Preview ready"))
	capture("poses")
	step("poses in plan", chromedp.Click(`[data-blocking-view="plan"]`, chromedp.ByQuery), browserText("#blocking-status", "Preview ready"))
	capture("poses-plan")
	// The figures read the key light's shadow map through the Selena lighting
	// chunk. Without that chunk, or with a figure on its fallback material, the
	// picture still looks plausible, so check both.
	var shading struct {
		Lighting bool   `json:"lighting"`
		Fallback string `json:"fallback"`
	}
	step("shadow shading", chromedp.Evaluate(`(() => {
			const stage = document.querySelector('.blocking-surface');
			return {lighting: !!window.__gosx_scene3d_selena_lighting_api, fallback: stage.getAttribute('data-gosx-scene3d-render-mesh-material-fallback') || ''};
		})()`, &shading))
	if !shading.Lighting || (shading.Fallback != "" && shading.Fallback != "0") {
		t.Fatalf("the figures do not read the shadow map: lighting chunk loaded %v, material fallbacks %q", shading.Lighting, shading.Fallback)
	}
	response, err = chromedp.RunResponse(ctx, chromedp.Click(`form[action="/blocking/save"] button[type="submit"]`, chromedp.ByQuery))
	if err != nil || response.Status != 200 {
		t.Fatalf("save poses: %+v %v", response, err)
	}
	project, _ = store.Load(id)
	shot, _ = findShot(project, sid)
	if spec, err = readBlockingScene(shot); err != nil || spec.Actors[0].Pose != "sit" || spec.Actors[1].Arms != "raise" || spec.Actors[2].Look != "left" || spec.Light != "key-right" {
		t.Fatal("saved poses", spec, err)
	}
	t.Log("Plan and camera rendered with the zero Plan origin retained. Numeric editing and a real Plan pointer click persisted actor X=-2, Z=1.5. The actual camera PNG was saved, narrow layout fit, and the board loaded its cached poster without Scene3D requests. The poster became the selected sketch take through the page's own button, and Add actor saved a third actor and retired the poster.")
}
