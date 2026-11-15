// Package blockingflow is the blocking editor's preview and poster-capture
// lifecycle as one explicit state machine.
//
// The browser client reports events (an edit, a fetched preview, an animation
// frame) and performs the Effect each event returns. Every state the editor
// can be in is a value of Scene, Capture and the fetch fields below, so a
// combination such as "capture pending in Plan view" cannot be represented.
// The package has no browser dependency and is tested natively.
package blockingflow

// Mode is the stage view the preview renders.
type Mode string

const (
	Plan   Mode = "plan"
	Camera Mode = "camera"
)

// Scene is the lifecycle of the 3D surface.
type Scene int

const (
	Loading Scene = iota
	Ready
	Unavailable
)

// Capture is the lifecycle of one "Capture camera poster" request.
//
//	Idle ─click→ Requested ─preview sent→ Armed ─commands applied→ Framing ─frame→ Saving ─result→ Idle
//
// An edit, a view change, invalid fields or a failed preview return Requested,
// Armed and Framing to Idle. Saving always runs to its result, because the
// frame was already read from the saved scene.
type Capture int

const (
	CaptureIdle Capture = iota
	CaptureRequested
	CaptureArmed
	CaptureFraming
	CaptureSaving
)

// The status line texts. Browser tests wait for StatusReady and
// StatusPosterSaved, so change them together with those tests.
const (
	StatusPreparing     = "Preparing preview…"
	StatusReady         = "Preview ready"
	StatusUpdating      = "Updating preview…"
	StatusUnavailable   = "The 3D preview is unavailable. Positions can still be saved."
	StatusInvalid       = "Check the highlighted fields to update the preview."
	StatusPreviewFailed = "Preview could not update. Check the fields and try again."
	StatusNeedsCamera   = "Switch to Camera to capture its frame."
	StatusSaveFirst     = "Save blocking before capturing a poster."
	StatusSaving        = "Saving camera poster…"
	StatusPosterSaved   = "Camera poster saved"
	StatusPosterUnknown = "Poster save could not be confirmed. Reopen the shot to check its saved poster."
)

// Effect is what the client must do after an event. The zero value means
// "nothing".
type Effect struct {
	Status      string // replace the status line unless empty
	Debounce    bool   // restart the debounce timer, then call PreviewDue
	Fetch       bool   // request a preview of the current form, then call PreviewFetched
	Redo        bool   // drop the response and call PreviewDue again now
	Send        int    // dispatch the fetched commands to the scene under this revision
	Frame       bool   // request an animation frame, then call Framed
	CancelFrame bool   // cancel the requested animation frame
	Save        bool   // read the canvas and post the poster, then call PosterDone
}

// Flow holds the editor state. Create one per mounted editor with New.
type Flow struct {
	mode  Mode
	scene Scene

	version        int // bumps on every change the preview must show
	appliedVersion int // version of the last preview sent to the scene
	revision       int // number of command batches sent to the scene

	fetching     bool
	fetchVersion int // version of the request in flight

	capture      Capture
	armed        int // command revision an Armed capture waits for
	frameVersion int // version a Framing capture will read
}

func New(mode Mode) *Flow { return &Flow{mode: mode} }

func (f *Flow) Mode() Mode       { return f.mode }
func (f *Flow) Capture() Capture { return f.capture }

// SceneReady reports that the 3D surface can take commands. Edits made while
// it loaded are previewed now.
func (f *Flow) SceneReady() Effect {
	f.scene = Ready
	if f.version != f.appliedVersion {
		return Effect{Status: StatusUpdating, Debounce: true}
	}
	return Effect{Status: StatusReady}
}

// SceneUnavailable reports that the 3D surface did not start. Fields can still
// be edited and saved; a later SceneReady recovers.
func (f *Flow) SceneUnavailable() Effect {
	f.scene = Unavailable
	return Effect{Status: StatusUnavailable, CancelFrame: f.cancelCapture()}
}

// Changed reports an edit to the blocking form.
func (f *Flow) Changed() Effect {
	f.version++
	effect := Effect{CancelFrame: f.cancelCapture()}
	switch f.scene {
	case Ready:
		effect.Status, effect.Debounce = StatusUpdating, true
	case Loading:
		effect.Status = StatusPreparing
	default:
		effect.Status = StatusUnavailable
	}
	return effect
}

