package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/island"
	"m31labs.dev/gosx/scene"
)

func (s *ProjectStore) saveBlocking(id, sid string, revision int, base string, spec *blockingScene, image []byte) (ProjectDocument, error) {
	return s.mutateChecked(id, func(project ProjectDocument) error {
		shot, err := findShot(project, sid)
		if err != nil {
			return err
		}
		if revision < 0 || revision > documentRevision(project) || blockingVersion(shot, shotText(project, "aspect")) != base {
			return ErrRevision
		}
		_, err = readBlockingScene(shot)
		return err
	}, func(project ProjectDocument) error {
		return editShotDocument(project, sid, func(shot ProjectDocument) error {
			if spec != nil {
				shot["blocking_scene"] = mergeBlockingScene(shot["blocking_scene"], *spec)
				if blockingVersion(shot, shotText(project, "aspect")) == base {
					return errNoProjectChange
				}
				return nil
			}
			if len(shot["blocking_scene"]) == 0 {
				return fmt.Errorf("Save the blocking before capturing a poster")
			}
			config, err := png.DecodeConfig(bytes.NewReader(image))
			if err != nil || config.Width < 128 || config.Height < 128 || config.Width > 1920 || config.Height > 1920 || config.Width*config.Height > 1920*1080 {
				return fmt.Errorf("The poster must be a PNG between 128 and 1920 pixels per side")
			}
			ratio := blockingAspect(shotText(project, "aspect"))
			if difference := float64(config.Width) - float64(config.Height)*ratio; difference < -3 || difference > 3 {
				return fmt.Errorf("Capture the Camera view in the project format")
			}
			frame, err := png.Decode(bytes.NewReader(image))
			if err != nil {
				return fmt.Errorf("The captured poster could not be read")
			}
			colours := map[uint32]bool{}
			for y := 0; y < config.Height; y += max(1, config.Height/40) {
				for x := 0; x < config.Width; x += max(1, config.Width/40) {
					r, g, b, a := frame.At(x, y).RGBA()
					if a > 0 {
						colours[(r>>10)<<12|(g>>10)<<6|b>>10] = true
					}
				}
			}
			if len(colours) < 3 {
				return fmt.Errorf("The captured frame is blank. Wait for the preview and try again")
			}
			digest := sha256.Sum256(image)
			hash := hex.EncodeToString(digest[:])
			poster := object(shot, "blocking_poster")
			if poster == nil {
				poster = ProjectDocument{}
			}
			if shotText(poster, "sha256") == hash && shotText(poster, "scene_version") == base {
				return errNoProjectChange
			}
			dir, err := projectDirectory(s.root, id)
			if err != nil {
				return err
			}
			defer dir.Close()
			rel := "media/" + sid + "/blocking/" + base[:16] + "-" + hash[:16] + ".png"
			if err = atomicStoredBytes(dir, rel, image); err != nil {
				return err
			}
			poster["path"], poster["sha256"], poster["scene_version"] = rawValue(rel), rawValue(hash), rawValue(base)
			poster["width"], poster["height"] = rawValue(config.Width), rawValue(config.Height)
			shot["blocking_poster"] = rawValue(poster)
			return nil
		})
	})
}

func (h *workspaceHandler) saveBlockingAction(ctx *action.Context) error {
	spec, fields := decodeBlockingFields(ctx.FormData)
	revision, err := strconv.Atoi(ctx.FormData["revision"])
	if err != nil {
		fields["revision"] = "Reopen the shot to get a valid revision."
	}
	if len(fields) > 0 {
		return validationFailure(ctx.FormData, fields, "Check the blocking fields")
	}
	message := "Blocking saved"
	if op := ctx.FormData["op"]; op != "" {
		// "Add actor" and "Remove" are submit buttons of this form, so the
		// edits already typed in the other fields are saved with them.
		if spec, message, err = applyBlockingOp(spec, op); err != nil {
			return mutationFailure("/blocking/save", ctx.FormData, err)
		}
	}
	project, err := h.store.saveBlocking(ctx.FormData["project"], ctx.FormData["shot"], revision, ctx.FormData["blocking_base"], &spec, nil)
	if err != nil {
		return mutationFailure("/blocking/save", ctx.FormData, err)
	}
	h.actionSaved(ctx, project, message)
	return nil
}

