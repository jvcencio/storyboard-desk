package main

import "m31labs.dev/gosx"

func copyArea(id, title, value string) gosx.Node {
	return el("details", el("summary", txt(title)), el("textarea", attrs("id", id, "rows", "8", "readonly", "readonly"), txt(value)), el("button", attrs("type", "button", "data-copy", id), txt("Copy text")))
}
func (w workspacePage) prompts() gosx.Node {
	p := promptPackage(w.project, w.shot, w.promptProvider)
	return cls("section", "paper", el("h2", txt("Prompts for "+shotText(w.shot, "title"))), copyArea("prompt-composition", "Composition prompt", shotText(p, "composition")), copyArea("prompt-enhancement", "Finished-image prompt", shotText(p, "enhancement")), copyArea("prompt-animation", "Motion prompt", shotText(p, "animation")), el("p", attrs("class", "fine"), txt("Copying text makes no provider request. Approve a finished starting image before generating video.")))
}
