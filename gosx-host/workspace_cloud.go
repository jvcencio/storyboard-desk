package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"m31labs.dev/gosx"
)

func (h *workspaceHandler) cloudCommand(rw http.ResponseWriter, r *http.Request, id string, rev int, values url.Values) (ProjectDocument, error) {
	cloud, err := h.store.Cloud()
	if err != nil {
		return nil, err
	}
	switch r.URL.Path {
	case "/cloud-connect":
		err = cloud.Connect(id)
	case "/cloud-allowance":
		var n float64
		n, err = strconv.ParseFloat(values.Get("allowance"), 64)
		if err == nil {
			err = cloud.Allowance(id, n)
		}
	case "/cloud-brief":
		err = cloud.CreateBrief(id)
	case "/cloud-approve-brief":
		err = cloud.ApproveBrief(id, values.Get("hash"))
	case "/cloud-prepare":
		var raw ProjectDocument
		raw, err = formDocument(values)
		if err == nil {
			if values.Has("duration") {
				var n float64
				n, err = strconv.ParseFloat(values.Get("duration"), 64)
				if err == nil && finite(n) {
					raw["duration"] = rawValue(n)
				} else {
					err = fmt.Errorf("enter a valid duration")
				}
			}
			ids := values["reference_ids"]
			if ids == nil {
				ids = []string{}
			}
			raw["reference_ids"] = rawValue(ids)
			if raw["reference_ids"] == nil {
				raw["reference_ids"] = rawValue([]string{})
			}
			if err == nil {
				var q ProjectDocument
				q, err = cloud.Prepare(id, values.Get("shot"), rev, raw)
				if err == nil {
					http.Redirect(rw, r, pageURL(id, "generate", values.Get("shot"), "")+"&quote="+url.QueryEscape(shotText(q, "id")), 303)
					return nil, nil
				}
			}
		}
	case "/cloud-submit":
		var job ProjectDocument
		job, err = cloud.Submit(values.Get("quote"), values.Get("confirmed") == "true")
		if err == nil && shotText(job, "status") == "queued" {
			_, err = cloud.Process(shotText(job, "id"))
		}
	case "/cloud-resume":
		_, err = cloud.Process(values.Get("job"))
	default:
		err = fmt.Errorf("unknown generation operation")
	}
	return nil, err
}
func (w workspacePage) generation() gosx.Node {
	if w.cloudError != "" {
		return cls("div", "error", txt(w.cloudError))
	}
	state := w.cloudState
	content := []any{el("h2", txt("Generate images & silent video")), el("p", txt("Review a production brief, set an allowance, then inspect the exact request before confirming a paid submission. New media arrives unselected.")), el("p", txt(shotText(state, "connection_message")))}
	f := w.controls("/cloud-connect")
	f = append(f, button("Check OpenRouter connection"))
	content = append(content, el("form", f...))
	f = w.controls("/cloud-allowance")
	f = append(f, w.field("/cloud-allowance", "Total project allowance (USD)", "allowance", fmt.Sprint(number(state, "allowance_usd")), "number"), button("Set allowance"))
	content = append(content, el("p", txt(fmt.Sprintf("Committed charges and reservations: $%.4f", number(state, "committed_usd")))), el("form", f...))
	f = w.controls("/cloud-brief")
	f = append(f, button("Prepare / refresh production brief"))
	content = append(content, el("form", f...))
	if brief := object(state, "brief"); brief != nil {
		f = w.controls("/cloud-approve-brief")
		hash := pythonDigest(object(brief, "content"))
		f = append(f, hidden("hash", hash), el("pre", txt(jsonText(object(brief, "content")))), button("Approve this production brief"))
		content = append(content, el("details", attrs("class", "paper"), el("summary", txt("Review production brief")), el("form", f...)))
	}
	selector := []any{attrs("method", "get"), hidden("project", shotText(w.project, "id")), hidden("view", "generate"), w.shotSelector("shot", shotText(w.shot, "id")), button("Choose shot")}
	content = append(content, el("form", selector...))
	f = w.controls("/cloud-prepare")
	f = append(f, attrs("data-cloud-form", "true"))
	f = append(f, w.selectField("/cloud-prepare", "Output", "kind", "image", []string{"image", "video"}))
	models := []any{attrs("name", "model")}
	for _, key := range []string{"image_models", "video_models"} {
		for _, m := range list(state, key) {
			models = append(models, el("option", attrs("value", shotText(m, "id"), "data-kind", strings.TrimSuffix(key, "_models")), txt(shotText(m, "name"))))
		}
	}
	f = append(f, el("label", txt("Model"), el("select", models...)), w.field("/cloud-prepare", "Exact generation prompt", "prompt", shotText(promptPackage(w.project, w.shot, "grok"), "composition"), "textarea"), w.selectField("/cloud-prepare", "Video duration", "duration", "8", []string{"4", "6", "8"}), w.selectField("/cloud-prepare", "Video resolution", "resolution", "720p", []string{"720p", "1080p", "4K"}))
	refs := []any{el("legend", txt("Image references · up to six for images; video uses the approved starter"))}
	for _, r := range cloudImages(w.project) {
		refs = append(refs, el("label", txt(shotText(r, "label")), el("input", attrs("type", "checkbox", "name", "reference_ids", "value", shotText(r, "id")))))
	}
	pkg := promptPackage(w.project, w.shot, "grok")
	f = append(f, el("textarea", attrs("data-default-image", "true", "hidden", "hidden"), txt(shotText(pkg, "composition"))), el("textarea", attrs("data-default-video", "true", "hidden", "hidden"), txt(shotText(pkg, "animation")+"\nSilent picture only. No speech, music or effects.")))
	f = append(f, el("fieldset", refs...), button("Prepare request · no paid call"))
	content = append(content, cls("div", "paper", el("h3", txt(shotText(w.shot, "title"))), el("form", f...)))
	if q := w.quote; q != nil {
		f = w.controls("/cloud-submit")
		f = append(f, hidden("quote", w.quoteID), el("h2", txt("Review this exact request")), el("p", txt(fmt.Sprintf("%s · %s · %s · reservation $%.4f", shotText(q, "kind"), shotText(q, "model"), shotText(q, "resolution"), number(q, "reserved_usd")))), el("pre", txt(shotText(q, "prompt"))), el("p", txt(shotText(q, "price_note"))))
		for _, ref := range list(q, "refs") {
			f = append(f, el("p", txt(shotText(ref, "title"))), el("img", attrs("class", "reference-image", "src", mediaURL(shotText(w.project, "id"), shotText(ref, "path")), "alt", shotText(ref, "title"))))
		}
		f = append(f, w.field("/cloud-submit", "I reviewed the exact prompt, references and reservation", "confirmed", "false", "checkbox"), button("Confirm one paid generation request"))
		content = append(content, cls("section", "paper quote", el("form", f...)))
	}
	for _, j := range list(state, "jobs") {
		f = w.controls("/cloud-resume")
		f = append(f, hidden("job", shotText(j, "id")), el("h3", txt(shotText(j, "model")+" · "+shotText(j, "status"))), el("p", txt(shotText(j, "message"))), el("p", txt(fmt.Sprintf("Reserved $%.4f · reported cost %s", number(j, "reserved_usd"), string(rawOr(j, "actual_cost_usd", nil))))))
		if shotText(j, "remote_id") != "" || shotText(j, "result_file") != "" || shotText(j, "status") == "queued" {
			f = append(f, button("Check / recover this existing job"))
		}
		content = append(content, cls("article", "paper", el("form", f...)))
	}
	return el("section", content...)
}
