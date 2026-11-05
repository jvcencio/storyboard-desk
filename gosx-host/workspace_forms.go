package main

import (
	"net/url"
	"strconv"
	"strings"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/action"
)

// FormScope contains only the identity and navigation values a component needs.
// Storage and HTTP requests stay outside the component.
type FormScope struct {
	CSRF, ProjectID, ShotID, Revision, View, Scene string
}

type FormView struct {
	Scope    FormScope
	Action   string
	Rejected *action.Result
}

func formValues(values url.Values) map[string]string {
	if values == nil {
		return nil
	}
	result := make(map[string]string, len(values))
	for key := range values {
		result[key] = values.Get(key)
	}
	return result
}

func (w workspacePage) formScope() FormScope {
	return FormScope{
		CSRF: w.csrf, ProjectID: shotText(w.project, "id"), ShotID: shotText(w.shot, "id"),
		Revision: strconv.Itoa(documentRevision(w.project)), View: w.view, Scene: w.scene,
	}
}

func (w workspacePage) form(endpoint string) FormView {
	f := FormView{Action: endpoint, Scope: w.formScope()}
	if w.submission.Submitted() && w.submission.Name == endpoint {
		f.Rejected = &w.submission.Result
		// A deleted or unavailable entity must never turn a rejected draft into
		// an edit of whichever entity the page selected as a fallback.
		f.Scope.ProjectID = f.value("project", f.Scope.ProjectID)
		if endpoint == "/save" || endpoint == "/insert" {
			f.Scope.ShotID = f.value("shot", f.Scope.ShotID)
		}
	}
	return f
}

func (f FormView) value(name, fallback string) string {
	if f.Rejected != nil {
		if value, ok := f.Rejected.Values[name]; ok {
			return value
		}
	}
	return fallback
}

func (f FormView) attributes() gosx.AttrList {
	if downloadForm(f.Action) {
		return attrs("method", "post", "action", f.Action, "data-download-form", "true")
	}
	fields := []string{"method", "post", "action", f.Action, "data-dirty", "true"}
	if f.Rejected != nil {
		fields = append(fields, "data-rejected-draft", "true")
	}
	return attrs(fields...)
}

func (f FormView) controls() []any {
	return f.sharedControls().forShot(f.Scope.ShotID)
}

// The board can reuse the common nodes and insert each card's shot control.
type formControls struct {
	beforeShot [3]any
	afterShot  [3]any
}

func (f FormView) sharedControls() formControls {
	return formControls{
		beforeShot: [3]any{f.attributes(), hidden("csrf", f.Scope.CSRF), hidden("project", f.Scope.ProjectID)},
		afterShot: [3]any{hidden("revision", f.value("revision", f.Scope.Revision)),
			hidden("view", f.value("view", f.Scope.View)), hidden("scene_filter", f.value("scene_filter", f.Scope.Scene))},
	}
}

func (c formControls) forShot(shotID string, children ...any) []any {
	nodes := make([]any, 0, len(c.beforeShot)+1+len(c.afterShot)+len(children))
	nodes = append(nodes, c.beforeShot[:]...)
	nodes = append(nodes, hidden("shot", shotID))
	nodes = append(nodes, c.afterShot[:]...)
	return append(nodes, children...)
}

func (f FormView) errorID(name string) string {
	return strings.TrimPrefix(f.Action, "/") + "-" + f.Scope.ProjectID + "-" + f.Scope.ShotID + "-" + name + "-error"
}

func (f FormView) fieldError(name string) string {
	if f.Rejected == nil {
		return ""
	}
	return f.Rejected.FieldErrors[name]
}

func (f FormView) validationAttrs(name string) gosx.AttrList {
	if f.fieldError(name) != "" {
		return attrs("aria-invalid", "true", "aria-describedby", f.errorID(name))
	}
	return attrs()
}

func (f FormView) label(label, name string, input gosx.Node) gosx.Node {
	children := []any{txt(label), input}
	if message := f.fieldError(name); message != "" {
		children = append(children, el("span", attrs("id", f.errorID(name), "class", "field-error"), txt(message)))
	}
	return el("label", children...)
}

func (f FormView) field(label, name, value, kind string) gosx.Node {
	value = f.value(name, value)
	var input gosx.Node
	switch kind {
	case "textarea":
		limit := "12000"
		if name == "plan" || name == "template" {
			limit = "1048576"
		}
		input = el("textarea", attrs("name", name, "rows", "3", "maxlength", limit), f.validationAttrs(name), txt(value))
	case "checkbox":
		a := []any{attrs("type", "checkbox", "name", name, "value", "true"), f.validationAttrs(name)}
		if value == "true" {
			a = append(a, attrs("checked", "checked"))
		}
		input = el("input", a...)
	default:
		a := []string{"type", kind, "name", name, "value", value}
		if kind == "number" {
			a = append(a, "step", "0.01", "min", "0")
		}
		input = el("input", attrs(a...), f.validationAttrs(name))
	}
	return f.label(label, name, input)
}

func (f FormView) selectField(label, name, value string, options []string) gosx.Node {
	value = f.value(name, value)
	children := []any{attrs("name", name), f.validationAttrs(name)}
	found := false
	for _, opt := range options {
		a := []any{attrs("value", opt)}
		if opt == value {
			a = append(a, attrs("selected", "selected"))
			found = true
		}
		label := opt
		if label == "" {
			label = "Unspecified"
			if name == "scene" && f.Action == "" {
				label = "Whole episode"
			}
		}
		children = append(children, el("option", append(a, txt(label))...))
	}
	if !found && value != "" {
		children = append(children, el("option", attrs("value", value, "selected", "selected"), txt(value)))
	}
	return f.label(label, name, el("select", children...))
}

// Existing feature views share the same form contract while they migrate to
// explicit component props one feature at a time.
func (w workspacePage) controls(endpoint string) []any { return w.form(endpoint).controls() }
func (w workspacePage) formAttrs(endpoint string) gosx.AttrList {
	return w.form(endpoint).attributes()
}
func (w workspacePage) field(endpoint, label, name, value, kind string) gosx.Node {
	return w.form(endpoint).field(label, name, value, kind)
}
func (w workspacePage) selectField(endpoint, label, name, value string, options []string) gosx.Node {
	return w.form(endpoint).selectField(label, name, value, options)
}
