package main

import "m31labs.dev/gosx"

func downloadForm(action string) bool {
	return containsField([]string{"/export", "/claude", "/section-reel", "/section-export"}, action)
}

// A rejected POST still contains unsaved user input after the full-page reload.
// Advertise it in the HTML so the Go client can restore its navigation guard.
func (w workspacePage) formAttrs(action string) gosx.AttrList {
	if downloadForm(action) {
		return attrs("method", "post", "action", action, "data-download-form", "true")
	}
	fields := []string{"method", "post", "action", action, "data-dirty", "true"}
	if w.action == action && w.draft != nil {
		fields = append(fields, "data-rejected-draft", "true")
	}
	return attrs(fields...)
}

// Repeated forms only restore a rejected draft into its original entity.
func (w workspacePage) scoped(key, value string) workspacePage {
	if w.draft != nil && w.draft.Has(key) && w.draft.Get(key) != value {
		w.draft = nil
	}
	return w
}
