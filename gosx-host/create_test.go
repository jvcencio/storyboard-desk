package main

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "net/url"
    "strings"
    "testing"
)

func TestEditorCreatesProject(t *testing.T) {
    creates := 0
    backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        switch r.URL.Path {
        case "/api/state":
            w.Write([]byte(`{"app":"Storyboard Desk","token":"backend-secret","projects":[]}`))
        case "/api/new":
            creates++
            if r.Header.Get("X-Storyboard-Token") != "backend-secret" { t.Error("backend token missing") }
            var body struct {
                Title string `json:"title"`
                Brief string `json:"brief"`
                Aspect string `json:"aspect"`
                Shots []struct { Title string `json:"title"`; Duration int `json:"duration"` } `json:"shots"`
            }
            if err := json.NewDecoder(r.Body).Decode(&body); err != nil { t.Fatal(err) }
            if body.Title != "My story" || body.Brief != "A brief" || body.Aspect != "16:9" || len(body.Shots) != 1 || body.Shots[0].Title != "The opening" || body.Shots[0].Duration != 5 { t.Errorf("bad payload: %+v", body) }
            w.Write([]byte(`{"project":{"id":"new-project"}}`))
        default:
            t.Errorf("unexpected route: %s", r.URL.Path)
        }
    }))
    defer backend.Close()
    client, err := NewServiceClient(backend.URL); if err != nil { t.Fatal(err) }
    handler := newEditorHandler(client, "csrf-secret")
    page := httptest.NewRecorder()
    handler.ServeHTTP(page, httptest.NewRequest("GET", "http://127.0.0.1:1234/", nil))
    for _, want := range []string{`action="/create"`, `name="title"`, `name="aspect"`} {
        if !strings.Contains(page.Body.String(), want) { t.Errorf("page missing %s", want) }
    }
    form := url.Values{"csrf":{"csrf-secret"}, "title":{"My story"}, "brief":{"A brief"}, "aspect":{"16:9"}}
    post := func(values url.Values, origin string) *httptest.ResponseRecorder {
        req := httptest.NewRequest("POST", "http://127.0.0.1:1234/create", strings.NewReader(values.Encode()))
        req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
        if origin != "" { req.Header.Set("Origin", origin) }
        rec := httptest.NewRecorder(); handler.ServeHTTP(rec, req); return rec
    }
    bad := url.Values{"csrf":{"wrong"}, "title":{"My story"}, "aspect":{"16:9"}}
    if r := post(bad, ""); r.Code != http.StatusForbidden { t.Errorf("bad CSRF: %d", r.Code) }
    if r := post(form, "http://evil.example"); r.Code != http.StatusForbidden { t.Errorf("bad origin: %d", r.Code) }
    bad = url.Values{"csrf":{"csrf-secret"}, "aspect":{"invalid"}}
    if r := post(bad, ""); r.Code != http.StatusBadRequest { t.Errorf("bad aspect: %d", r.Code) }
    if creates != 0 { t.Fatal("invalid request reached backend") }
    if r := post(form, "http://127.0.0.1:1234"); r.Code != http.StatusSeeOther || r.Header().Get("Location") != "/" { t.Fatalf("create failed: %d %s", r.Code, r.Body.String()) }
    if creates != 1 { t.Fatalf("created %d projects", creates) }
}
