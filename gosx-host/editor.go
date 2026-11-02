package main

import (
    "crypto/subtle"
    "encoding/json"
    "fmt"
    "net"
    "net/http"
    "strconv"
    "strings"

    "m31labs.dev/gosx"
)

// editorHandler is an experimental GoSX screen backed by the existing service.
// The per-process CSRF token is separate from the backend's mutation token.
type editorHandler struct { service EditorService; csrf string }
func newEditorHandler(service EditorService, csrf string) http.Handler { return &editorHandler{service, csrf} }

func shotText(shot map[string]json.RawMessage, name string) string {
    var s string
    _ = json.Unmarshal(shot[name], &s)
    return s
}

func editorPage(state ServiceState, csrf string) string {
    projects := make([]gosx.Node, 0, len(state.Projects))
    for _, project := range state.Projects {
        shots := make([]gosx.Node, 0, len(project.Shots))
        for _, shot := range project.Shots {
            hidden := func(name, value string) gosx.Node { return gosx.El("input", gosx.Attrs(gosx.Attr("type", "hidden"), gosx.Attr("name", name), gosx.Attr("value", value))) }
            fields := make([]gosx.Node, 0, 5)
            for _, name := range []string{"title", "purpose", "composition", "action", "camera"} {
                fields = append(fields, gosx.El("label", gosx.Text(strings.ToUpper(name[:1])+name[1:]), gosx.El("input", gosx.Attrs(gosx.Attr("name", name), gosx.Attr("value", shotText(shot, name))))))
            }
            shots = append(shots, gosx.El("form", gosx.Attrs(gosx.Attr("method", "post"), gosx.Attr("action", "/save")),
                ShotCard(ShotCardProps{Title:shotText(shot,"title"), Purpose:shotText(shot,"purpose"), Composition:shotText(shot,"composition"), Action:shotText(shot,"action"), Camera:shotText(shot,"camera")}),
                hidden("csrf", csrf), hidden("project", project.ID), hidden("shot", shotText(shot,"id")), hidden("revision", strconv.Itoa(project.Revision)), gosx.Fragment(fields...), gosx.El("button", gosx.Attrs(gosx.Attr("type", "submit")), gosx.Text("Save shot"))))
            shots = append(shots, gosx.El("form", gosx.Attrs(gosx.Attr("method", "post"), gosx.Attr("action", "/insert")),
                hidden("csrf", csrf), hidden("project", project.ID), hidden("shot", shotText(shot,"id")), hidden("revision", strconv.Itoa(project.Revision)),
                gosx.El("button", gosx.Attrs(gosx.Attr("type", "submit"), gosx.Attr("name", "position"), gosx.Attr("value", "before")), gosx.Text("Insert before")),
                gosx.El("button", gosx.Attrs(gosx.Attr("type", "submit"), gosx.Attr("name", "position"), gosx.Attr("value", "after")), gosx.Text("Insert after"))))
        }
        projects = append(projects, gosx.El("section", gosx.El("h2", gosx.Text(project.Title)), gosx.Fragment(shots...)))
    }
    create := gosx.El("form", gosx.Attrs(gosx.Attr("method", "post"), gosx.Attr("action", "/create")),
        gosx.El("h2", gosx.Text("New storyboard")),
        gosx.El("input", gosx.Attrs(gosx.Attr("type", "hidden"), gosx.Attr("name", "csrf"), gosx.Attr("value", csrf))),
        gosx.El("label", gosx.Text("Title"), gosx.El("input", gosx.Attrs(gosx.Attr("name", "title")))),
        gosx.El("label", gosx.Text("Brief"), gosx.El("textarea", gosx.Attrs(gosx.Attr("name", "brief")))),
        gosx.El("label", gosx.Text("Aspect"), gosx.El("select", gosx.Attrs(gosx.Attr("name", "aspect")),
            gosx.El("option", gosx.Attrs(gosx.Attr("value", "9:16")), gosx.Text("9:16")),
            gosx.El("option", gosx.Attrs(gosx.Attr("value", "16:9")), gosx.Text("16:9")),
            gosx.El("option", gosx.Attrs(gosx.Attr("value", "1:1")), gosx.Text("1:1")))),
        gosx.El("button", gosx.Attrs(gosx.Attr("type", "submit")), gosx.Text("Create storyboard")))
    page := gosx.El("html", gosx.El("head", gosx.El("meta", gosx.Attrs(gosx.Attr("charset", "utf-8"))), gosx.El("title", gosx.Text("Storyboard Desk GoSX editor"))), gosx.El("body", gosx.El("main", gosx.El("h1", gosx.Text("Storyboard Desk — GoSX editor")), gosx.El("p", gosx.Text("Experimental: shot briefs only. Save changes before creating or inserting; unsaved edits will be lost. Close the editor process before opening this data in the full workspace.")), create, gosx.Fragment(projects...))))
    return "<!doctype html>\n" + gosx.RenderHTML(page)
}

