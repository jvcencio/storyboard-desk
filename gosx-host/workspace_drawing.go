package main

import (
	"fmt"
	"net/url"
	"strconv"

	"m31labs.dev/gosx"
)

func (h *workspaceHandler) drawingCommand(path, id, sid string, rev int, values url.Values) error {
	service, err := h.store.Drawing()
	if err != nil {
		return err
	}
	switch path {
	case "/draw":
		strength, err := strconv.ParseFloat(values.Get("strength"), 64)
		if err != nil {
			return err
		}
		_, err = service.Start(id, sid, rev, values.Get("mode"), values.Get("input_image"), strength)
		return err
	case "/draw-cancel":
		return service.Cancel(id, values.Get("job"))
	case "/draw-recover":
		return service.Recover(id, values.Get("job"))
	}
	return fmt.Errorf("unknown local drawing operation")
}
func (w workspacePage) localDrawing() gosx.Node {
	f := w.controls("/draw")
	f = append(f, w.selectField("/draw", "Local draft type", "mode", "text", []string{"text", "image"}), w.field("/draw", "Image change strength", "strength", "0.4", "number"))
	options := []any{attrs("name", "input_image"), el("option", attrs("value", ""), txt("No input · text layout"))}
	for _, r := range cloudImages(w.project) {
		if len(shotText(r, "id")) >= 7 && shotText(r, "id")[:7] == "poster-" {
			continue
		}
		options = append(options, el("option", attrs("value", shotText(r, "id")), txt(shotText(r, "label"))))
	}
	f = append(f, el("label", txt("Starting canvas for image variation"), el("select", options...)), button("Draw one local layout"))
	return cls("details", "paper", el("summary", txt("Draw locally · optional Mac integration")), el("p", txt("Requires Draw Things CLI, the configured local model and normal memory pressure verified by Mini Meter. A rough layout is a composition candidate; inspect and enhance it before approving a finished starting image.")), el("form", f...))
}
func (w workspacePage) localJobs() gosx.Node {
	if w.drawError != "" {
		return cls("div", "error", txt(w.drawError))
	}
	rows := []any{}
	for _, job := range w.drawJobs {
		f := w.controls("/draw-cancel")
		f = append(f, hidden("job", shotText(job, "id")), el("h3", txt("Local layout · "+shotText(job, "status"))), el("p", txt(shotText(job, "message"))))
		if containsField([]string{"queued", "running"}, shotText(job, "status")) {
			f = append(f, button("Cancel local draft"))
		}
		rows = append(rows, el("form", f...))
		if shotText(job, "result_file") != "" && shotText(job, "status") != "done" {
			f = w.controls("/draw-recover")
			f = append(f, hidden("job", shotText(job, "id")), button("Import saved local result"))
			rows = append(rows, el("form", f...))
		}
	}
	if len(rows) == 0 {
		return el("div")
	}
	return cls("section", "paper", el("h2", txt("Local drawing jobs")), link("Refresh job status", w.href("library", shotText(w.shot, "id")), "button secondary"), fragment(rows...))
}
