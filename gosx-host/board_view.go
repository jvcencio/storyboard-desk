package main

import (
    "encoding/json"

    "m31labs.dev/gosx"
)

// RenderBoard renders a read-only GoSX preview. It never includes the service token.
func RenderBoard(state ServiceState) string {
    projects := make([]gosx.Node, 0, len(state.Projects))
    for _, project := range state.Projects {
        shots := make([]gosx.Node, 0, len(project.Shots))
        for _, shot := range project.Shots {
            field := func(name string) string { var s string; _ = json.Unmarshal(shot[name], &s); return s }
            shots = append(shots, ShotCard(ShotCardProps{Title: field("title"), Purpose: field("purpose"), Composition: field("composition"), Action: field("action"), Camera: field("camera")}))
        }
        projects = append(projects, gosx.El("section", gosx.El("h2", gosx.Text(project.Title)), gosx.Fragment(shots...)))
    }
    page := gosx.El("html", gosx.El("head", gosx.El("meta", gosx.Attrs(gosx.Attr("charset", "utf-8"))), gosx.El("title", gosx.Text("Storyboard Desk — GoSX preview"))), gosx.El("body", gosx.El("main", gosx.El("h1", gosx.Text("Storyboard Desk")), gosx.El("p", gosx.Text("GoSX storyboard preview — read only")), gosx.Fragment(projects...))))
    return "<!doctype html>\n" + gosx.RenderHTML(page)
}
