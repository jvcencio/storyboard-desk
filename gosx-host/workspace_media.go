package main

import (
	"crypto/subtle"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"m31labs.dev/gosx"
)

func mediaURL(id, rel string) string {
	parts := strings.Split(rel, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return "/media/" + url.PathEscape(id) + "/" + strings.Join(parts, "/")
}
func (s *ProjectStore) OpenMedia(id, rel string) (*os.File, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return nil, err
	}
	if !(strings.HasPrefix(rel, "media/") || strings.HasPrefix(rel, "exports/")) {
		return nil, fmt.Errorf("invalid media path")
	}
	dir, err := projectDirectory(s.root, id)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	if err = safeProjectPath(dir, rel, false); err != nil {
		return nil, err
	}
	return dir.Open(rel)
}
func (h *workspaceHandler) media(rw http.ResponseWriter, r *http.Request) {
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/media/"), "/", 2)
	if len(parts) != 2 {
		http.NotFound(rw, r)
		return
	}
	f, err := h.store.OpenMedia(parts[0], parts[1])
	if err != nil {
		http.Error(rw, "Media unavailable", 404)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(rw, r)
		return
	}
	rw.Header().Set("Content-Type", mime.TypeByExtension(filepath.Ext(parts[1])))
	if strings.HasPrefix(parts[1], "exports/") {
		rw.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(parts[1])+`"`)
	}
	http.ServeContent(rw, r, info.Name(), info.ModTime(), f)
}
func (h *workspaceHandler) upload(rw http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(rw, r.Body, maxUpload+(2<<20))
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		http.Error(rw, "Upload is missing or too large", 400)
		return
	}
	defer r.MultipartForm.RemoveAll()
	if subtle.ConstantTimeCompare([]byte(r.PostForm.Get("csrf")), []byte(h.csrf)) != 1 {
		http.Error(rw, "Invalid token", 403)
		return
	}
	values := r.PostForm
	rev, err := strconv.Atoi(values.Get("revision"))
	if err != nil {
		http.Error(rw, "Invalid revision", 400)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(rw, "Choose a media file", 400)
		return
	}
	defer file.Close()
	_, err = h.store.ImportMedia(values.Get("project"), values.Get("shot"), values.Get("kind"), header.Filename, header.Size, file, rev, values.Get("title"), values.Get("notes"))
	if err != nil {
		status := 400
		if err == ErrRevision {
			status = 409
		}
		q := r.URL.Query()
		q.Set("project", values.Get("project"))
		q.Set("shot", values.Get("shot"))
		q.Set("view", values.Get("view"))
		r.URL.RawQuery = q.Encode()
		h.render(rw, r, status, formFailure("/upload", formValues(values), err))
		return
	}
	http.Redirect(rw, r, pageURL(values.Get("project"), values.Get("view"), values.Get("shot"), values.Get("scene_filter")), 303)
}
func (w workspacePage) uploadForm(kind string) gosx.Node {
	options := []string{"sketch", "keyframe", "clip"}
	children := w.controls("/upload")
	children = append(children, attrs("enctype", "multipart/form-data"))
	if kind == "" {
		children = append(children, w.selectField("/upload", "Import as", "kind", "clip", options))
	} else {
		children = append(children, hidden("kind", kind))
	}
	accept := ".png,.jpg,.jpeg,.webp,.mp4,.mov,.webm"
	if kind == "reference" {
		accept = ".png,.jpg,.jpeg,.webp"
		children = append(children, w.field("/upload", "Reference title", "title", "", "text"), w.field("/upload", "Reference notes", "notes", "", "textarea"))
	}
	if kind == "soundtrack" {
		accept = ".wav"
	}
	children = append(children, el("label", txt("Choose a file · up to 256 MiB"), el("input", attrs("type", "file", "name", "file", "accept", accept, "required", "required"))), button("Copy into project"))
	return el("form", children...)
}
func (w workspacePage) assetPreview(a ProjectDocument) gosx.Node {
	src := mediaURL(shotText(w.project, "id"), shotText(a, "path"))
	if shotText(a, "kind") == "clip" {
		return el("video", attrs("src", src, "controls", "controls", "preload", "metadata", "muted", "muted", "playsinline", "playsinline"))
	}
	return el("img", attrs("src", src, "alt", shotText(a, "original_name"), "loading", "lazy"))
}
func boardPreview(s ProjectDocument) (ProjectDocument, string) {
	var a ProjectDocument
	for _, kind := range []string{"keyframe", "sketch", "clip"} {
		if a = selectedAsset(s, kind); a != nil {
			break
		}
	}
	if a == nil {
		return nil, ""
	}
	path := shotText(a, "path")
	if shotText(a, "kind") == "clip" {
		path = shotText(a, "poster_path")
	}
	return a, path
}

