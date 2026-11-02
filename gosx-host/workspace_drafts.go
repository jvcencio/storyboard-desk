package main

// Repeated forms only restore a rejected draft into its original entity.
func (w workspacePage) scoped(key, value string) workspacePage {
	if w.draft != nil && w.draft.Has(key) && w.draft.Get(key) != value {
		w.draft = nil
	}
	return w
}
