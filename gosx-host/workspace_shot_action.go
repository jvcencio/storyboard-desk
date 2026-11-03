package main

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"m31labs.dev/gosx/action"
)

// ShotEditCommand is independent of HTTP and GoSX rendering. Missing fields
// remain absent, so older form clients can continue to submit partial edits.
type ShotEditCommand struct {
	ProjectID, ShotID string
	Revision          int
	Text              map[string]string
	Duration          *float64
}

func decodeShotEdit(values map[string]string) (ShotEditCommand, map[string]string) {
	command := ShotEditCommand{ProjectID: values["project"], ShotID: values["shot"], Text: map[string]string{}}
	fields := map[string]string{}
	var err error
	command.Revision, err = strconv.Atoi(values["revision"])
	if err != nil {
		fields["revision"] = "Reopen the project to get a valid revision."
	}
	for _, name := range shotFields {
		if value, ok := values[name]; ok {
			if err := validateText(rawValue(value)); err != nil {
				fields[name] = err.Error()
			}
			command.Text[name] = value
		}
	}
	if value, ok := values["duration"]; ok {
		n, err := strconv.ParseFloat(value, 64)
		if err != nil || !finite(n) {
			fields["duration"] = "Enter a finite number between 1 and 60."
		} else if err = validateNumber(rawValue(n), 1, 60); err != nil {
			fields["duration"] = err.Error()
		} else {
			command.Duration = &n
		}
	}
	return command, fields
}

func (command ShotEditCommand) apply(store *ProjectStore) (ProjectDocument, error) {
	patch := make(ProjectDocument, len(command.Text)+1)
	for name, value := range command.Text {
		patch[name] = rawValue(value)
	}
	if command.Duration != nil {
		patch["duration"] = rawValue(*command.Duration)
	}
	return store.UpdateShot(command.ProjectID, command.ShotID, command.Revision, patch)
}

func shotEditTarget(values map[string]string) string {
	if _, ok := values["view"]; !ok {
		return "/" // Original form clients used a root redirect.
	}
	view := values["view"]
	if view == "" {
		view = "board"
	}
	return pageURL(values["project"], view, values["shot"], values["scene_filter"])
}

func submittedValues(values map[string]string) map[string]string {
	copy := make(map[string]string, len(values))
	for name, value := range values {
		if name != "csrf" {
			copy[name] = value
		}
	}
	return copy
}

func (h *workspaceHandler) saveShotAction(ctx *action.Context) error {
	command, fields := decodeShotEdit(ctx.FormData)
	if len(fields) > 0 {
		message := "Check the shot brief fields"
		if fields["revision"] != "" {
			message = fields["revision"]
		}
		failure := action.Validation(message, fields, submittedValues(ctx.FormData))
		failure.Status = http.StatusBadRequest // Preserve the existing /save contract.
		return failure
	}
	project, err := command.apply(h.store)
	if err != nil {
		failure := formFailure("/save", ctx.FormData, err)
		return &action.ResultError{Status: failure.Status, Result: failure.Result}
	}
	ctx.SetResult(action.Result{OK: true, Message: "Shot brief saved", Redirect: shotEditTarget(ctx.FormData), Values: map[string]string{},
		Data: rawValue(struct {
			ProjectID string `json:"project_id"`
			ShotID    string `json:"shot_id"`
			Revision  int    `json:"revision"`
		}{command.ProjectID, command.ShotID, documentRevision(project)}),
	})
	ctx.SetStatus(http.StatusOK)
	return nil
}

func formFailure(endpoint string, values map[string]string, err error) action.View {
	status := http.StatusBadRequest
	if errors.Is(err, ErrRevision) {
		status = http.StatusConflict
	}
	return action.View{Name: endpoint, Status: status, Result: action.Result{Message: err.Error(), Values: submittedValues(values)}}
}

// saveShot is the transport adapter. The caller already checked loopback Host,
// Origin, the body cap and the per-run CSRF token. Both transports invoke the
// same GoSX action, using POST values only (never query-string overrides).
func (h *workspaceHandler) saveShot(rw http.ResponseWriter, r *http.Request) {
	values := formValues(r.PostForm)
	if action.WantsJSON(r) {
		action.ServeHandlerWithOptions(rw, r, func(ctx *action.Context) error {
			ctx.FormData = values
			return h.saveShotAction(ctx)
		}, action.ServeHandlerOptions{MaxBodyBytes: 2 << 20})
		return
	}
	ctx := &action.Context{Request: r, FormData: values}
	if err := h.saveShotAction(ctx); err != nil {
		var failure *action.ResultError
		if !errors.As(err, &failure) {
			http.Error(rw, "Could not save the shot brief", http.StatusInternalServerError)
			return
		}
		q := url.Values{"project": {values["project"]}, "shot": {values["shot"]}, "view": {values["view"]}, "scene": {values["scene_filter"]}}
		r.URL.RawQuery = q.Encode()
		h.render(rw, r, failure.StatusCode(), action.View{Name: "/save", Status: failure.StatusCode(), Result: failure.ActionResult()})
		return
	}
	http.Redirect(rw, r, shotEditTarget(values), http.StatusSeeOther)
}
