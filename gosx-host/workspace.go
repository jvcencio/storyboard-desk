package main

import (
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"m31labs.dev/gosx"
)

//go:embed workspace.css workspace.js
var workspaceAssets embed.FS

type workspaceHandler struct {
	store *ProjectStore
	csrf  string
}
type workspacePage struct {
	exportFiles                                         []string
	store                                               *ProjectStore
	cloudState, quote                                   ProjectDocument
	cloudError, drawError, handoffClaude, handoffIntake string
	drawJobs                                            []ProjectDocument
	quoteID                                             string
	projects                                            []ProjectDocument
	project, shot                                       ProjectDocument
	csrf, view, scene, message                          string
	promptProvider                                      string
	draft                                               url.Values
	action                                              string
}

func el(tag string, children ...any) gosx.Node { return gosx.El(tag, children...) }
func fragment(children ...any) gosx.Node {
	nodes := []gosx.Node{}
	for _, child := range children {
		nodes = append(nodes, child.(gosx.Node))
	}
	return gosx.Fragment(nodes...)
}
func txt(s string) gosx.Node { return gosx.Text(s) }
func attrs(pairs ...string) gosx.AttrList {
	nodes := []any{}
	for i := 0; i+1 < len(pairs); i += 2 {
		nodes = append(nodes, gosx.Attr(pairs[i], pairs[i+1]))
	}
	return gosx.Attrs(nodes...)
}
func cls(tag, class string, children ...any) gosx.Node {
	return el(tag, append([]any{attrs("class", class)}, children...)...)
}
func link(label, href, class string) gosx.Node {
	if parsed, err := url.Parse(strings.TrimSpace(href)); err != nil || (parsed.Scheme != "" && parsed.Scheme != "http" && parsed.Scheme != "https") || strings.HasPrefix(href, "//") {
		href = "#"
	}
	a := attrs("href", href, "class", class)
	if strings.HasPrefix(href, "/download/") || (strings.HasPrefix(href, "/media/") && strings.Contains(href, "/exports/")) {
		return el("a", a, attrs("download", "download"), txt(label))
	}
	return el("a", a, txt(label))
}
func hidden(name, value string) gosx.Node {
	return el("input", attrs("type", "hidden", "name", name, "value", value))
}
func jsonFileField(target string) gosx.Node {
	return el("label", txt("Read a JSON file into the form"), el("input", attrs("type", "file", "accept", ".json,application/json", "data-json-file", target)), el("span", attrs("role", "status", "class", "fine")))
}
func button(label string) gosx.Node {
	return el("button", attrs("type", "submit", "class", "button primary"), txt(label))
}
func jsonText(v any) string { b, _ := json.MarshalIndent(v, "", "  "); return string(b) }
func number(p ProjectDocument, k string) float64 {
	var n float64
	_ = json.Unmarshal(p[k], &n)
	return n
}
func boolean(p ProjectDocument, k string) bool { var b bool; _ = json.Unmarshal(p[k], &b); return b }
func list(p ProjectDocument, k string) []ProjectDocument {
	var rows []ProjectDocument
	_ = json.Unmarshal(p[k], &rows)
	return rows
}
func object(p ProjectDocument, k string) ProjectDocument {
	var doc ProjectDocument
	_ = json.Unmarshal(p[k], &doc)
	return doc
}
func pageURL(id, view, shot, scene string) string {
	q := url.Values{}
	if id != "" {
		q.Set("project", id)
	}
	if view != "" {
		q.Set("view", view)
	}
	if shot != "" {
		q.Set("shot", shot)
	}
	if scene != "" {
		q.Set("scene", scene)
	}
	return "/?" + q.Encode()
}
func (w workspacePage) href(view, shot string) string {
	return pageURL(shotText(w.project, "id"), view, shot, w.scene)
}
func (w workspacePage) controls(action string) []any {
	rev := strconv.Itoa(documentRevision(w.project))
	if w.action == action && w.draft.Has("revision") {
		rev = w.draft.Get("revision")
	}
	a := attrs("method", "post", "action", action, "data-dirty", "true")
	if containsField([]string{"/export", "/claude", "/section-reel", "/section-export"}, action) {
		a = attrs("method", "post", "action", action, "data-download-form", "true")
	}
	return []any{a, hidden("csrf", w.csrf), hidden("project", shotText(w.project, "id")), hidden("shot", shotText(w.shot, "id")), hidden("revision", rev), hidden("view", w.view), hidden("scene_filter", w.scene)}
}
func (w workspacePage) field(action, label, name, value, kind string) gosx.Node {
	if w.action == action && w.draft.Has(name) {
		value = w.draft.Get(name)
	}
	var input gosx.Node
	switch kind {
	case "textarea":
		limit := "12000"
		if name == "plan" || name == "template" {
			limit = "1048576"
		}
		input = el("textarea", attrs("name", name, "rows", "3", "maxlength", limit), txt(value))
	case "checkbox":
		a := []any{attrs("type", "checkbox", "name", name, "value", "true")}
		if value == "true" {
			a = append(a, attrs("checked", "checked"))
		}
		input = el("input", a...)
	default:
		a := []string{"type", kind, "name", name, "value", value}
		if kind == "number" {
			a = append(a, "step", "0.01", "min", "0")
		}
		input = el("input", attrs(a...))
	}
	return el("label", txt(label), input)
}
func (w workspacePage) selectField(action, label, name, value string, options []string) gosx.Node {
	if w.action == action && w.draft.Has(name) {
		value = w.draft.Get(name)
	}
	children := []any{attrs("name", name)}
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
			if name == "scene" && action == "" {
				label = "Whole episode"
			}
		}
		children = append(children, el("option", append(a, txt(label))...))
	}
	if !found && value != "" {
		children = append(children, el("option", attrs("value", value, "selected", "selected"), txt(value)))
	}
	return el("label", txt(label), el("select", children...))
}

