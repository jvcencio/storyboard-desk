package main

import (
    "encoding/json"
    "os"
    "strings"
    "testing"
)

// Invoked against the real Python server by test_gosx_bridge.py.
func TestPythonServiceRoundTrip(t *testing.T) {
    base := os.Getenv("STORYBOARD_TEST_URL")
    if base == "" { t.Skip("real service not configured") }
    c, err := NewServiceClient(base)
    if err != nil { t.Fatal(err) }
    state, err := c.State()
    if err != nil { t.Fatal(err) }
    if len(state.Projects) != 1 || len(state.Projects[0].Shots) != 1 { t.Fatalf("unexpected fixture: %+v", state.Projects) }
    var shotID string
    if err := json.Unmarshal(state.Projects[0].Shots[0]["id"], &shotID); err != nil { t.Fatal(err) }
    project, err := c.SaveShot(state, state.Projects[0].ID, shotID, map[string]string{"title":"GoSX edit"})
    if err != nil { t.Fatal(err) }
    if project.Revision != state.Projects[0].Revision+1 { t.Fatalf("unexpected revision: %d", project.Revision) }
    latest, err := c.State()
    if err != nil { t.Fatal(err) }
    var title, composition string
    _ = json.Unmarshal(latest.Projects[0].Shots[0]["title"], &title)
    _ = json.Unmarshal(latest.Projects[0].Shots[0]["composition"], &composition)
    if title != "GoSX edit" || composition != "Keep the framing" { t.Fatalf("unexpected shot: %q %q", title, composition) }
    _, err = c.SaveShot(state, state.Projects[0].ID, shotID, map[string]string{"title":"stale"})
    if err == nil || !strings.Contains(err.Error(), "changed") { t.Fatalf("expected stale revision error: %v", err) }
}
