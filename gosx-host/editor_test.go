package main

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "net/url"
    "strings"
    "testing"
)

func TestEditorRendersAndSavesWithRevisionAndCSRF(t *testing.T) {
    saves := 0
    backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        switch r.URL.Path {
        case "/api/state":
            w.Write([]byte(`{"app":"Storyboard Desk","token":"backend-secret","projects":[{"id":"p","title":"Project","revision":3,"shots":[{"id":"s","title":"Before","purpose":"Original","future":{"x":1}}]}]}`))
        case "/api/save":
            saves++
            if r.Header.Get("X-Storyboard-Token") != "backend-secret" { t.Error("missing backend token") }
            var body struct { Revision int `json:"revision"`; Shots []map[string]json.RawMessage `json:"shots"` }
            if err := json.NewDecoder(r.Body).Decode(&body); err != nil { t.Fatal(err) }
            if body.Revision != 3 || string(body.Shots[0]["future"]) != `{"x":1}` || string(body.Shots[0]["title"]) != `"After"` { t.Errorf("bad payload: %+v", body) }
            w.Write([]byte(`{"project":{"id":"p","revision":4}}`))
        default: t.Errorf("unexpected route: %s", r.URL.Path)
        }
    }))
    defer backend.Close()
    c, err := NewServiceClient(backend.URL + "/"); if err != nil { t.Fatal(err) }
    handler := newEditorHandler(c, "csrf-secret")
    get := httptest.NewRecorder()
    handler.ServeHTTP(get, httptest.NewRequest("GET", "http://127.0.0.1:1234/", nil))
    if get.Code != 200 || !strings.Contains(get.Body.String(), "Before") || !strings.Contains(get.Body.String(), `name="csrf"`) { t.Fatalf("bad page: %d %s", get.Code, get.Body.String()) }
    if strings.Contains(get.Body.String(), "backend-secret") { t.Fatal("exposed backend token") }
    form := url.Values{"csrf":{"csrf-secret"}, "project":{"p"}, "shot":{"s"}, "revision":{"3"}, "title":{"After"}, "purpose":{"New"}, "composition":{""}, "action":{""}, "camera":{""}}
    post := func(v url.Values, origin string) *httptest.ResponseRecorder {
        req := httptest.NewRequest("POST", "http://127.0.0.1:1234/save", strings.NewReader(v.Encode()))
        req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
        if origin != "" { req.Header.Set("Origin", origin) }
        r := httptest.NewRecorder(); handler.ServeHTTP(r, req); return r
    }
    bad := url.Values{}; for k, v := range form { bad[k] = v }; bad.Set("csrf", "wrong")
    if r := post(bad, ""); r.Code != http.StatusForbidden { t.Fatalf("bad CSRF accepted: %d", r.Code) }
    if r := post(form, "http://evil.example"); r.Code != http.StatusForbidden { t.Fatalf("bad origin accepted: %d", r.Code) }
    stale := url.Values{}; for k, v := range form { stale[k] = v }; stale.Set("revision", "2")
    if r := post(stale, ""); r.Code != http.StatusConflict { t.Fatalf("stale revision accepted: %d", r.Code) }
    if saves != 0 { t.Fatal("unauthorized save reached backend") }
    if r := post(form, "http://127.0.0.1:1234"); r.Code != http.StatusSeeOther { t.Fatalf("save failed: %d %s", r.Code, r.Body.String()) }
    if saves != 1 { t.Fatalf("got %d saves", saves) }
}