func (h *workspaceHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	host, port, err := net.SplitHostPort(r.Host)
	if err != nil || host != "127.0.0.1" || port == "" {
		http.Error(rw, "Use the local workspace address", 403)
		return
	}
	rw.Header().Set("X-Content-Type-Options", "nosniff")
	rw.Header().Set("Cache-Control", "no-store")
	rw.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; script-src 'self'; img-src 'self' data:; media-src 'self'; connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
	if r.Method == "GET" && (r.URL.Path == "/workspace.css" || r.URL.Path == "/workspace.js") {
		data, err := workspaceAssets.ReadFile(strings.TrimPrefix(r.URL.Path, "/"))
		if err != nil {
			http.NotFound(rw, r)
			return
		}
		mime := "text/css; charset=utf-8"
		if strings.HasSuffix(r.URL.Path, ".js") {
			mime = "text/javascript; charset=utf-8"
		}
		rw.Header().Set("Content-Type", mime)
		rw.Write(data)
		return
	}
	if r.Method == "GET" && r.URL.Path == "/api/health" {
		rw.Header().Set("Content-Type", "application/json")
		fmt.Fprint(rw, `{"app":"Storyboard Desk","backend":"go","version":"0.12.0"}`)
		return
	}
	if r.Method == "GET" && r.URL.Path == "/download/script" {
		p, err := h.store.Load(r.URL.Query().Get("project"))
		if err != nil {
			http.Error(rw, err.Error(), 400)
			return
		}
		rw.Header().Set("Content-Type", "text/plain; charset=utf-8")
		rw.Header().Set("Content-Disposition", `attachment; filename="storyboard.txt"`)
		fmt.Fprint(rw, scriptText(p))
		return
	}
	if r.Method == "GET" && r.URL.Path == "/" {
		h.render(rw, r, 200, "", nil, "")
		return
	}
	if r.Method == "GET" && r.URL.Path == "/download/plan" {
		p, err := h.store.Load(r.URL.Query().Get("project"))
		if err != nil {
			http.Error(rw, err.Error(), 400)
			return
		}
		rw.Header().Set("Content-Type", "application/json")
		rw.Header().Set("Content-Disposition", `attachment; filename="storyboard-plan.json"`)
		fmt.Fprint(rw, jsonText(projectPlan(p)))
		return
	}
	if r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/media/") {
		h.media(rw, r)
		return
	}
	if r.Method != "POST" {
		http.NotFound(rw, r)
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
		http.Error(rw, "Invalid origin", 403)
		return
	}
	if r.URL.Path == "/upload" {
		h.upload(rw, r)
		return
	}
	r.Body = http.MaxBytesReader(rw, r.Body, 2<<20)
	if err := r.ParseForm(); err != nil {
		http.Error(rw, "Invalid form", 400)
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.PostForm.Get("csrf")), []byte(h.csrf)) != 1 {
		http.Error(rw, "Invalid token", 403)
		return
	}
	values := r.PostForm
	id, sid := values.Get("project"), values.Get("shot")
	view := values.Get("view")
	if view == "" {
		view = "board"
	}
	var updated ProjectDocument
	if r.URL.Path == "/create" {
		raw := ProjectDocument{"title": rawValue(values.Get("title")), "brief": rawValue(values.Get("brief")), "aspect": rawValue(values.Get("aspect")), "music_video": rawValue(values.Get("music_video") == "true"), "shots": rawValue([]ProjectDocument{{"title": rawValue("The opening"), "purpose": rawValue("Establish the story.")}})}
		if values.Get("template") != "" {
			raw, err = decodeDocument(values.Get("template"))
			if err == nil && shotText(raw, "template_kind") != "storyboard-project-v1" {
				err = fmt.Errorf("choose a storyboard-project-v1 template")
			}
		}
		if err == nil {
			updated, err = h.store.CreateTemplate(raw)
		}
	} else {
		var rev int
		rev, err = strconv.Atoi(values.Get("revision"))
		if err == nil {
			switch r.URL.Path {
			case "/save":
				patch := ProjectDocument{}
				for _, k := range shotFields {
					if values.Has(k) {
						patch[k] = rawValue(values.Get(k))
					}
				}
				if values.Has("duration") {
					var n float64
					n, err = strconv.ParseFloat(values.Get("duration"), 64)
					if err == nil && !finite(n) {
						err = fmt.Errorf("enter a finite number")
					}
					if err == nil {
						patch["duration"] = rawValue(n)
					}
				}
				if err == nil {
					updated, err = h.store.UpdateShot(id, sid, rev, patch)
				}
			case "/project":
				patch := ProjectDocument{}
				for _, k := range append(append([]string{}, projectTextFields...), "aspect") {
					if values.Has(k) {
						patch[k] = rawValue(values.Get(k))
					}
				}
				patch["music_video"] = rawValue(values.Get("music_video") == "true")
				if values.Has("runtime_limit_seconds") {
					var n float64
					n, err = strconv.ParseFloat(values.Get("runtime_limit_seconds"), 64)
					if err == nil && !finite(n) {
						err = fmt.Errorf("enter a finite number")
					}
					if err == nil {
						patch["runtime_limit_seconds"] = rawValue(n)
					}
				}
				if err == nil {
					updated, err = h.store.UpdateProject(id, rev, patch)
				}
			case "/reference-note":
				index, _ := strconv.Atoi(values.Get("index"))
				updated, err = h.store.ReferenceNote(id, rev, index, values.Get("operation"), values.Get("title"), values.Get("url"), values.Get("notes"))
			case "/references":
				var refs []ProjectDocument
				if json.Unmarshal([]byte(values.Get("references")), &refs) != nil || refs == nil {
					err = fmt.Errorf("references must be a JSON array")
				} else {
					updated, err = h.store.UpdateProject(id, rev, ProjectDocument{"references": rawValue(refs)})
				}
			case "/insert":
				updated, err = h.store.Insert(id, sid, values.Get("position"), rev)
			case "/move":
				var p ProjectDocument
				p, err = h.store.Load(id)
				if err == nil {
					shots, _ := documentShots(p)
					order := []string{}
					for _, s := range shots {
						order = append(order, shotText(s, "id"))
					}
					delta, _ := strconv.Atoi(values.Get("delta"))
					if delta != 1 && delta != -1 {
						err = fmt.Errorf("choose earlier or later")
					} else {
						found := false
						for i, v := range order {
							if v == sid {
								found = true
								j := i + delta
								if j < 0 || j >= len(order) {
									err = fmt.Errorf("shot is already at the edge")
								} else {
									order[i], order[j] = order[j], order[i]
								}
								break
							}
						}
						if !found {
							err = ErrNotFound
						}
					}
					if err == nil {
						updated, err = h.store.Reorder(id, rev, order)
					}
				}
			case "/draw", "/draw-cancel", "/draw-recover":
				err = h.drawingCommand(r.URL.Path, id, sid, rev, values)
			case "/claude":
				scene, shot := "", ""
				scope := values.Get("scope")
				if !containsField([]string{"shot", "scene", "episode"}, scope) {
					err = fmt.Errorf("choose a review scope")
				} else {
					if scope == "shot" {
						shot = sid
					}
					if scope == "scene" {
						scene = values.Get("scene_filter")
						if scene == "" {
							p, e := h.store.Load(id)
							if e != nil {
								err = e
							} else {
								target, e := findShot(p, sid)
								if e != nil {
									err = e
								} else {
									scene = shotText(target, "scene")
								}
							}
						}
						if scene == "" && err == nil {
							err = fmt.Errorf("choose a scene before exporting a scene review")
						}
					}
					if err == nil {
						var rel string
						rel, err = h.store.ExportClaude(id, scene, shot)
						if err == nil {
							http.Redirect(rw, r, mediaURL(id, rel), 303)
							return
						}
					}
				}

			case "/cloud-prepare":
				_, err = h.cloudCommand(rw, r, id, rev, values)
				if err == nil {
					return
				}
			case "/cloud-connect", "/cloud-allowance", "/cloud-brief", "/cloud-approve-brief", "/cloud-submit", "/cloud-resume":
				updated, err = h.cloudCommand(rw, r, id, rev, values)
			case "/music-cue", "/section-save", "/section-split", "/coverage-add", "/coverage-edit", "/section-edit", "/section-undo", "/section-reel", "/section-export":
				var rel string
				updated, rel, err = h.musicCommand(r.URL.Path, id, rev, values)
				if err == nil && rel != "" {
					http.Redirect(rw, r, mediaURL(id, rel), 303)
					return
				}
			case "/export":
				var rel string
				rel, err = h.store.ExportReview(id)
				if err == nil {
					http.Redirect(rw, r, mediaURL(id, rel), 303)
					return
				}
			case "/select":
				updated, err = h.store.Choose(id, sid, values.Get("kind"), values.Get("asset"), values.Get("approve") == "true", rev)
			case "/bin":
				updated, err = h.store.Bin(id, sid, values.Get("asset"), values.Get("reason"), values.Get("restore") == "true", rev)
			case "/clip-range", "/place":
				var start, end, measured float64
				start, err = strconv.ParseFloat(values.Get("start"), 64)
				if err == nil {
					end, err = strconv.ParseFloat(values.Get("end"), 64)
				}
				if err == nil {
					measured, err = strconv.ParseFloat(values.Get("measured"), 64)
				}
				if err == nil {
					if r.URL.Path == "/place" {
						updated, err = h.store.PlaceClip(id, sid, values.Get("asset"), values.Get("target"), rev, start, end, measured)
					} else {
						updated, err = h.store.TrimClip(id, sid, values.Get("asset"), rev, start, end, measured)
					}
				}
			case "/undo-placement":
				updated, err = h.store.UndoPlacement(id, rev)
			case "/review-clip":
				updated, err = h.store.ReviewClip(id, sid, values.Get("asset"), rev, values.Get("status"), values.Get("setup"), values.Get("phase"), values.Get("note"))
			case "/plan":
				var plan ProjectDocument
				plan, err = decodeDocument(values.Get("plan"))
				if err == nil {
					updated, err = h.store.ApplyPlan(id, rev, plan)
				}
			default:
				http.NotFound(rw, r)
				return
			}
		}
	}
	if err != nil {
		status := 400
		if errors.Is(err, ErrRevision) {
			status = 409
		}
		q := r.URL.Query()
		q.Set("project", id)
		q.Set("shot", sid)
		q.Set("view", view)
		r.URL.RawQuery = q.Encode()
		h.render(rw, r, status, err.Error(), values, r.URL.Path)
		return
	}
	if updated != nil {
		id = shotText(updated, "id")
	}
	if r.URL.Path == "/create" {
		view = "board"
		sid = ""
	}
	// Keep a root redirect for original form clients; richer navigation supplies view.
	target := "/"
	if values.Has("view") {
		target = pageURL(id, view, sid, values.Get("scene_filter"))
	}
	http.Redirect(rw, r, target, 303)
}