func (w workspacePage) cardArt(s ProjectDocument) gosx.Node {
	href := w.href("library", shotText(s, "id"))
	a, path := boardPreview(s)
	if w.shared {
		href = w.href("board", shotText(s, "id"))
		if !shareablePreview(path) {
			a = nil
		}
	}
	if a == nil {
		return el("a", attrs("href", href, "class", "card-art"), blockingDiagram(s, shotText(w.project, "aspect")))
	}
	if path == "" {
		return link("▶", href, "card-art")
	}
	return el("a", attrs("href", href, "class", "card-art"), el("img", attrs("src", mediaURL(shotText(w.project, "id"), path), "alt", shotText(s, "title"), "loading", "lazy")))
}
func (w workspacePage) mediaLibrary() gosx.Node {
	panels := []any{cls("div", "paper", el("h2", txt("Import images & footage")), el("p", txt("Choose the destination shot, then copy an image or clip into the project. Originals stay in place; new takes arrive unselected.")))}
	selector := []any{attrs("method", "get"), hidden("project", shotText(w.project, "id")), hidden("view", "library"), w.shotSelector("shot", shotText(w.shot, "id")), w.selectField("", "Prompt destination", "provider", w.promptProvider, []string{"grok", "flow", "gemini", "other"}), button("Choose shot")}
	panels = append(panels, el("form", selector...), cls("div", "paper", el("h3", txt("Import to "+shotText(w.shot, "title"))), w.uploadForm("")))
	cards := []any{}
	for _, s := range list(w.project, "shots") {
		for _, a := range list(s, "assets") {
			c := w
			c.shot = s
			cards = append(cards, c.takeCard(a))
		}
	}
	panels = append(panels, w.prompts(), w.localDrawing(), w.localJobs(), cls("div", "media-grid", fragment(cards...)))
	undo := w.controls("/undo-placement")
	undo = append(undo, button("Undo last footage placement"))
	panels = append(panels, el("form", undo...))
	return el("section", panels...)
}
func (w workspacePage) shotSelector(name, selected string) gosx.Node {
	options := []any{attrs("name", name)}
	for _, s := range list(w.project, "shots") {
		a := []any{attrs("value", shotText(s, "id"))}
		if shotText(s, "id") == selected {
			a = append(a, attrs("selected", "selected"))
		}
		options = append(options, el("option", append(a, txt(shotText(s, "title")))...))
	}
	return el("label", txt("Shot"), el("select", options...))
}
func (w workspacePage) takeCard(a ProjectDocument) gosx.Node {
	w = w.scoped("asset", shotText(a, "id")).scoped("shot", shotText(w.shot, "id"))
	kind, aid := shotText(a, "kind"), shotText(a, "id")
	binned := shotText(a, "binned_at") != ""
	label := fmt.Sprintf("%s · take %g · %s", kind, number(a, "take"), shotText(w.shot, "title"))
	if binned {
		label += " · binned"
	}
	if shotText(w.shot, "selected_"+kind) == aid {
		label += " · selected"
	}
	children := []any{attrs("data-asset", aid), el("h3", txt(label)), w.assetPreview(a), cls("p", "fine", txt(shotText(a, "original_name"))), cls("p", "fine", txt(shotText(a, "probe_error")))}
	if !binned {
		selects := w.controls("/select")
		selects = append(selects, hidden("asset", aid), hidden("kind", kind), button("Select this take"))
		if kind == "keyframe" {
			selects = append(selects, el("button", attrs("name", "approve", "value", "true"), txt("Select & approve starter")))
		}
		children = append(children, el("form", selects...))
	}
	if kind == "keyframe" && shotText(w.shot, "selected_keyframe") == aid {
		ready := "Approval needs review"
		if imageApproved(w.project, w.shot) {
			ready = "Approved starting image"
		}
		children = append(children, cls("p", "status", txt(ready)))
	}
	if kind == "clip" && !binned {
		if len(list(w.project, "music_sections")) > 0 {
			children = append(children, w.collectForm(a))
		}
		end := ""
		if rangeEnd(a) > 0 {
			end = fmt.Sprint(rangeEnd(a))
		}
		trim := w.controls("/clip-range")
		trim = append(trim, hidden("asset", aid), hidden("measured", fmt.Sprint(number(a, "duration_seconds"))), w.field("/clip-range", "Source in (seconds)", "start", fmt.Sprint(number(a, "in_seconds")), "number"), w.field("/clip-range", "Source out (seconds)", "end", end, "number"), button("Save source range"))
		children = append(children, el("form", trim...))
		place := w.controls("/place")
		place = append(place, hidden("asset", aid), hidden("measured", fmt.Sprint(number(a, "duration_seconds"))), w.shotSelector("target", shotText(w.shot, "id")), w.field("/place", "Source in", "start", fmt.Sprint(number(a, "in_seconds")), "number"), w.field("/place", "Source out", "end", end, "number"), button("Place footage in shot"))
		children = append(children, el("details", el("summary", txt("Use this source elsewhere")), el("form", place...)))
		if object(a, "source_ref") == nil {
			review := object(a, "editorial_review")
			f := w.controls("/review-clip")
			f = append(f, hidden("asset", aid), w.selectField("/review-clip", "Review status", "status", shotText(review, "status"), []string{"pending", "candidate", "avoid"}), w.field("/review-clip", "Setup / angle", "setup", shotText(review, "setup"), "text"), w.field("/review-clip", "Story phase", "phase", shotText(review, "phase"), "text"), w.field("/review-clip", "Review notes", "note", shotText(review, "note"), "textarea"), button("Save review"))
			children = append(children, el("details", el("summary", txt("Editorial review")), el("form", f...)))
		}
	}
	bin := w.controls("/bin")
	bin = append(bin, hidden("asset", aid))
	if binned {
		bin = append(bin, hidden("restore", "true"), button("Restore take"))
	} else {
		bin = append(bin, w.field("/bin", "Reason · optional", "reason", "", "text"), button("Move take to bin"))
	}
	children = append(children, el("details", el("summary", txt("Take bin")), el("form", bin...)))
	return cls("article", "paper take-card", children...)
}
