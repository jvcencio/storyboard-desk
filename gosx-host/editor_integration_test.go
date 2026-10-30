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

func TestPythonEditorRoundTrip(t *testing.T) {
    base := os.Getenv("STORYBOARD_TEST_URL")
    if base == "" { t.Skip("real service not configured") }
    client, err := NewServiceClient(base)
    if err != nil { t.Fatal(err) }
    state, err := client.State()
    if err != nil { t.Fatal(err) }
    project := state.Projects[0]
    shot := project.Shots[0]
    form := url.Values{
        "csrf": {"test-csrf"}, "project": {project.ID}, "shot": {shotText(shot, "id")},
        "revision": {strconv.Itoa(project.Revision)}, "title": {"Edited via GoSX"},
        "purpose": {"Real editor"}, "composition": {"Keep the framing"}, "action": {""}, "camera": {""},
    }
    req := httptest.NewRequest("POST", "http://127.0.0.1:1234/save", strings.NewReader(form.Encode()))
    req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
    response := httptest.NewRecorder()
    newEditorHandler(client, "test-csrf").ServeHTTP(response, req)
    if response.Code != http.StatusSeeOther { t.Fatalf("editor save failed: %d %s", response.Code, response.Body.String()) }
    saved, err := client.State()
    if err != nil { t.Fatal(err) }
    if got := shotText(saved.Projects[0].Shots[0], "title"); got != "Edited via GoSX" { t.Fatalf("editor title: %q", got) }
}