func (h *workspaceHandler) render(rw http.ResponseWriter, r *http.Request, status int, message string, draft url.Values, action string) {
	projects, err := h.store.List()
	if err != nil {
		http.Error(rw, err.Error(), 500)
		return
	}
	w := workspacePage{store: h.store, quoteID: r.URL.Query().Get("quote"), projects: projects, csrf: h.csrf, view: r.URL.Query().Get("view"), scene: r.URL.Query().Get("scene"), message: message, draft: draft, action: action}
	w.promptProvider = r.URL.Query().Get("provider")
	if !containsField([]string{"grok", "flow", "gemini", "other"}, w.promptProvider) {
		w.promptProvider = "grok"
	}
	if w.view == "" {
		w.view = "board"
	}
	id := r.URL.Query().Get("project")
	for _, p := range projects {
		if shotText(p, "id") == id {
			w.project = p
			break
		}
	}
	if w.project == nil && len(projects) > 0 {
		if id != "" {
			http.Error(rw, "Project not found", 404)
			return
		}
		w.project = projects[0]
	}
	if w.project != nil {
		shots, _ := documentShots(w.project)
		for _, s := range shots {
			if shotText(s, "id") == r.URL.Query().Get("shot") {
				w.shot = s
				break
			}
		}
		if w.shot == nil {
			for _, s := range shots {
				if w.scene == "" || shotText(s, "scene") == w.scene {
					w.shot = s
					break
				}
			}
		}
	}
	h.loadPageFeatures(&w)
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	rw.WriteHeader(status)
	fmt.Fprint(rw, "<!doctype html>\n"+gosx.RenderHTML(w.render()))
}

