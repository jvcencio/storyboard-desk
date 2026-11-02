package main

import (
    "net/http"
    "net/http/httptest"
    "net/url"
    "os"
    "strconv"
    "strings"
    "testing"
)

// Run separately so this test's insertion cannot affect other real-service tests.
func TestPythonInsertRoundTrip(t *testing.T) {
    if os.Getenv("STORYBOARD_INSERT_TEST") != "1" { t.Skip("isolated insert fixture not configured") }
    client, err := NewServiceClient(os.Getenv("STORYBOARD_TEST_URL")); if err != nil { t.Fatal(err) }
    state, err := client.State(); if err != nil { t.Fatal(err) }
    project := state.Projects[0]
    anchor := shotText(project.Shots[0], "id")
    form := url.Values{"csrf":{"test-csrf"}, "project":{project.ID}, "shot":{anchor}, "revision":{strconv.Itoa(project.Revision)}, "position":{"before"}}
    request := func() *httptest.ResponseRecorder {
        req := httptest.NewRequest("POST", "http://127.0.0.1:1234/insert", strings.NewReader(form.Encode()))
        req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
        rec := httptest.NewRecorder(); newEditorHandler(client, "test-csrf").ServeHTTP(rec, req); return rec
    }
    if rec := request(); rec.Code != http.StatusSeeOther { t.Fatalf("insert failed: %d %s", rec.Code, rec.Body.String()) }
    saved, err := client.State(); if err != nil { t.Fatal(err) }
    shots := saved.Projects[0].Shots
    if len(shots) != 2 || shotText(shots[1], "id") != anchor || shotText(shots[0], "title") != "A new beat" { t.Fatalf("incorrect inserted order: %+v", shots) }
    if rec := request(); rec.Code != http.StatusConflict { t.Fatalf("stale form accepted: %d", rec.Code) }
    if err := client.InsertShot(state, project.ID, anchor, "after", project.Revision); err == nil || !strings.Contains(err.Error(), "changed") { t.Fatalf("backend accepted stale revision: %v", err) }
    final, err := client.State(); if err != nil { t.Fatal(err) }
    if len(final.Projects[0].Shots) != 2 { t.Fatal("stale request inserted another shot") }
}
