package main

import (
	"fmt"
	"strconv"

	"m31labs.dev/gosx"
)

type ProjectEditorProps struct {
	Form   FormView
	Values map[string]string
}

func (w workspacePage) direction() gosx.Node {
	values := make(map[string]string, len(projectTextFields)+3)
	for _, name := range projectTextFields {
		values[name] = shotText(w.project, name)
	}
	values["aspect"] = shotText(w.project, "aspect")
	values["runtime_limit_seconds"] = fmt.Sprint(number(w.project, "runtime_limit_seconds"))
	values["music_video"] = strconv.FormatBool(boolean(w.project, "music_video"))
	return ProjectEditor(ProjectEditorProps{Form: w.form("/project"), Values: values})
}

func ProjectEditor(props ProjectEditorProps) gosx.Node {
	form := props.Form
	children := form.controls()
	children = append(children, el("h2", txt("Brief & appearance")))
	for _, field := range [][2]string{{"title", "Project title"}, {"brief", "The idea"}, {"look", "Shared appearance"}, {"continuity", "Shared continuity"}, {"episode_notes", "Story commitments"}, {"lyrics", "Lyrics · exact original text"}} {
		kind := "textarea"
		if field[0] == "title" {
			kind = "text"
		}
		children = append(children, form.field(field[1], field[0], props.Values[field[0]], kind))
	}
	children = append(children, form.selectField("Format", "aspect", props.Values["aspect"], []string{"9:16", "16:9", "1:1"}), form.field("Episode budget in seconds", "runtime_limit_seconds", props.Values["runtime_limit_seconds"], "number"), form.field("Music video", "music_video", props.Values["music_video"], "checkbox"), button("Save project"))
	return cls("section", "paper", el("form", children...))
}