func (w workspacePage) render() gosx.Node {
	nav := []any{}
	for _, p := range w.projects {
		class := "project-link"
		if shotText(p, "id") == shotText(w.project, "id") {
			class += " active"
		}
		nav = append(nav, link(shotText(p, "title"), pageURL(shotText(p, "id"), "board", "", ""), class))
	}
	sidebar := cls("aside", "sidebar", link("▤  STORYBOARD DESK", "/", "brand"), cls("p", "eyebrow", txt("YOUR PROJECTS")), el("nav", attrs("aria-label", "Projects"), fragment(nav...)), link("＋ New project", "#new-project", "button secondary"), cls("p", "local-status", txt("● Saved on your computer")))
	main := []any{cls("header", "topbar", txt("WORKBENCH / "+shotText(w.project, "title")), cls("span", "status", attrs("id", "save-status"), txt("Saved locally")))}
	if w.message != "" {
		main = append(main, cls("div", "error", attrs("role", "alert"), txt(w.message+". Your submitted values remain below.")))
	}
	if w.project == nil {
		main = append(main, cls("section", "paper welcome", cls("p", "eyebrow", txt("FROM AN IDEA TO A SEQUENCE")), el("h1", txt("A place for your next story.")), el("p", txt("Plan the shots, collect takes and shape the sequence."))))
	} else {
		shots, _ := documentShots(w.project)
		seconds := 0.0
		for _, s := range shots {
			seconds += number(s, "duration")
		}
		heading := cls("section", "project-heading", cls("p", "eyebrow", txt("FROM AN IDEA TO A SEQUENCE")), el("h1", txt(shotText(w.project, "title"))), el("p", txt(shotText(w.project, "brief"))))
		stats := fmt.Sprintf("%d shots · %.1f seconds · %s · revision %d", len(shots), seconds, shotText(w.project, "aspect"), documentRevision(w.project))
		main = append(main, heading, cls("div", "project-bar", txt(stats)))
		export := w.controls("/export")
		export = append(export, button("Export review package ↓"))
		main = append(main, el("form", export...))
		tabs := []any{}
		for _, tab := range [][2]string{{"episode", "Episode map"}, {"board", "Storyboard"}, {"review", "Review sequence"}, {"library", "Media Library"}, {"generate", "Generate"}, {"music", "Music & lyrics"}, {"sections", "Song sections"}, {"direction", "Brief & appearance"}, {"references", "References"}, {"handoff", "Plans & handoffs"}, {"exports", "Exports"}} {
			class := "tab"
			if w.view == tab[0] {
				class += " active"
			}
			tabs = append(tabs, link(tab[1], w.href(tab[0], ""), class))
		}
		main = append(main, cls("nav", "tabs", attrs("aria-label", "Workspace views"), fragment(tabs...)))
		switch w.view {
		case "exports":
			main = append(main, w.exports())
		case "review":
			main = append(main, w.review())
		case "generate":
			main = append(main, w.generation())
		case "music":
			main = append(main, w.music())
		case "sections":
			main = append(main, w.sections())
		case "library":
			main = append(main, w.mediaLibrary())
		case "direction":
			main = append(main, w.direction())
		case "references":
			main = append(main, w.references())
		case "handoff":
			main = append(main, w.handoffs())
		case "episode":
			main = append(main, w.episode())
		default:
			main = append(main, w.board())
		}
	}
	main = append(main, w.createForm(), cls("footer", "fine", txt("Local project files · "), txt("Storyboard Desk")))
	return el("html", attrs("lang", "en"), el("head", el("meta", attrs("charset", "utf-8")), el("meta", attrs("name", "viewport", "content", "width=device-width, initial-scale=1")), el("title", txt("Storyboard Desk")), el("link", attrs("rel", "stylesheet", "href", "/workspace.css")), el("script", attrs("src", "/workspace.js", "defer", "defer"))), el("body", sidebar, cls("main", "workspace", fragment(main...))))
}

