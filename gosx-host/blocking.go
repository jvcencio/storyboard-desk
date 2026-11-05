package main

import "m31labs.dev/gosx"

// Code-authored SVG staging remains separate from saved, approved image media.
func blockingDiagram(shot ProjectDocument, aspect string) gosx.Node {
	kind := shotText(shot, "blocking")
	if kind == "" {
		return txt("＋")
	}
	viewBox := "0 0 640 360"
	transform := ""
	if aspect == "9:16" {
		viewBox = "0 0 360 640"
		transform = "translate(-140 160)"
	}
	person := func(x, scale string, label string) gosx.Node {
		return el("g", attrs("transform", "translate("+x+" 240) scale("+scale+")", "stroke", "#46564f", "stroke-width", "3", "fill", "#f5f2e8"), el("circle", attrs("cy", "-62", "r", "15")), el("path", attrs("d", "M-18-43 Q0-52 18-43 L24 5 H-24Z")), el("path", attrs("d", "M-16-33L-30 5M16-33L30 5M-12 6L-18 43M12 6L18 43", "fill", "none")), el("text", attrs("x", "0", "y", "-57", "text-anchor", "middle", "fill", "#46564f", "stroke", "none", "font-size", "14"), txt(label)))
	}
	art := []any{el("path", attrs("d", "M35 260H605M70 55V260M570 55V260M70 55H570M70 260L170 175H470L570 260", "fill", "none", "stroke", "#c2c4b7", "stroke-width", "2"))}
	switch kind {
	case "selfie", "reaction":
		art = append(art, el("rect", attrs("x", "175", "y", "45", "width", "290", "height", "260", "rx", "18", "stroke", "#8b978a", "fill", "none")), person("320", "1.8", "J"))
	case "mirror":
		art = append(art, el("rect", attrs("x", "220", "y", "60", "width", "210", "height", "170", "fill", "#dfe4dc", "stroke", "#8b978a")), person("325", "1.1", "J"))
	case "passenger", "help":
		art = append(art, person("215", "1", "J"), person("390", "1", "P"))
	case "portal", "gate":
		art = append(art, el("path", attrs("d", "M350 245V68Q430 22 510 68V245Z", "fill", "#e3d3b5", "stroke", "#b85934", "stroke-width", "4")), person("230", "1", "J"))
	case "pass":
		art = append(art, el("rect", attrs("x", "230", "y", "175", "width", "150", "height", "65", "transform", "rotate(-12 300 210)", "fill", "#fffdf8", "stroke", "#b85934", "stroke-width", "3")))
	default:
		art = append(art, person("230", "1", "J"), el("rect", attrs("x", "280", "y", "70", "width", "180", "height", "40", "fill", "#46564f")), el("text", attrs("x", "370", "y", "96", "text-anchor", "middle", "fill", "#fffdf6", "font-size", "14"), txt(kind)))
	}
	art = append(art, el("path", attrs("d", "M275 260H450L430 250M450 260L430 270", "fill", "none", "stroke", "#b85934", "stroke-width", "3", "stroke-dasharray", "7 5")))
	return el("svg", attrs("viewBox", viewBox, "class", "blocking-frame", "role", "img", "aria-label", "Proposed blocking diagram for "+shotText(shot, "title")), el("rect", attrs("width", "100%", "height", "100%", "fill", "#eeebdf")), el("text", attrs("x", "20", "y", "29", "fill", "#7d8578", "font-size", "12"), txt("BEAT DIAGRAM · NOT A STARTER FRAME")), el("g", attrs("transform", transform), fragment(art...)))
}
