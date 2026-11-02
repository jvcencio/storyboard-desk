package main

import "fmt"

func (h *workspaceHandler) loadPageFeatures(w *workspacePage) {
	if w.project == nil {
		return
	}
	id := shotText(w.project, "id")
	switch w.view {
	case "exports":
		files, err := h.store.Exports(id)
		if err != nil {
			w.message = err.Error()
		} else {
			w.exportFiles = files
		}
	case "generate":
		cloud, err := h.store.Cloud()
		if err != nil {
			w.cloudError = err.Error()
			return
		}
		w.cloudState, err = cloud.Status(id)
		if err != nil {
			w.cloudError = err.Error()
			return
		}
		if w.quoteID != "" && preparedJobID.MatchString(w.quoteID) {
			w.quote, err = h.store.auxJSON("", "cloud/quotes/"+w.quoteID+".json", nil)
			if err != nil {
				w.cloudError = err.Error()
			} else if w.quote != nil && shotText(w.quote, "project_id") != id {
				w.quote = nil
				w.cloudError = "Prepared request belongs to another project"
			}
		}
	case "library":
		d, err := h.store.Drawing()
		if err != nil {
			w.drawError = err.Error()
		} else {
			w.drawJobs = d.Status(id)
		}
	case "handoff":
		text, err := claudeHandoff(w.project, w.scene, shotText(w.shot, "id"), cacheAttachments(h.store.cachedReferences(w.project))...)
		if err != nil {
			w.handoffClaude = fmt.Sprint(err)
		} else {
			w.handoffClaude = text
		}
		w.handoffIntake = planningHandoff(w.project, w.scene, shotText(w.shot, "id"))
	}
}
