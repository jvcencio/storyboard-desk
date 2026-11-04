package main

import (
	"errors"
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

func (command ShotEditCommand) patch() ProjectDocument {
	patch := make(ProjectDocument, len(command.Text)+1)
	for name, value := range command.Text {
		patch[name] = rawValue(value)
	}
	if command.Duration != nil {
		patch["duration"] = rawValue(*command.Duration)
	}
	return patch
}

func (h *workspaceHandler) saveShotAction(ctx *action.Context) error {
	return h.saveShot(ctx, false)
}

func (h *workspaceHandler) saveShot(ctx *action.Context, shared bool) error {
	command, fields := decodeShotEdit(ctx.FormData)
	if len(fields) > 0 {
		return validationFailure(ctx.FormData, fields, "Check the shot brief fields")
	}
	var project ProjectDocument
	var err error
	if shared {
		project, err = h.store.updateSharedShot(command.ProjectID, command.ShotID, command.Revision, ctx.FormData["shot_base"], command.patch())
		if errors.Is(err, ErrRevision) {
			failure := mutationFailure("/save", ctx.FormData, err)
			failure.Result.Message = "This shot changed since you opened it. Copy your draft and open the latest board before applying it"
			return failure
		}
	} else {
		project, err = h.store.UpdateShot(command.ProjectID, command.ShotID, command.Revision, command.patch())
	}
	if err != nil {
		return mutationFailure("/save", ctx.FormData, err)
	}
	h.actionSaved(ctx, project, "Shot brief saved")
	return nil
}
