package main

import (
	"errors"
	"net/http"
	"net/url"

	"m31labs.dev/gosx/action"
)

func (h *workspaceHandler) coreAction(endpoint string) action.Handler {
	switch endpoint {
	case "/save":
		return h.saveShotAction
	case "/project":
		return h.saveProjectAction
	case "/insert":
		return h.insertShotAction
	case "/move":
		return h.moveShotAction
	case "/reference-note":
		return h.referenceNoteAction
	}
	return nil
}

func formTarget(values map[string]string) string {
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

func validationFailure(values map[string]string, fields map[string]string, message string) *action.ResultError {
	if fields["revision"] != "" {
		message = fields["revision"]
	}
	failure := action.Validation(message, fields, submittedValues(values))
	failure.Status = http.StatusBadRequest // Preserve existing form status codes.
	return failure
}

func formFailure(endpoint string, values map[string]string, err error) action.View {
	status := http.StatusBadRequest
	if errors.Is(err, ErrRevision) {
		status = http.StatusConflict
	}
	return action.View{Name: endpoint, Status: status, Result: action.Result{Message: err.Error(), Values: submittedValues(values)}}
}

func mutationFailure(endpoint string, values map[string]string, err error) *action.ResultError {
	failure := formFailure(endpoint, values, err)
	return &action.ResultError{Status: failure.Status, Result: failure.Result}
}

func (h *workspaceHandler) actionSaved(ctx *action.Context, project ProjectDocument, message string) {
	if h.onSave != nil {
		h.onSave(documentRevision(project))
	}
	ctx.SetResult(action.Result{OK: true, Message: message, Redirect: formTarget(ctx.FormData), Values: map[string]string{},
		Data: rawValue(struct {
			ProjectID string `json:"project_id"`
			ShotID    string `json:"shot_id"`
			Revision  int    `json:"revision"`
		}{shotText(project, "id"), ctx.FormData["shot"], documentRevision(project)}),
	})
	ctx.SetStatus(http.StatusOK)
}

// serveFormAction runs behind the existing loopback Host, Origin, body-cap and
// per-run CSRF checks. Both transports use POST values, never query overrides.
func (h *workspaceHandler) serveFormAction(rw http.ResponseWriter, r *http.Request, handler action.Handler) {
	serveFormAction(rw, r, handler, h.render)
}

func serveFormAction(rw http.ResponseWriter, r *http.Request, handler action.Handler, render func(http.ResponseWriter, *http.Request, int, action.View)) {
	values := formValues(r.PostForm)
	if action.WantsJSON(r) {
		action.ServeHandlerWithOptions(rw, r, func(ctx *action.Context) error {
			ctx.FormData = values
			return handler(ctx)
		}, action.ServeHandlerOptions{MaxBodyBytes: 2 << 20})
		return
	}
	ctx := &action.Context{Request: r, FormData: values}
	if err := handler(ctx); err != nil {
		var failure *action.ResultError
		if !errors.As(err, &failure) {
			http.Error(rw, "Could not save the changes", http.StatusInternalServerError)
			return
		}
		q := url.Values{"project": {values["project"]}, "shot": {values["shot"]}, "view": {values["view"]}, "scene": {values["scene_filter"]}}
		r.URL.RawQuery = q.Encode()
		render(rw, r, failure.StatusCode(), action.View{Name: r.URL.Path, Status: failure.StatusCode(), Result: failure.ActionResult()})
		return
	}
	http.Redirect(rw, r, formTarget(values), http.StatusSeeOther)
}