func (w workspacePage) createForm() gosx.Node {
	children := []any{attrs("method", "post", "action", "/create", "data-dirty", "true"), hidden("csrf", w.csrf), hidden("view", "board"), w.field("/create", "Project title", "title", "", "text"), w.field("/create", "Your idea", "brief", "", "textarea"), w.selectField("/create", "Format", "aspect", "9:16", []string{"9:16", "16:9", "1:1"}), w.field("/create", "Music video", "music_video", "false", "checkbox"), button("Create storyboard"), el("details", el("summary", txt("Start from a template")), jsonFileField("template"), w.field("/create", "Project template JSON", "template", "", "textarea"), el("p", attrs("class", "fine"), txt("A template creates a new project with fresh shot identities and no media or approvals.")))}
	return cls("details", "paper new-project", attrs("id", "new-project"), el("summary", txt("＋ A new story")), el("form", children...))
}

func (w workspacePage) direction() gosx.Node {
	children := w.controls("/project")
	children = append(children, el("h2", txt("Brief & appearance")))
	for _, f := range [][2]string{{"title", "Project title"}, {"brief", "The idea"}, {"look", "Shared appearance"}, {"continuity", "Shared continuity"}, {"episode_notes", "Story commitments"}, {"lyrics", "Lyrics · exact original text"}} {
		kind := "textarea"
		if f[0] == "title" {
			kind = "text"
		}
		children = append(children, w.field("/project", f[1], f[0], shotText(w.project, f[0]), kind))
	}
	children = append(children, w.selectField("/project", "Format", "aspect", shotText(w.project, "aspect"), []string{"9:16", "16:9", "1:1"}), w.field("/project", "Episode budget in seconds", "runtime_limit_seconds", fmt.Sprint(number(w.project, "runtime_limit_seconds")), "number"), w.field("/project", "Music video", "music_video", strconv.FormatBool(boolean(w.project, "music_video")), "checkbox"), button("Save project"))
	return cls("section", "paper", el("form", children...))
}

