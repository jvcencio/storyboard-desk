package main

import (
	"fmt"
	"strconv"

	"m31labs.dev/gosx/action"
)

type ProjectEditCommand struct {
	ProjectID    string
	Revision     int
	Text         map[string]string
	Aspect       *string
	RuntimeLimit *float64
	MusicVideo   bool
}

func decodeProjectEdit(values map[string]string) (ProjectEditCommand, map[string]string) {
	command := ProjectEditCommand{ProjectID: values["project"], Text: map[string]string{}, MusicVideo: values["music_video"] == "true"}
	fields := map[string]string{}
	var err error
	command.Revision, err = strconv.Atoi(values["revision"])
	if err != nil {
		fields["revision"] = "Reopen the project to get a valid revision."
	}
	for _, name := range projectTextFields {
		if value, ok := values[name]; ok {
			if err := validateText(rawValue(value)); err != nil {
				fields[name] = err.Error()
			}
			command.Text[name] = value
		}
	}
	if value, ok := values["aspect"]; ok {
		if err := validateAspect(rawValue(value)); err != nil {
			fields["aspect"] = err.Error()
		}
		command.Aspect = &value
	}
	if value, ok := values["runtime_limit_seconds"]; ok {
		n, err := strconv.ParseFloat(value, 64)
		if err != nil || !finite(n) {
			fields["runtime_limit_seconds"] = "Enter a finite number between 0 and 3600."
		} else if err = validateNumber(rawValue(n), 0, 3600); err != nil {
			fields["runtime_limit_seconds"] = err.Error()
		} else {
			command.RuntimeLimit = &n
		}
	}
	return command, fields
}

func (command ProjectEditCommand) apply(store *ProjectStore) (ProjectDocument, error) {
	patch := ProjectDocument{"music_video": rawValue(command.MusicVideo)}
	for name, value := range command.Text {
		patch[name] = rawValue(value)
	}
	if command.Aspect != nil {
		patch["aspect"] = rawValue(*command.Aspect)
	}
	if command.RuntimeLimit != nil {
		patch["runtime_limit_seconds"] = rawValue(*command.RuntimeLimit)
	}
	return store.UpdateProject(command.ProjectID, command.Revision, patch)
}

func (h *workspaceHandler) saveProjectAction(ctx *action.Context) error {
	command, fields := decodeProjectEdit(ctx.FormData)
	// An unchecked HTML checkbox is absent from POST. Keep its intended false
	// value when rendering a rejection instead of falling back to saved true.
	ctx.FormData["music_video"] = strconv.FormatBool(command.MusicVideo)
	if len(fields) > 0 {
		return validationFailure(ctx.FormData, fields, "Check the project fields")
	}
	project, err := command.apply(h.store)
	if err != nil {
		return mutationFailure("/project", ctx.FormData, err)
	}
	h.actionSaved(ctx, project, "Project saved")
	return nil
}

type InsertShotCommand struct {
	ProjectID, ShotID, Position string
	Revision                    int
}

func (h *workspaceHandler) insertShotAction(ctx *action.Context) error {
	values := ctx.FormData
	revision, err := strconv.Atoi(values["revision"])
	if err != nil {
		return validationFailure(values, map[string]string{"revision": "Reopen the project to get a valid revision."}, "Invalid revision")
	}
	command := InsertShotCommand{values["project"], values["shot"], values["position"], revision}
	project, err := command.apply(h.store)
	if err != nil {
		return mutationFailure("/insert", values, err)
	}
	h.actionSaved(ctx, project, "Shot inserted")
	return nil
}

func (command InsertShotCommand) apply(store *ProjectStore) (ProjectDocument, error) {
	return store.Insert(command.ProjectID, command.ShotID, command.Position, command.Revision)
}

type MoveShotCommand struct {
	ProjectID, ShotID string
	Revision, Delta   int
}

func (command MoveShotCommand) apply(store *ProjectStore) (ProjectDocument, error) {
	if command.Delta != -1 && command.Delta != 1 {
		return nil, fmt.Errorf("choose earlier or later")
	}
	project, err := store.Load(command.ProjectID)
	if err != nil {
		return nil, err
	}
	shots, err := documentShots(project)
	if err != nil {
		return nil, err
	}
	order := make([]string, len(shots))
	index := -1
	for i, shot := range shots {
		order[i] = shotText(shot, "id")
		if order[i] == command.ShotID {
			index = i
		}
	}
	if index < 0 {
		return nil, ErrNotFound
	}
	target := index + command.Delta
	if target < 0 || target >= len(order) {
		return nil, fmt.Errorf("shot is already at the edge")
	}
	order[index], order[target] = order[target], order[index]
	// Reorder still checks the submitted revision under the store's write lock.
	return store.Reorder(command.ProjectID, command.Revision, order)
}

func (h *workspaceHandler) moveShotAction(ctx *action.Context) error {
	values := ctx.FormData
	revision, err := strconv.Atoi(values["revision"])
	if err != nil {
		return validationFailure(values, map[string]string{"revision": "Reopen the project to get a valid revision."}, "Invalid revision")
	}
	delta, _ := strconv.Atoi(values["delta"])
	command := MoveShotCommand{values["project"], values["shot"], revision, delta}
	project, err := command.apply(h.store)
	if err != nil {
		return mutationFailure("/move", values, err)
	}
	h.actionSaved(ctx, project, "Shot moved")
	return nil
}
