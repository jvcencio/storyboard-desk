package main

import (
	"fmt"

	"m31labs.dev/gosx"
)

type ShotEditorProps struct {
	Save, Insert FormView
	Values       map[string]string
	Base         string
	BlockingURL  string
}

func (w workspacePage) shotForm() gosx.Node {
	if w.shot == nil {
		return el("div")
	}
	values := make(map[string]string, len(shotFields)+1)
	for _, name := range shotFields {
		values[name] = shotText(w.shot, name)
	}
	values["duration"] = fmt.Sprint(number(w.shot, "duration"))
	save := w.form("/save")
	base := ""
	if w.shared {
		if save.Rejected != nil {
			base = save.Rejected.Values["shot_base"]
		} else {
			base = shotBriefVersion(w.shot)
		}
	}
	blockingURL := ""
	if !w.shared {
		blockingURL = w.href("blocking", shotText(w.shot, "id"))
	}
	return ShotEditor(ShotEditorProps{Save: save, Insert: w.form("/insert"), Values: values, Base: base, BlockingURL: blockingURL})
}

// ShotEditor renders a shot brief using explicit values and action state. It
// cannot load projects or mutate the store.
func ShotEditor(props ShotEditorProps) gosx.Node {
	form := props.Save
	children := form.controls()
	if props.Base != "" {
		children = append(children, hidden("shot_base", props.Base))
	}
	children = append(children, cls("p", "eyebrow", txt("THE SHOT BRIEF")), el("h2", txt(props.Values["title"])), button("Save shot brief"))
	for _, name := range shotFields {
		kind := "textarea"
		if name == "title" || name == "scene" {
			kind = "text"
		}
		var field gosx.Node
		switch name {
		case "story_status":
			field = form.selectField(fieldLabels[name], name, props.Values[name], []string{"", "proposed", "reported_existing", "existing_candidate"})
		case "blocking":
			field = form.selectField(fieldLabels[name], name, props.Values[name], []string{"", "selfie", "mirror", "terminal", "walkway", "sign", "reaction", "passenger", "pass", "help", "turbulence", "gate", "portal"})
		default:
			field = form.field(fieldLabels[name], name, props.Values[name], kind)
		}
		children = append(children, field)
		if name == "title" {
			children = append(children, form.field("Target duration (seconds)", "duration", props.Values["duration"], "number"))
		}
	}
	children = append(children, button("Save shot brief"))
	insert := props.Insert.controls()
	insert = append(insert, el("button", attrs("name", "position", "value", "before"), txt("＋ Insert before")), el("button", attrs("name", "position", "value", "after"), txt("＋ Insert after")))
	var update gosx.Node
	if props.Base != "" {
		update = cls("div", "room-shot-update", attrs("id", "room-shot-update", "hidden", "hidden", "role", "status"),
			el("p", txt("A teammate updated this shot. Your open brief is kept here.")),
			// This link usually names the page that is already open, where a
			// plain link would only scroll. data-reload makes the client load
			// the saved version.
			el("a", attrs("href", pageURL(form.Scope.ProjectID, form.Scope.View, form.Scope.ShotID, form.Scope.Scene)+"#shot-editor", "class", "button secondary", "data-reload", "true"), txt("Review latest shot")))
	}
	var blocking gosx.Node
	if props.BlockingURL != "" {
		blocking = link("Edit blocking", props.BlockingURL, "button secondary blocking-open")
	}
	return cls("aside", "paper shot-editor", attrs("id", "shot-editor"), update, blocking, el("form", children...), el("form", insert...))
}
