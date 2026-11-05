package main

import (
    "strings"
    "testing"

    "m31labs.dev/gosx"
)

func TestShotCardRendersAndEscapesProjectContent(t *testing.T) {
    html := gosx.RenderHTML(ShotCard(ShotCardProps{Title: `<script>alert("x")</script>`, Purpose: "Set the scene", Camera: "Wide"}))
    for _, want := range []string{"&lt;script&gt;", "Set the scene", "Wide"} {
        if !strings.Contains(html, want) { t.Errorf("missing %q in %s", want, html) }
    }
    if strings.Contains(html, "<script>") { t.Fatal("project content was not escaped") }
}
