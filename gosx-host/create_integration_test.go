package main

import (
    "net/http"
    "net/http/httptest"
    "net/url"
    "os"
    "strings"
    "testing"
)

// Run separately so the new project does not change other shared-service fixtures.
func TestPythonEditorCreatesProject(t *testing.T) {
    base := os.Getenv("STORYBOARD_CREATE_TEST_URL")
    if base == "" { t.Skip("creation service not configured") }
    client, err := NewServiceClient(base)
    if err != nil { t.Fatal(err) }
    form := url.Values{"csrf":{"csrf"}, "title":{"GoSX story"}, "brief":{"A new idea"}, "aspect":{"16:9"}}
    req := httptest.NewRequest("POST", "http://127.0.0.1:1234/create", strings.NewReader(form.Encode()))
    req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
    rec := httptest.NewRecorder()
    newEditorHandler(client, "csrf").ServeHTTP(rec, req)
    if rec.Code != http.StatusSeeOther { t.Fatalf("create: %d %s", rec.Code, rec.Body.String()) }
    state, err := client.State()
    if err != nil { t.Fatal(err) }
    for _, project := range state.Projects {
        if project.Title != "GoSX story" { continue }
        if len(project.Shots) != 1 || shotText(project.Shots[0], "title") != "The opening" || shotText(project.Shots[0], "purpose") != "Establish the story." {
            t.Fatalf("incorrect initial shot: %+v", project.Shots)
        }
        return
    }
    t.Fatal("created project not found")
}
