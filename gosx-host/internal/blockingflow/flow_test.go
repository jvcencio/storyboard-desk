package blockingflow

import "testing"

// ready returns a flow whose scene has loaded, with a clean saved form.
func ready(t *testing.T, mode Mode) *Flow {
	t.Helper()
	flow := New(mode)
	if effect := flow.SceneReady(); effect.Status != StatusReady || effect.Debounce {
		t.Fatalf("first ready effect %+v", effect)
	}
	return flow
}

// applyPreview drives one debounce, fetch and applied round and returns the
// effect of the commands-applied event.
func applyPreview(t *testing.T, flow *Flow) Effect {
	t.Helper()
	if effect := flow.PreviewDue(true); !effect.Fetch {
		t.Fatalf("preview did not start: %+v", effect)
	}
	sent := flow.PreviewFetched(true)
	if sent.Send == 0 {
		t.Fatalf("preview was not sent: %+v", sent)
	}
	return flow.Applied(sent.Send)
}

func TestCaptureReadsTheCameraFrameAfterItsOwnPreview(t *testing.T) {
	flow := ready(t, Camera)
	if effect := flow.CaptureClicked(true, false); !effect.Debounce || flow.Capture() != CaptureRequested {
		t.Fatalf("capture was not requested: %+v", effect)
	}
	if effect := applyPreview(t, flow); !effect.Frame || flow.Capture() != CaptureFraming {
		t.Fatalf("applied preview did not ask for a frame: %+v", effect)
	}
	if effect := flow.Framed(false); !effect.Save || effect.Status != StatusSaving || flow.Capture() != CaptureSaving {
		t.Fatalf("frame did not save: %+v", effect)
	}
	if effect := flow.PosterDone(true, ""); effect.Status != StatusPosterSaved || flow.Capture() != CaptureIdle {
		t.Fatalf("poster result %+v", effect)
	}
}

// The defect this package exists for: a capture asked for in Camera must never
// read the canvas after the person switches to Plan.
func TestSwitchingToPlanCancelsAPendingCapture(t *testing.T) {
	for name, switchAt := range map[string]int{"before the preview": 0, "while the preview is in flight": 1, "after the preview is sent": 2, "while waiting for the frame": 3} {
		t.Run(name, func(t *testing.T) {
			flow := ready(t, Camera)
			flow.CaptureClicked(true, false)
			step := 0
			toPlan := func() {
				if step == switchAt {
					flow.ModeChanged(Plan)
				}
				step++
			}
			toPlan()
			flow.PreviewDue(true)
			toPlan()
			sent := flow.PreviewFetched(true)
			if sent.Redo {
				flow.PreviewDue(true)
				sent = flow.PreviewFetched(true)
			}
			toPlan()
			applied := flow.Applied(sent.Send)
			toPlan()
			if flow.Capture() != CaptureIdle {
				t.Fatalf("capture survived the switch to Plan: state %v, applied %+v", flow.Capture(), applied)
			}
			if effect := flow.Framed(false); effect.Save {
				t.Fatal("a Plan frame was saved as the camera poster")
			}
		})
	}
}

func TestAnEditCancelsACaptureAndSaysSo(t *testing.T) {
	flow := ready(t, Camera)
	flow.CaptureClicked(true, false)
	applied := applyPreview(t, flow)
	if !applied.Frame {
		t.Fatal("expected a frame request")
	}
	if effect := flow.Changed(); !effect.CancelFrame || flow.Capture() != CaptureIdle {
		t.Fatalf("edit left the capture pending: %+v", effect)
	}
	if effect := flow.Framed(true); effect.Save {
		t.Fatal("saved a poster of an unsaved scene")
	}
}

func TestADirtyFormAtFrameTimeCancelsWithAMessage(t *testing.T) {
	flow := ready(t, Camera)
	flow.CaptureClicked(true, false)
	applyPreview(t, flow)
	effect := flow.Framed(true)
	if effect.Save || effect.Status != StatusSaveFirst || flow.Capture() != CaptureIdle {
		t.Fatalf("dirty frame %+v state %v", effect, flow.Capture())
	}
}

func TestCaptureIsRefusedWithAReason(t *testing.T) {
	plan := ready(t, Plan)
	if effect := plan.CaptureClicked(true, false); effect.Status != StatusNeedsCamera || plan.Capture() != CaptureIdle {
		t.Fatalf("Plan capture %+v", effect)
	}
	camera := ready(t, Camera)
	if effect := camera.CaptureClicked(false, false); effect.Status != StatusSaveFirst {
		t.Fatalf("unsaved capture %+v", effect)
	}
	if effect := camera.CaptureClicked(true, true); effect.Status != StatusSaveFirst {
		t.Fatalf("dirty capture %+v", effect)
	}
	loading := New(Camera)
	if effect := loading.CaptureClicked(true, false); effect.Status != StatusPreparing || loading.Capture() != CaptureIdle {
		t.Fatalf("loading capture %+v", effect)
	}
}