func applyBlockingOp(spec blockingScene, op string) (blockingScene, string, error) {
	if op == "add-actor" {
		spec, err := addBlockingActor(spec)
		return spec, "Actor added", err
	}
	if index, ok := strings.CutPrefix(op, "remove-actor."); ok {
		n, err := strconv.Atoi(index)
		if err != nil {
			return spec, "", fmt.Errorf("Choose an actor in this scene to remove")
		}
		spec, err := removeBlockingActor(spec, n)
		return spec, "Actor removed", err
	}
	return spec, "", fmt.Errorf("Unknown blocking action")
}

// useBlockingPoster adds the current camera poster to the shot as a sketch
// take, which makes it selectable as an image reference for generation. The
// take records the scene it shows and its actor legend, so the prompt can
// explain the figures even after the scene changes.
func (s *ProjectStore) useBlockingPoster(id, sid, base string) (ProjectDocument, error) {
	project, err := s.Load(id)
	if err != nil {
		return nil, err
	}
	shot, err := findShot(project, sid)
	if err != nil {
		return nil, err
	}
	poster, state := blockingPoster(project, shot)
	if state != blockingPosterCurrent {
		return nil, fmt.Errorf("Capture a camera poster of the saved blocking first")
	}
	if blockingVersion(shot, shotText(project, "aspect")) != base {
		return nil, ErrRevision
	}
	spec, err := readBlockingScene(shot)
	if err != nil {
		return nil, err
	}
	job := blockingStudyJob(poster)
	if job == "" {
		return nil, fmt.Errorf("Capture the camera poster again")
	}
	if existing := blockingStudy(shot, job); existing != nil {
		if shotText(existing, "binned_at") != "" {
			return nil, fmt.Errorf("This poster is already a sketch take in the bin. Restore it there")
		}
		return project, nil
	}
	file, err := s.OpenMedia(id, shotText(poster, "path"))
	if err != nil {
		return nil, fmt.Errorf("The saved poster is missing. Capture it again")
	}
	data, err := io.ReadAll(io.LimitReader(file, 1<<20+1))
	file.Close()
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != shotText(poster, "sha256") {
		return nil, fmt.Errorf("The saved poster changed on disk. Capture it again")
	}
	actors := []ProjectDocument{}
	for _, actor := range spec.Actors {
		actors = append(actors, ProjectDocument{"id": rawValue(actor.ID), "name": rawValue(actor.Name), "colour": rawValue(actor.Colour), "colour_name": rawValue(colourName(actor.Colour))})
	}
	study := ProjectDocument{"integration": rawValue("blocking"), "job_id": rawValue(job), "scene_version": rawValue(base),
		"poster_sha256": poster["sha256"], "actors": rawValue(actors), "camera": rawValue(spec.Camera)}
	// importMedia rejects a project that changed since Load, so the poster
	// checked above is the one that becomes the take.
	return s.importMedia(id, sid, "sketch", "blocking-study.png", int64(len(data)), bytes.NewReader(data), documentRevision(project), "", "", study)
}

func (h *workspaceHandler) useBlockingPosterAction(ctx *action.Context) error {
	project, err := h.store.useBlockingPoster(ctx.FormData["project"], ctx.FormData["shot"], ctx.FormData["blocking_base"])
	if err != nil {
		return mutationFailure("/blocking/sketch", ctx.FormData, err)
	}
	h.actionSaved(ctx, project, "Blocking sketch added")
	return nil
}

