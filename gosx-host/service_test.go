package main

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"
)

func TestServiceClientLoadsAndSavesShotWithoutLosingFields(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        switch r.URL.Path {
        case "/api/state":
            w.Header().Set("Content-Type", "application/json")
            w.Write([]byte(`{"app":"Storyboard Desk","token":"secret","projects":[{"id":"p1","revision":3,"shots":[{"id":"s1","title":"Before","purpose":"Old","composition":"Keep me","future_field":{"x":1}},{"id":"s2","title":"Second","camera":"Wide"}]}]}`))
        case "/api/save":
            if r.Method != "POST" || r.Header.Get("X-Storyboard-Token") != "secret" { t.Errorf("missing authenticated POST") }
            var body struct { ID string `json:"id"`; Revision int `json:"revision"`; Shots []map[string]json.RawMessage `json:"shots"` }
            if err := json.NewDecoder(r.Body).Decode(&body); err != nil { t.Fatal(err) }
            if body.ID != "p1" || body.Revision != 3 || len(body.Shots) != 2 { t.Fatalf("bad save: %+v", body) }
            if string(body.Shots[0]["title"]) != `"After"` || string(body.Shots[0]["composition"]) != `"Keep me"` || string(body.Shots[0]["future_field"]) != `{"x":1}` || string(body.Shots[1]["camera"]) != `"Wide"` { t.Fatalf("lost shot data: %v", body.Shots) }
            w.Write([]byte(`{"project":{"id":"p1","revision":4,"shots":[]}}`))
        default: t.Fatalf("unexpected route %s", r.URL.Path)
        }
    }))
    defer server.Close()
    c, err := NewServiceClient(server.URL + "/")
    if err != nil { t.Fatal(err) }
    state, err := c.State()
    if err != nil { t.Fatal(err) }
    updated, err := c.SaveShot(state, "p1", "s1", map[string]string{"title":"After"})
    if err != nil { t.Fatal(err) }
    if updated.Revision != 4 { t.Fatalf("wrong revision: %d", updated.Revision) }
}

func TestServiceClientSurfacesConflict(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusConflict)
        w.Write([]byte(`{"error":"Project changed in another window. Reload before saving."}`))
    }))
    defer server.Close()
    c, err := NewServiceClient(server.URL + "/")
    if err != nil { t.Fatal(err) }
    state := ServiceState{Token:"token", Projects: []ServiceProject{{ID:"p", Revision:1, Shots: []map[string]json.RawMessage{{"id":json.RawMessage(`"s"`)}}}}}
    _, err = c.SaveShot(state, "p", "s", map[string]string{"title":"New"})
    if err == nil || !strings.Contains(err.Error(), "Reload before saving") { t.Fatalf("expected conflict, got %v", err) }
}

func TestServiceClientRejectsRemoteAndRedirects(t *testing.T) {
    if _, err := NewServiceClient("https://example.com/"); err == nil { t.Fatal("accepted remote backend") }
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "https://example.com/", http.StatusFound) }))
    defer server.Close()
    c, err := NewServiceClient(server.URL + "/")
    if err != nil { t.Fatal(err) }
    if _, err := c.State(); err == nil { t.Fatal("followed redirect") }
}
