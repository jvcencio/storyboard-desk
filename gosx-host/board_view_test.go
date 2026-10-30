package main

import (
    "encoding/json"
    "strings"
    "testing"
)

func TestRenderBoardListsProjectsAndEscapesShotFields(t *testing.T) {
    state := ServiceState{Token: "top-secret", Projects: []ServiceProject{{ID: "p1", Title: "First <project>", Shots: []map[string]json.RawMessage{{"title": json.RawMessage(`"Opening"`), "purpose": json.RawMessage(`"<script>"`), "camera": json.RawMessage(`"Wide"`)}}}, {ID: "p2", Title: "Second"}}}
    output := RenderBoard(state)
    for _, want := range []string{"First &lt;project&gt;", "Second", "Opening", "&lt;script&gt;", "Wide"} {
        if !strings.Contains(output, want) { t.Errorf("missing %q in %s", want, output) }
    }
    if strings.Contains(output, "top-secret") || strings.Contains(output, "<script>") { t.Fatal("exposed token or unescaped content") }
}