// ModeChanged reports a switch between Plan and Camera. It cancels a pending
// capture: the poster is the Camera frame and must never be read from Plan.
func (f *Flow) ModeChanged(mode Mode) Effect {
	f.mode = mode
	return f.Changed()
}

// CaptureClicked reports a click on "Capture camera poster". saved says the
// shot has a saved blocking scene; dirty says the form has unsaved edits.
func (f *Flow) CaptureClicked(saved, dirty bool) Effect {
	switch {
	case f.scene == Loading:
		return Effect{Status: StatusPreparing}
	case f.scene == Unavailable:
		return Effect{Status: StatusUnavailable}
	case f.mode != Camera:
		return Effect{Status: StatusNeedsCamera}
	case !saved || dirty:
		return Effect{Status: StatusSaveFirst}
	case f.capture == CaptureSaving:
		return Effect{Status: StatusSaving}
	}
	cancel := f.cancelCapture()
	// A fresh preview makes the scene draw once more, so the frame read below
	// is the one GoSX has just drawn.
	f.version++
	f.capture = CaptureRequested
	return Effect{Status: StatusUpdating, Debounce: true, CancelFrame: cancel}
}

// PreviewDue reports that the debounce elapsed. valid says the form passes
// its field constraints.
func (f *Flow) PreviewDue(valid bool) Effect {
	if f.scene != Ready || f.fetching || f.version == f.appliedVersion {
		return Effect{}
	}
	if !valid {
		f.cancelCapture()
		return Effect{Status: StatusInvalid}
	}
	f.fetching, f.fetchVersion = true, f.version
	return Effect{Status: StatusUpdating, Fetch: true}
}

// PreviewFetched reports the result of the request PreviewDue started.
func (f *Flow) PreviewFetched(ok bool) Effect {
	if !f.fetching {
		return Effect{}
	}
	f.fetching = false
	if f.fetchVersion != f.version {
		return Effect{Redo: true}
	}
	if !ok {
		f.cancelCapture()
		return Effect{Status: StatusPreviewFailed}
	}
	f.revision++
	f.appliedVersion = f.fetchVersion
	if f.capture == CaptureRequested {
		f.capture, f.armed = CaptureArmed, f.revision
	}
	return Effect{Send: f.revision}
}

// Applied reports that the scene applied the command batch with this revision.
func (f *Flow) Applied(revision int) Effect {
	if revision != f.revision || f.version != f.appliedVersion {
		return Effect{} // an older batch, or a newer preview is on its way
	}
	if f.capture == CaptureArmed && f.armed == revision {
		f.capture, f.frameVersion = CaptureFraming, f.version
		return Effect{Status: StatusReady, Frame: true}
	}
	return Effect{Status: StatusReady}
}

// Framed reports the animation frame a Framing capture asked for. dirty says
// the form has unsaved edits now.
func (f *Flow) Framed(dirty bool) Effect {
	if f.capture != CaptureFraming {
		return Effect{}
	}
	if dirty || f.mode != Camera || f.version != f.frameVersion {
		f.capture = CaptureIdle
		return Effect{Status: StatusSaveFirst}
	}
	f.capture = CaptureSaving
	return Effect{Status: StatusSaving, Save: true}
}

// PosterDone reports the result of the poster request. message is the
// server's reason when ok is false.
func (f *Flow) PosterDone(ok bool, message string) Effect {
	if f.capture != CaptureSaving {
		return Effect{}
	}
	f.capture = CaptureIdle
	if ok {
		return Effect{Status: StatusPosterSaved}
	}
	if message == "" {
		message = StatusPosterUnknown
	}
	return Effect{Status: message}
}

// cancelCapture returns a capture that has not read its frame yet to Idle and
// reports whether an animation frame must be cancelled.
func (f *Flow) cancelCapture() bool {
	switch f.capture {
	case CaptureRequested, CaptureArmed:
		f.capture = CaptureIdle
	case CaptureFraming:
		f.capture = CaptureIdle
		return true
	}
	return false
}
