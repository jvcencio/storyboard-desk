package main

import (
	"fmt"
	"m31labs.dev/gosx"
)

func (w workspacePage) review() gosx.Node {
	rows := []any{}
	elapsed := 0.0
	for _, s := range w.shots {
		a := selectedAsset(s, "clip", "keyframe", "sketch")
		kind := "gap"
		available := number(s, "duration")
		var preview gosx.Node
		if a == nil {
			preview = cls("div", "review-gap", el("h2", txt("No selected take")), el("p", txt("Choose media for this shot before continuing the sequence.")))
		} else {
			kind = shotText(a, "kind")
			if kind == "clip" {
				available = max(0, rangeEnd(a)-number(a, "in_seconds"))
			}
			preview = w.reviewPreview(a)
		}
		rows = append(rows, cls("article", "review-shot", attrs("data-review-shot", "true", "data-duration", fmt.Sprint(number(s, "duration")), "data-song-start", fmt.Sprint(elapsed), "data-start", fmt.Sprint(number(a, "in_seconds")), "data-available", fmt.Sprint(available), "data-kind", kind), el("h2", txt(shotText(s, "title"))), preview, el("p", txt(shotText(s, "purpose"))), link("Choose a take ↗", w.href("library", shotText(s, "id")), "button secondary")))
		elapsed += number(s, "duration")
	}
	return w.sequenceReview("Review the selected sequence", "Playback follows storyboard order and target durations, stops at missing footage, and never stretches a source take. Clips are muted; the original song supplies music.", rows)
}
func (w workspacePage) reviewPreview(a ProjectDocument) gosx.Node {
	if shotText(a, "kind") == "clip" {
		return el("video", attrs("src", mediaURL(shotText(w.project, "id"), shotText(a, "path")), "muted", "muted", "playsinline", "playsinline", "preload", "metadata"))
	}
	return w.assetPreview(a)
}
func (w workspacePage) sequenceReview(title, description string, rows []any) gosx.Node {
	content := []any{attrs("data-review", "true"), el("h2", txt(title)), el("p", txt(description)), el("div", attrs("class", "button-row"), el("button", attrs("type", "button", "data-review-prev", "true"), txt("← Previous")), el("button", attrs("type", "button", "data-review-play", "true"), txt("Play / pause")), el("button", attrs("type", "button", "data-review-next", "true"), txt("Next →"))), el("p", attrs("role", "status", "data-review-status", "true"), txt("Ready to review"))}
	if track := object(w.project, "soundtrack"); track != nil && boolean(w.project, "music_video") {
		content = append(content, el("audio", attrs("data-review-audio", "true", "preload", "metadata", "src", mediaURL(shotText(w.project, "id"), shotText(track, "path")))))
	}
	content = append(content, fragment(rows...))
	return cls("section", "paper sequence-review", content...)
}

func (w workspacePage) sectionPreview(section ProjectDocument) gosx.Node {
	rows := []any{}
	start, end := number(section, "start_seconds"), number(section, "end_seconds")
	clock := start
	for _, entry := range list(section, "clips") {
		if clock >= end {
			break
		}
		duration := min(end-clock, number(entry, "out_seconds")-number(entry, "in_seconds"))
		if duration <= 0 {
			continue
		}
		kind, available := "gap", 0.0
		preview := cls("div", "review-gap", txt("This assembly source is unavailable."))
		if shot, err := findShot(w.project, shotText(entry, "source_shot")); err == nil {
			if asset, err := findAsset(shot, shotText(entry, "source_asset")); err == nil && shotText(asset, "binned_at") == "" {
				preview = w.reviewPreview(asset)
				kind = "clip"
				available = max(0, min(number(entry, "out_seconds"), number(asset, "duration_seconds"))-number(entry, "in_seconds"))
			}
		}
		rows = append(rows, cls("article", "review-shot", attrs("data-review-shot", "true", "data-duration", fmt.Sprint(duration), "data-song-start", fmt.Sprint(clock), "data-start", fmt.Sprint(number(entry, "in_seconds")), "data-available", fmt.Sprint(available), "data-kind", kind), preview))
		clock += duration
	}
	if clock < end {
		rows = append(rows, cls("article", "review-shot", attrs("data-review-shot", "true", "data-duration", fmt.Sprint(end-clock), "data-song-start", fmt.Sprint(clock), "data-kind", "gap"), cls("div", "review-gap", txt(fmt.Sprintf("The assembly leaves %.2f seconds uncovered.", end-clock)))))
	}
	return el("details", el("summary", txt("Preview the rough assembly with the song")), w.sequenceReview(shotText(section, "label"), "Preview stops at the section end or missing footage. Sources play at their original speed. Exports retain the full chosen ranges.", rows))
}
