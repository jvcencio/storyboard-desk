package main

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "net/url"
    "strings"
    "testing"
)

func TestEditorInsertShot(t *testing.T) {
    inserts := 0
    backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        switch r.URL.Path {
        case "/api/state":
            w.Write([]byte(`{"app":"Storyboard Desk","token":"backend-token","projects":[{"id":"p","title":"Project","revision":7,"shots":[{"id":"s","title":"Opening"}]}]}`))
        case "/api/insert":
            inserts++
            if r.Header.Get("X-Storyboard-Token") != "backend-token" { t.Error("missing backend token") }
            var body struct { Project string `json:"project"`; Anchor string `json:"anchor"`; Position string `json:"position"`; Revision int `json:"revision"` }
            if err := json.NewDecoder(r.Body).Decode(&body); err != nil { t.Fatal(err) }
            if body.Project != "p" || body.Anchor != "s" || body.Position != "after" || body.Revision != 7 { t.Errorf("bad insert payload: %+v", body) }
            w.Write([]byte(`{"project":{"id":"p","revision":8},"shot_id":"new"}`))
        default: t.Errorf("unexpected backend route %s", r.URL.Path)
        }
    }))
    defer backend.Close()
    client, err := NewServiceClient(backend.URL); if err != nil { t.Fatal(err) }
    handler := newEditorHandler(client, "csrf-secret")
    page := httptest.NewRecorder()
    handler.ServeHTTP(page, httptest.NewRequest("GET", "http://127.0.0.1:1234/", nil))
    for _, want := range []string{`action="/insert"`, `value="before"`, `value="after"`} {
        if !strings.Contains(page.Body.String(), want) { t.Errorf("page missing %s", want) }
    }
    form := url.Values{"csrf":{"csrf-secret"}, "project":{"p"}, "shot":{"s"}, "revision":{"7"}, "position":{"after"}}
    post := func(values url.Values, origin string) *httptest.ResponseRecorder {
        req := httptest.NewRequest("POST", "http://127.0.0.1:1234/insert", strings.NewReader(values.Encode()))
        req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
        if origin != "" { req.Header.Set("Origin", origin) }
        rec := httptest.NewRecorder(); handler.ServeHTTP(rec, req); return rec
    }
    changed := func(key, value string) url.Values { clone := url.Values{}; for k, v := range form { clone[k] = append([]string(nil), v...) }; clone.Set(key, value); return clone }
    for _, test := range []struct { values url.Values; origin string; want int } {
        {changed("csrf", "bad"), "", http.StatusForbidden},
        {form, "http://evil.example", http.StatusForbidden},
        {changed("revision", "6"), "", http.StatusConflict},
        {changed("position", "elsewhere"), "", http.StatusBadRequest},
        {changed("shot", "missing"), "", http.StatusNotFound},
    } {
        rec := post(test.values, test.origin)
        if rec.Code != test.want { t.Errorf("expected %d got %d: %s", test.want, rec.Code, rec.Body.String()) }
    }
    if inserts != 0 { t.Fatal("invalid requests reached insert endpoint") }
    rec := post(form, "http://127.0.0.1:1234")
    if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" { t.Fatalf("insert failed: %d %s", rec.Code, rec.Body.String()) }
    if inserts != 1 { t.Fatalf("expected one insertion, got %d", inserts) }
}