func TestInvalidFieldsCancelARequestedCapture(t *testing.T) {
	flow := ready(t, Camera)
	flow.CaptureClicked(true, false)
	if effect := flow.PreviewDue(false); effect.Fetch || effect.Status != StatusInvalid || flow.Capture() != CaptureIdle {
		t.Fatalf("invalid preview %+v state %v", effect, flow.Capture())
	}
}

func TestAFailedPreviewCancelsTheCapture(t *testing.T) {
	flow := ready(t, Camera)
	flow.CaptureClicked(true, false)
	flow.PreviewDue(true)
	if effect := flow.PreviewFetched(false); effect.Send != 0 || effect.Status != StatusPreviewFailed || flow.Capture() != CaptureIdle {
		t.Fatalf("failed preview %+v", effect)
	}
	// The next edit previews again: a failure must not leave the flow busy.
	flow.Changed()
	if effect := flow.PreviewDue(true); !effect.Fetch {
		t.Fatalf("flow stayed busy after a failure: %+v", effect)
	}
}

func TestAStaleResponseIsDroppedAndFetchedAgain(t *testing.T) {
	flow := ready(t, Plan)
	flow.Changed()
	flow.PreviewDue(true)
	flow.Changed() // a newer edit arrives while the request is in flight
	if effect := flow.PreviewDue(true); effect.Fetch {
		t.Fatal("started a second request while one is in flight")
	}
	stale := flow.PreviewFetched(true)
	if stale.Send != 0 || !stale.Redo {
		t.Fatalf("stale response was applied: %+v", stale)
	}
	if effect := flow.PreviewDue(true); !effect.Fetch {
		t.Fatalf("no refetch after a stale response: %+v", effect)
	}
	if fresh := flow.PreviewFetched(true); fresh.Send != 1 || fresh.Redo {
		t.Fatalf("fresh response %+v", fresh)
	}
	// Nothing changed since, so the debounce that is still pending does nothing.
	if effect := flow.PreviewDue(true); effect.Fetch {
		t.Fatal("refetched an already applied version")
	}
}

func TestOnlyTheLatestAppliedBatchReportsReady(t *testing.T) {
	flow := ready(t, Plan)
	flow.Changed()
	flow.PreviewDue(true)
	first := flow.PreviewFetched(true)
	flow.Changed()
	flow.PreviewDue(true)
	second := flow.PreviewFetched(true)
	if effect := flow.Applied(first.Send); effect.Status != "" {
		t.Fatalf("an old batch reported ready: %+v", effect)
	}
	if effect := flow.Applied(second.Send); effect.Status != StatusReady {
		t.Fatalf("the latest batch did not report ready: %+v", effect)
	}
}

func TestEditsWhileLoadingPreviewOnceTheSceneIsReady(t *testing.T) {
	flow := New(Plan)
	if effect := flow.Changed(); effect.Debounce || effect.Status != StatusPreparing {
		t.Fatalf("edit while loading %+v", effect)
	}
	if effect := flow.SceneReady(); !effect.Debounce {
		t.Fatalf("ready did not preview the edits made while loading: %+v", effect)
	}
}

func TestAnUnavailableSceneKeepsItsMessageAndCanRecover(t *testing.T) {
	flow := New(Camera)
	if effect := flow.SceneUnavailable(); effect.Status != StatusUnavailable {
		t.Fatalf("unavailable %+v", effect)
	}
	if effect := flow.Changed(); effect.Debounce || effect.Status != StatusUnavailable {
		t.Fatalf("edit replaced the unavailable message: %+v", effect)
	}
	if effect := flow.CaptureClicked(true, false); effect.Status != StatusUnavailable || flow.Capture() != CaptureIdle {
		t.Fatalf("capture without a scene %+v", effect)
	}
	if effect := flow.SceneReady(); !effect.Debounce || effect.Status != StatusUpdating {
		t.Fatalf("late ready %+v", effect)
	}
}

func TestASecondCaptureWhileSavingWaits(t *testing.T) {
	flow := ready(t, Camera)
	flow.CaptureClicked(true, false)
	applyPreview(t, flow)
	flow.Framed(false)
	if effect := flow.CaptureClicked(true, false); effect.Debounce || effect.Status != StatusSaving || flow.Capture() != CaptureSaving {
		t.Fatalf("second capture while saving %+v", effect)
	}
	if effect := flow.PosterDone(false, "The captured frame is blank."); effect.Status != "The captured frame is blank." || flow.Capture() != CaptureIdle {
		t.Fatalf("failed poster %+v", effect)
	}
}
