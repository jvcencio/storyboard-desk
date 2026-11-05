package main

import "m31labs.dev/gosx/action"

func downloadForm(action string) bool {
	return containsField([]string{"/export", "/claude", "/section-reel", "/section-export"}, action)
}

// Repeated forms only restore a rejected draft into its original entity.
func (w workspacePage) scoped(key, value string) workspacePage {
	if submitted, ok := w.submission.Result.Values[key]; ok && submitted != value {
		w.submission = action.View{}
	}
	return w
}