func (w workspacePage) references() gosx.Node {
	refs := list(w.project, "references")
	cards := []any{}
	for i, r := range refs {
		c := w
		c = c.scoped("index", strconv.Itoa(i))
		f := c.controls("/reference-note")
		f = append(f, hidden("index", strconv.Itoa(i)), hidden("operation", "update"), c.field("/reference-note", "Title", "title", shotText(r, "title"), "text"), c.field("/reference-note", "Source URL", "url", shotText(r, "url"), "url"), c.field("/reference-note", "Notes", "notes", shotText(r, "notes"), "textarea"), button("Save reference note"))
		remove := c.controls("/reference-note")
		remove = append(remove, hidden("index", strconv.Itoa(i)), hidden("operation", "remove"), button("Remove note"))
		cards = append(cards, cls("article", "paper", link("Open reference ↗", shotText(r, "url"), "button secondary"), el("form", f...), el("form", remove...)))
	}
	raw := w.project["references"]
	if len(raw) == 0 {
		raw = rawValue([]any{})
	}
	children := w.controls("/references")
	children = append(children, el("h2", txt("Reference notes")), el("p", txt("Keep source links, titles and notes together. Attach actual image references above for visual conditioning.")), w.field("/references", "References · JSON array of title, url and notes", "references", string(raw), "textarea"), button("Save reference notes"))
	for _, ref := range list(w.project, "visual_references") {
		cards = append(cards, cls("article", "paper", el("img", attrs("src", mediaURL(shotText(w.project, "id"), shotText(ref, "path")), "alt", shotText(ref, "title"), "class", "reference-image")), el("h3", txt(shotText(ref, "title"))), el("p", txt(shotText(ref, "notes")))))
	}
	add := w.controls("/reference-note")
	add = append(add, hidden("operation", "add"), w.field("/reference-note", "Title", "title", "", "text"), w.field("/reference-note", "Source URL", "url", "", "url"), w.field("/reference-note", "Notes", "notes", "", "textarea"), button("Add reference note"))
	return el("section", cls("div", "paper", el("h2", txt("Appearance images")), w.uploadForm("reference")), cls("div", "reference-grid", fragment(cards...)), cls("details", "paper", el("summary", txt("Add a reference note")), el("form", add...)), cls("details", "paper", el("summary", txt("Advanced reference JSON")), el("form", children...)))
}