func (h *workspaceHandler) blockingRequest(w http.ResponseWriter, r *http.Request) {
	values := formValues(r.PostForm)
	project, err := h.store.Load(values["project"])
	if err != nil {
		http.Error(w, "Project unavailable", 404)
		return
	}
	shot, err := findShot(project, values["shot"])
	if err != nil {
		http.Error(w, "Shot unavailable", 404)
		return
	}
	if _, err = readBlockingScene(shot); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if r.URL.Path == "/blocking/preview" {
		spec, fields := decodeBlockingFields(values)
		prior, err := url.ParseQuery(values["previous"])
		before, previousFields := decodeBlockingFields(formValues(prior))
		if len(fields) > 0 || err != nil || len(previousFields) > 0 {
			http.Error(w, "Check the blocking fields", 400)
			return
		}
		previous, err := blockingRender(before, shotText(project, "aspect"), prior.Get("mode"))
		if err != nil {
			http.Error(w, "Material unavailable", 500)
			return
		}
		next, err := blockingRender(spec, shotText(project, "aspect"), values["mode"])
		if err != nil {
			http.Error(w, "Material unavailable", 500)
			return
		}
		commands := scene.DiffCommands(previous.SceneIR(), next.SceneIR())
		commands = append(commands, scene.Command{Kind: scene.CommandSetCamera, Data: blockingCameraPayload(next)})
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"commands": commands})
		return
	}
	if r.URL.Path != "/blocking/poster" {
		http.NotFound(w, r)
		return
	}
	image, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(values["image"], "data:image/png;base64,"))
	if err != nil || len(image) == 0 || len(image) > 1<<20 {
		http.Error(w, "The captured PNG is missing or too large", 400)
		return
	}
	revision, err := strconv.Atoi(values["revision"])
	if err != nil {
		http.Error(w, "Invalid revision", 400)
		return
	}
	project, err = h.store.saveBlocking(values["project"], values["shot"], revision, values["blocking_base"], nil, image)
	if err != nil {
		status := 400
		if err == ErrRevision {
			status = 409
		}
		http.Error(w, err.Error(), status)
		return
	}
	if h.onSave != nil {
		h.onSave(documentRevision(project))
	}
	shot, _ = findShot(project, values["shot"])
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"revision": documentRevision(project), "url": mediaURL(values["project"], shotText(object(shot, "blocking_poster"), "path"))})
}

