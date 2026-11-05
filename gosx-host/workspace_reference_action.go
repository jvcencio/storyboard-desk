package main

import (
	"strconv"

	"m31labs.dev/gosx/action"
)

func (h *workspaceHandler) referenceNoteAction(ctx *action.Context) error {
	values := ctx.FormData
	fields := map[string]string{}
	revision, err := strconv.Atoi(values["revision"])
	if err != nil {
		fields["revision"] = "Reopen the project to get a valid revision."
	}
	operation := values["operation"]
	index := -1
	switch operation {
	case "add":
	case "update", "remove":
		rawIndex := values["index"]
		index, err = strconv.Atoi(rawIndex)
		if err != nil || index < 0 {
			fields["index"] = "Choose the reference note again."
			values["index"] = rawIndex // An absent index must not match any note.
		} else {
			values["index"] = strconv.Itoa(index)
		}
	default:
		fields["operation"] = "Choose a reference note action."
		values["operation"] = operation
	}
	if len(fields) > 0 {
		return validationFailure(values, fields, "Check the reference note action")
	}
	project, err := h.store.ReferenceNote(values["project"], revision, index, operation, values["title"], values["url"], values["notes"])
	if err != nil {
		return mutationFailure("/reference-note", values, err)
	}
	h.actionSaved(ctx, project, "Reference notes saved")
	return nil
}