func (w workspacePage) handoffs() gosx.Node {
	children := w.controls("/plan")
	children = append(children, el("h2", txt("Refine the plan")), el("p", txt("Import revisions to existing shots. Shot identities, saved media and selected takes stay with the project.")), link("Download plan JSON ↓", "/download/plan?project="+url.QueryEscape(shotText(w.project, "id")), "button secondary"), jsonFileField("plan"), w.field("/plan", "Revised plan JSON", "plan", "", "textarea"), button("Apply reviewed plan"))
	pack := w.controls("/claude")
	pack = append(pack, w.selectField("/claude", "Review scope", "scope", "shot", []string{"shot", "scene", "episode"}), button("Download images & review brief ZIP"))
	return cls("section", "paper", el("h2", txt("Manual direction review")), copyArea("claude-brief", "Current-shot review brief", w.handoffClaude), copyArea("planning-intake", "Planning intake", w.handoffIntake), el("form", pack...), el("form", children...))
}

func (w workspacePage) episode() gosx.Node {
	shots, _ := documentShots(w.project)
	rows := []any{}
	elapsed := 0.0
	for i, s := range shots {
		rows = append(rows, cls("article", "episode-row", cls("span", "shot-number", txt(fmt.Sprintf("%02d", i+1))), el("div", cls("p", "eyebrow", txt(fmt.Sprintf("%s · %.1f–%.1fs", shotText(s, "scene"), elapsed, elapsed+number(s, "duration")))), el("h2", txt(shotText(s, "title"))), el("p", txt(shotText(s, "purpose"))), el("p", txt(shotText(s, "dialogue")))), link("Open shot ↗", w.href("board", shotText(s, "id")), "button secondary")))
		elapsed += number(s, "duration")
	}
	return cls("section", "paper", el("h2", txt("The whole episode")), el("p", txt(shotText(w.project, "episode_notes"))), fragment(rows...))
}