func (w workspacePage) blockingEditor(renderer *island.Renderer) gosx.Node {
	if w.shot == nil {
		return cls("section", "paper", txt("Choose a shot to stage."))
	}
	spec, err := readBlockingScene(w.shot)
	if err != nil {
		return cls("section", "paper", el("h2", txt("Shot blocking")), el("p", attrs("role", "alert"), txt(err.Error())), link("Back to storyboard", w.href("board", shotText(w.shot, "id")), "button secondary"))
	}
	props, err := blockingRender(spec, shotText(w.project, "aspect"), "plan")
	if err != nil {
		return cls("section", "paper", txt("The blocking materials could not be prepared."))
	}
	config := props.EngineConfig()
	var initial ProjectDocument
	_ = json.Unmarshal(config.Props, &initial)
	initial["camera"] = rawValue(blockingCameraPayload(props))
	config.Props = rawValue(initial)
	config.MountAttrs["class"] = "blocking-surface"
	stage := renderer.RenderEngine(config, el("p", txt("Preparing the blocking preview…")))
	form := w.form("/blocking/save")
	fields := blockingFields(spec)
	controls := append(form.controls(), hidden("blocking_base", form.value("blocking_base", blockingVersion(w.shot, shotText(w.project, "aspect")))), hidden("actor_count", form.value("actor_count", fields["actor_count"])), button("Save blocking"))
	actors := []any{}
	selection := []any{el("legend", txt("Place an actor"))}
	for i, actor := range spec.Actors {
		prefix := fmt.Sprintf("actor.%d.", i)
		selected := attrs()
		if i == 0 {
			selected = attrs("checked", "checked")
		}
		selection = append(selection, cls("label", "blocking-actor-select", el("input", attrs("type", "radio", "name", "selected_actor", "value", strconv.Itoa(i)), selected), txt(actor.Name)))
		items := []any{hidden(prefix+"id", actor.ID)}
		for _, field := range [][3]string{{"name", "Name", "text"}, {"x", "Across · m", "number"}, {"z", "Depth · m", "number"}, {"heading", "Facing · °", "number"}, {"height", "Height · m", "number"}, {"colour", "Colour", "color"}} {
			items = append(items, blockingField(form, field[1], prefix+field[0], fields[prefix+field[0]], field[2]))
		}
		if len(spec.Actors) > 1 {
			items = append(items, el("button", attrs("type", "submit", "name", "op", "value", fmt.Sprintf("remove-actor.%d", i), "class", "button secondary blocking-remove", "aria-label", "Remove "+actor.Name), txt("Remove")))
		}
		actors = append(actors, cls("fieldset", "blocking-actor", el("legend", txt(actor.Name)), fragment(items...)))
	}
	if len(spec.Actors) < blockingMaxActors {
		actors = append(actors, cls("p", "blocking-add", el("button", attrs("type", "submit", "name", "op", "value", "add-actor", "class", "button secondary"), txt("Add actor"))))
	}
	camera := []any{el("legend", txt("Camera · 36 mm sensor"))}
	for _, field := range [][2]string{{"x", "Across · m"}, {"y", "Height · m"}, {"z", "Distance · m"}, {"pan", "Pan · °"}, {"tilt", "Tilt · °"}, {"lens", "Lens · mm"}} {
		camera = append(camera, blockingField(form, field[1], "camera."+field[0], fields["camera."+field[0]], "number"))
	}
	controls = append(controls, fragment(actors...), cls("fieldset", "blocking-camera", fragment(camera...)))
	sketch := w.form("/blocking/sketch")
	var posterNode, noteNode, offerNode gosx.Node
	poster, posterState := blockingPoster(w.project, w.shot)
	offered := false
	if posterState == blockingPosterCurrent {
		posterNode = el("img", attrs("src", mediaURL(shotText(w.project, "id"), shotText(poster, "path")), "alt", "Saved camera blocking study", "class", "blocking-poster"))
		if study := blockingStudy(w.shot, blockingStudyJob(poster)); study != nil && shotText(study, "binned_at") == "" {
			note := fmt.Sprintf("This poster is sketch take %g. ", number(study, "take"))
			if shotText(w.shot, "selected_sketch") == shotText(study, "id") {
				note += "It is the shot's selected composition sketch, so the image prompt explains its figures."
			} else {
				note += "It is not selected. Select it among the shot's takes to add the figure legend to the image prompt."
			}
			noteNode = el("p", attrs("class", "fine blocking-study", "data-blocking-study-note", "true"), txt(note))
		} else {
			offered = true
		}
	}
	if len(w.shot["blocking_scene"]) > 0 {
		// The offer is in the page whenever a poster can exist, so the client
		// can reveal it after a capture without a reload.
		offer := attrs("class", "blocking-study", "data-blocking-study-offer", "true")
		if !offered {
			offer = append(offer, attrs("hidden", "hidden")...)
		}
		offerNode = el("div", offer, el("form", append(sketch.controls(),
			hidden("blocking_base", blockingVersion(w.shot, shotText(w.project, "aspect"))),
			el("button", attrs("type", "submit", "class", "button secondary"), txt("Use poster as composition sketch")),
			el("p", attrs("class", "fine"), txt("Adds the poster as a sketch take that image generation can use as a layout reference.")))...))
	}
	var failure gosx.Node
	for _, rejected := range []*action.Result{form.Rejected, sketch.Rejected} {
		if rejected != nil {
			failure = el("p", attrs("role", "alert", "class", "error"), txt(rejected.Message))
		}
	}
	return cls("section", "blocking-editor", attrs("data-blocking-editor", "true", "data-mode", "plan", "data-aspect", shotText(w.project, "aspect"), "data-saved", strconv.FormatBool(len(w.shot["blocking_scene"]) > 0)),
		failure,
		cls("header", "blocking-heading", el("div", cls("p", "eyebrow", txt("SHOT BLOCKING")), el("h2", txt(shotText(w.shot, "title")))), link("Back to storyboard", w.href("board", shotText(w.shot, "id")), "button secondary")),
		cls("div", "blocking-layout", el("div", cls("div", "blocking-toolbar", el("div", attrs("role", "group", "aria-label", "Stage view"), el("button", attrs("type", "button", "data-blocking-view", "plan", "aria-pressed", "true"), txt("Plan")), el("button", attrs("type", "button", "data-blocking-view", "camera", "aria-pressed", "false"), txt("Camera"))), el("button", attrs("type", "button", "data-blocking-capture", "true"), txt("Capture camera poster"))),
			cls("div", "blocking-viewport", stage), el("p", attrs("class", "fine", "id", "blocking-help"), txt("Select an actor, then click or drag in Plan to place them. Positions are in metres.")),
			el("p", attrs("role", "status", "id", "blocking-status"), txt("Preparing preview…")), posterNode, noteNode, offerNode), cls("aside", "paper blocking-inspector", el("fieldset", selection...), el("form", controls...))))
}

func blockingField(form FormView, label, name, value, kind string) gosx.Node {
	if kind != "number" {
		return form.field(label, name, value, kind)
	}
	return form.label(label, name, el("input", attrs("type", "number", "name", name, "value", form.value(name, value), "step", "any"), form.validationAttrs(name)))
}