func (h *editorHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    if store, ok := h.service.(*ProjectStore); ok { (&workspaceHandler{store:store, csrf:h.csrf}).ServeHTTP(w,r); return }
    host, port, err := net.SplitHostPort(r.Host)
    if err != nil || host != "127.0.0.1" || port == "" { http.Error(w, "Use the local editor address", http.StatusForbidden); return }
    w.Header().Set("X-Content-Type-Options", "nosniff")
    w.Header().Set("Content-Security-Policy", "default-src 'none'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
    w.Header().Set("Cache-Control", "no-store")
    if r.Method == http.MethodGet && r.URL.Path == "/" {
        state, err := h.service.State(); if err != nil { http.Error(w, err.Error(), http.StatusBadGateway); return }
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        fmt.Fprint(w, editorPage(state, h.csrf)); return
    }
    if r.Method != http.MethodPost || (r.URL.Path != "/save" && r.URL.Path != "/insert" && r.URL.Path != "/create") { http.NotFound(w, r); return }
    if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host { http.Error(w, "Invalid origin", http.StatusForbidden); return }
    r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
    if err := r.ParseForm(); err != nil { http.Error(w, "Invalid form", http.StatusBadRequest); return }
    if subtle.ConstantTimeCompare([]byte(r.PostForm.Get("csrf")), []byte(h.csrf)) != 1 { http.Error(w, "Invalid token", http.StatusForbidden); return }
    if r.URL.Path == "/create" {
        aspect := r.PostForm.Get("aspect")
        if aspect != "9:16" && aspect != "16:9" && aspect != "1:1" { http.Error(w, "Invalid aspect", http.StatusBadRequest); return }
        state, err := h.service.State(); if err != nil { http.Error(w, err.Error(), http.StatusBadGateway); return }
        if err := h.service.CreateProject(state, r.PostForm.Get("title"), r.PostForm.Get("brief"), aspect); err != nil { http.Error(w, err.Error(), http.StatusBadGateway); return }
        http.Redirect(w, r, "/", http.StatusSeeOther)
        return
    }
    rev, err := strconv.Atoi(r.PostForm.Get("revision")); if err != nil { http.Error(w, "Invalid revision", http.StatusBadRequest); return }
    state, err := h.service.State(); if err != nil { http.Error(w, err.Error(), http.StatusBadGateway); return }
    id := r.PostForm.Get("project")
    found := false
    for _, p := range state.Projects { if p.ID == id { found = true; if p.Revision != rev { http.Error(w, "Project changed; reload before saving", http.StatusConflict); return }; break } }
    if !found { http.Error(w, "Project not found", http.StatusNotFound); return }
    if r.URL.Path == "/insert" {
        position := r.PostForm.Get("position")
        if position != "before" && position != "after" { http.Error(w, "Invalid position", http.StatusBadRequest); return }
        anchor := r.PostForm.Get("shot")
        foundShot := false
        for _, p := range state.Projects { if p.ID == id { for _, shot := range p.Shots { if shotText(shot, "id") == anchor { foundShot = true; break } }; break } }
        if !foundShot { http.Error(w, "Shot not found", http.StatusNotFound); return }
        if err = h.service.InsertShot(state, id, anchor, position, rev); err != nil { http.Error(w, err.Error(), http.StatusConflict); return }
        http.Redirect(w, r, "/", http.StatusSeeOther)
        return
    }
    fields := map[string]string{}
    for _, name := range []string{"title", "purpose", "composition", "action", "camera"} { fields[name] = r.PostForm.Get(name) }
    if _, err = h.service.SaveShot(state, id, r.PostForm.Get("shot"), fields); err != nil { http.Error(w, err.Error(), http.StatusConflict); return }
    http.Redirect(w, r, "/", http.StatusSeeOther)
}