var fieldLabels = map[string]string{"scene": "Scene / sequence", "story_status": "Story source", "title": "Shot title", "purpose": "Story purpose", "composition": "Composition & starting pose", "action": "Subject action & timing", "camera": "Camera behavior", "sound": "Sound", "continuity": "Continuity", "feedback": "Feedback for the next take", "transition": "Ending / cut", "dialogue": "Dialogue", "narration": "Narration / voiceover", "audience": "What the audience notices", "jorge": "Character’s interpretation", "anomaly": "Intentional anomaly", "start_state": "Start state", "end_state": "End state", "screen_direction": "Screen direction / eye line", "assets_needed": "Required assets / missing inputs", "reference_notes": "Reference application & source status", "blocking": "Blocking diagram", "local_prompt": "Local drawing prompt", "local_negative_prompt": "Drawing exclusions"}

func (w workspacePage) board() gosx.Node {
	shots, _ := documentShots(w.project)
	cards := []any{}
	scenes := []string{""}
	seen := map[string]bool{}
	for i, s := range shots {
		scene := shotText(s, "scene")
		if scene != "" && !seen[scene] {
			scenes = append(scenes, scene)
			seen[scene] = true
		}
		if w.scene != "" && scene != w.scene {
			continue
		}
		class := "shot-card"
		if shotText(s, "id") == shotText(w.shot, "id") {
			class += " selected"
		}
		card := []any{cls("div", "card-top", el("span", txt(fmt.Sprintf("SHOT %03d", (i+1)*10))), el("span", txt(fmt.Sprintf("%.1fs", number(s, "duration"))))), w.cardArt(s), cls("div", "card-body", cls("p", "eyebrow", txt(scene)), el("h2", txt(shotText(s, "title"))), el("p", txt(shotText(s, "purpose"))))}
		c := w
		c.shot = s
		f := c.controls("/move")
		f = append(f, el("button", attrs("name", "delta", "value", "-1", "title", "Move earlier"), txt("←")), el("button", attrs("name", "delta", "value", "1", "title", "Move later"), txt("→")), link("Edit shot ↗", w.href("board", shotText(s, "id")), "button secondary"))
		card = append(card, cls("div", "card-footer", el("form", f...)))
		cards = append(cards, cls("article", class, card...))
	}
	filters := []any{attrs("method", "get", "class", "scene-filter"), hidden("project", shotText(w.project, "id")), hidden("view", "board"), w.selectField("", "Scene / sequence", "scene", w.scene, scenes), button("Filter")}
	return el("section", el("form", filters...), cls("div", "board-layout", cls("div", "shot-grid", fragment(cards...)), w.shotForm()))
}

func (w workspacePage) shotForm() gosx.Node {
	if w.shot == nil {
		return el("div")
	}
	children := w.controls("/save")
	children = append(children, cls("p", "eyebrow", txt("THE SHOT BRIEF")), el("h2", txt(shotText(w.shot, "title"))))
	children = append(children, button("Save shot brief"))
	for _, k := range shotFields {
		kind := "textarea"
		if k == "title" || k == "scene" {
			kind = "text"
		}
		var field gosx.Node
		switch k {
		case "story_status":
			field = w.selectField("/save", fieldLabels[k], k, shotText(w.shot, k), []string{"", "proposed", "reported_existing", "existing_candidate"})
		case "blocking":
			field = w.selectField("/save", fieldLabels[k], k, shotText(w.shot, k), []string{"", "selfie", "mirror", "terminal", "walkway", "sign", "reaction", "passenger", "pass", "help", "turbulence", "gate", "portal"})
		default:
			field = w.field("/save", fieldLabels[k], k, shotText(w.shot, k), kind)
		}
		children = append(children, field)
		if k == "title" {
			children = append(children, w.field("/save", "Target duration (seconds)", "duration", fmt.Sprint(number(w.shot, "duration")), "number"))
		}
	}
	children = append(children, button("Save shot brief"))
	insert := w.controls("/insert")
	insert = append(insert, el("button", attrs("name", "position", "value", "before"), txt("＋ Insert before")), el("button", attrs("name", "position", "value", "after"), txt("＋ Insert after")))
	return cls("aside", "paper shot-editor", attrs("id", "shot-editor"), el("form", children...), el("form", insert...))
}
