package main

import (
	"fmt"
	"net/url"
	"strconv"

	"m31labs.dev/gosx"
)

func formDocument(values url.Values) (ProjectDocument, error) {
	p := ProjectDocument{}
	numeric := []string{"start", "end", "start_seconds", "end_seconds", "part_seconds", "in_seconds", "out_seconds", "duration_seconds", "delta"}
	for k, v := range values {
		if len(v) == 0 {
			continue
		}
		if containsField(numeric, k) {
			if v[0] == "" {
				continue
			}
			n, err := strconv.ParseFloat(v[0], 64)
			if err != nil || !finite(n) {
				return nil, fmt.Errorf("enter a valid finite time")
			}
			p[k] = rawValue(n)
		} else {
			p[k] = rawValue(v[0])
		}
	}
	return p, nil
}
func (h *workspaceHandler) musicCommand(path, id string, rev int, values url.Values) (ProjectDocument, string, error) {
	raw, err := formDocument(values)
	if err != nil {
		return nil, "", err
	}
	sid := values.Get("section")
	switch path {
	case "/music-cue":
		var end *float64
		if values.Get("end_seconds") != "" {
			n := number(raw, "end_seconds")
			end = &n
		}
		p, err := h.store.MusicCue(id, rev, values.Get("label"), number(raw, "start_seconds"), end, values.Get("cue"), values.Get("remove") == "true")
		return p, "", err
	case "/section-save":
		raw["id"] = rawValue(sid)
		p, err := h.store.SaveSection(id, rev, raw)
		return p, "", err
	case "/section-split":
		p, err := h.store.SplitSections(id, rev, number(raw, "start_seconds"), number(raw, "end_seconds"), number(raw, "part_seconds"), values.Get("prefix"), values.Get("kind"))
		return p, "", err
	case "/coverage-add":
		raw["operation"] = rawValue("add")
		p, err := h.store.SectionEdit(id, sid, rev, raw, true)
		return p, "", err
	case "/coverage-edit":
		if values.Get("operation") == "update" {
			raw["exclusive"], raw["include_in_handoff"] = rawValue(values.Get("exclusive") == "true"), rawValue(values.Get("include_in_handoff") == "true")
		}
		p, err := h.store.SectionEdit(id, sid, rev, raw, true)
		return p, "", err
	case "/section-edit":
		p, err := h.store.SectionEdit(id, sid, rev, raw, false)
		return p, "", err
	case "/section-undo":
		p, err := h.store.UndoSection(id, rev)
		return p, "", err
	case "/section-reel":
		rel, err := h.store.ExportReel(id, sid, rev, values.Get("scope"))
		return nil, rel, err
	case "/section-export":
		rel, err := h.store.ExportSection(id, sid, rev, values.Get("include_coverage") == "true")
		return nil, rel, err
	}
	return nil, "", fmt.Errorf("unknown music operation")
}
func (w workspacePage) sectionSelector(name string) gosx.Node {
	options := []any{attrs("name", name)}
	for _, s := range list(w.project, "music_sections") {
		options = append(options, el("option", attrs("value", shotText(s, "id")), txt(shotText(s, "label"))))
	}
	return el("label", txt("Song section"), el("select", options...))
}
func (w workspacePage) music() gosx.Node {
	track := object(w.project, "soundtrack")
	content := []any{el("h2", txt("Music & lyrics")), el("p", txt("Keep the original PCM WAV as the soundtrack. Footage is reviewed and exported as silent picture.")), w.uploadForm("soundtrack")}
	if track != nil {
		timing := musicTiming(w.project)
		content = append(content, el("h3", txt(shotText(track, "original_name"))), el("audio", attrs("controls", "controls", "preload", "metadata", "src", mediaURL(shotText(w.project, "id"), shotText(track, "path")))), el("p", txt(fmt.Sprintf("Song %.1fs · planned %.1fs · covered %.1fs · gaps %.1fs", number(timing, "track_seconds"), number(timing, "planned_seconds"), number(timing, "covered_seconds"), number(timing, "gap_seconds")))))
		cue := w.controls("/music-cue")
		cue = append(cue, w.field("/music-cue", "Cue / lyric label", "label", "", "text"), w.field("/music-cue", "Song start (seconds)", "start_seconds", "0", "number"), w.field("/music-cue", "Song end · optional", "end_seconds", "", "number"), button("Add song cue"))
		content = append(content, el("h3", txt("Cues on the original song")), el("form", cue...))
		for _, c := range list(w.project, "music_cues") {
			if shotText(c, "track_sha256") != shotText(track, "sha256") {
				continue
			}
			f := w.controls("/music-cue")
			f = append(f, hidden("cue", shotText(c, "id")), hidden("remove", "true"), el("span", txt(fmt.Sprintf("%s · %.3f–%.3fs", shotText(c, "label"), number(c, "seconds"), max(number(c, "seconds"), number(c, "end_seconds"))))), button("Remove cue"))
			content = append(content, el("form", f...))
		}
	}
	f := w.controls("/project")
	f = append(f, hidden("music_video", "true"), w.field("/project", "Exact original lyrics", "lyrics", shotText(w.project, "lyrics"), "textarea"), button("Save lyrics"))
	content = append(content, el("form", f...))
	return cls("section", "paper", content...)
}
func (w workspacePage) sections() gosx.Node {
	prepareCoverage(w.project)
	content := []any{el("h2", txt("Song sections")), el("p", txt("Collect alternatives separately from a rough assembly. Preferred ranges can be longer than the song section; finish the musical edit in your editor."))}
	f := w.controls("/section-save")
	f = append(f, w.field("/section-save", "Section label", "label", "", "text"), w.selectField("/section-save", "Musical tag", "kind", "none", musicKinds), w.field("/section-save", "Song start (s)", "start_seconds", "0", "number"), w.field("/section-save", "Song end (s)", "end_seconds", "", "number"), w.field("/section-save", "Section notes", "note", "", "textarea"), button("Create song section"))
	content = append(content, cls("div", "paper", el("form", f...)))
	f = w.controls("/section-split")
	f = append(f, w.field("/section-split", "Label prefix", "prefix", "Part", "text"), w.field("/section-split", "Range start", "start_seconds", "0", "number"), w.field("/section-split", "Range end", "end_seconds", "", "number"), w.field("/section-split", "Seconds per portion", "part_seconds", "15", "number"), w.selectField("/section-split", "Initial tag", "kind", "none", musicKinds), button("Create working portions"))
	content = append(content, el("details", attrs("class", "paper"), el("summary", txt("Split a time range into portions")), el("form", f...)))
	undo := w.controls("/section-undo")
	undo = append(undo, button("Undo last section edit"))
	content = append(content, el("form", undo...))
	for _, s := range list(w.project, "music_sections") {
		content = append(content, w.sectionCard(s))
	}
	return el("section", content...)
}
func (w workspacePage) sectionCard(s ProjectDocument) gosx.Node {
	w = w.scoped("section", shotText(s, "id"))
	sid := shotText(s, "id")
	content := []any{el("h2", txt(shotText(s, "label"))), el("p", txt(fmt.Sprintf("%.3f–%.3fs · %s", number(s, "start_seconds"), number(s, "end_seconds"), shotText(s, "kind"))))}
	edit := w.controls("/section-save")
	edit = append(edit, hidden("section", sid), w.field("/section-save", "Label", "label", shotText(s, "label"), "text"), w.selectField("/section-save", "Tag", "kind", shotText(s, "kind"), musicKinds), w.field("/section-save", "Song start", "start_seconds", fmt.Sprint(number(s, "start_seconds")), "number"), w.field("/section-save", "Song end", "end_seconds", fmt.Sprint(number(s, "end_seconds")), "number"), w.field("/section-save", "Story scene for reservations", "story_scene", shotText(s, "story_scene"), "text"), w.field("/section-save", "Section note", "note", shotText(s, "note"), "textarea"), button("Save section"))
	content = append(content, el("details", el("summary", txt("Section timing & notes")), el("form", edit...)))
	for _, kind := range []string{"coverage", "clips"} {
		heading := "Coverage options"
		if kind == "clips" {
			heading = "Rough assembly"
		}
		content = append(content, el("h3", txt(heading)))
		for _, e := range list(s, kind) {
			ss, err := findShot(w.project, shotText(e, "source_shot"))
			if err != nil {
				continue
			}
			a, err := findAsset(ss, shotText(e, "source_asset"))
			if err != nil {
				continue
			}
			c := w
			c.shot = ss
			action, scope := "/coverage-edit", "coverage"
			if kind == "clips" {
				action, scope = "/section-edit", "entry"
			}
			c = c.scoped(scope, shotText(e, "id"))
			f := c.controls(action)
			f = append(f, hidden("section", sid), hidden("coverage", shotText(e, "id")), hidden("entry", shotText(e, "id")), hidden("operation", "update"), c.field(action, "Source in", "in_seconds", fmt.Sprint(number(e, "in_seconds")), "number"), c.field(action, "Source out", "out_seconds", fmt.Sprint(number(e, "out_seconds")), "number"))
			if kind == "coverage" {
				f = append(f, c.selectField(action, "Musical tag", "music_tag", shotText(e, "music_tag"), musicKinds), c.field(action, "Angle", "angle", shotText(e, "angle"), "text"), c.field(action, "Option note", "note", shotText(e, "note"), "textarea"), c.field(action, "Reserve for this section", "exclusive", strconv.FormatBool(boolean(e, "exclusive")), "checkbox"), c.field(action, "Include in handoff", "include_in_handoff", strconv.FormatBool(!hasFalse(e, "include_in_handoff")), "checkbox"))
			}
			f = append(f, button("Save source option"))
			move := w.controls("/coverage-edit")
			if kind == "clips" {
				move = w.controls("/section-edit")
			}
			move = append(move, hidden("section", sid), hidden("coverage", shotText(e, "id")), hidden("entry", shotText(e, "id")), hidden("operation", "move"), el("button", attrs("name", "delta", "value", "-1"), txt("Earlier")), el("button", attrs("name", "delta", "value", "1"), txt("Later")))
			remove := w.controls("/coverage-edit")
			if kind == "clips" {
				remove = w.controls("/section-edit")
			}
			remove = append(remove, hidden("section", sid), hidden("coverage", shotText(e, "id")), hidden("entry", shotText(e, "id")), hidden("operation", "remove"), button("Remove range"))
			content = append(content, cls("article", "paper take-card", el("h3", txt(shotText(ss, "title")+" · "+shotText(e, "angle"))), c.assetPreview(a), el("form", f...), el("form", move...), el("form", remove...)))
		}
	}
	reel := w.controls("/section-reel")
	reel = append(reel, hidden("section", sid), w.selectField("/section-reel", "Reel source ranges", "scope", "preferred", []string{"preferred", "coverage", "assembly"}), button("Export silent selects reel MP4"))
	pack := w.controls("/section-export")
	pack = append(pack, hidden("section", sid), w.field("/section-export", "Include full coverage options", "include_coverage", "true", "checkbox"), button("Export editor package ZIP"))
	content = append(content, w.sectionPreview(s), el("form", reel...), el("form", pack...))
	return cls("section", "paper", content...)
}
func hasFalse(p ProjectDocument, k string) bool { _, ok := p[k]; return ok && !boolean(p, k) }
func (w workspacePage) collectForm(a ProjectDocument) gosx.Node {
	f := w.controls("/coverage-add")
	f = append(f, hidden("source_shot", shotText(w.shot, "id")), hidden("source_asset", shotText(a, "id")), hidden("duration_seconds", fmt.Sprint(number(a, "duration_seconds"))), w.sectionSelector("section"), w.field("/coverage-add", "Preferred source in", "in_seconds", fmt.Sprint(number(a, "in_seconds")), "number"), w.field("/coverage-add", "Preferred source out", "out_seconds", fmt.Sprint(rangeEnd(a)), "number"), button("Collect in coverage bin"))
	assembly := w.controls("/section-edit")
	assembly = append(assembly, hidden("operation", "add"), hidden("source_shot", shotText(w.shot, "id")), hidden("source_asset", shotText(a, "id")), hidden("duration_seconds", fmt.Sprint(number(a, "duration_seconds"))), hidden("position", "end"), w.sectionSelector("section"), w.field("/section-edit", "Source in", "in_seconds", fmt.Sprint(number(a, "in_seconds")), "number"), w.field("/section-edit", "Source out", "out_seconds", fmt.Sprint(rangeEnd(a)), "number"), button("Append to rough assembly"))
	return el("details", el("summary", txt("Collect for a song section")), el("form", f...), el("form", assembly...))
}
