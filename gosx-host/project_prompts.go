package main

import (
	"fmt"
	"strings"
)

func localDrawingPrompt(p, s ProjectDocument) string {
	if short := strings.TrimSpace(shotText(s, "local_prompt")); short != "" {
		return "Native " + shotText(p, "aspect") + ". " + short
	}
	return strings.TrimSpace(fmt.Sprintf("Native %s. %s\n%s\nStarting frame: %s", shotText(p, "aspect"), shotText(p, "look"), shotText(s, "composition"), shotText(s, "start_state")))
}
func promptPackage(p, s ProjectDocument, provider string) ProjectDocument {
	order := 0
	for i, shot := range list(p, "shots") {
		if shotText(shot, "id") == shotText(s, "id") {
			order = (i + 1) * 10
			break
		}
	}
	stem := fmt.Sprintf("%04d_%s", order, shotText(s, "id"))
	identity := fmt.Sprintf("PROJECT: %s\nSHOT: %s — %s\nSTORY PURPOSE: %s", shotText(p, "title"), stem, shotText(s, "title"), shotText(s, "purpose"))
	shared := fmt.Sprintf("APPEARANCE AND TREATMENT: %s\nSHARED CONTINUITY: %s\nSHOT CONTINUITY: %s\nFRAME: %s", shotText(p, "look"), shotText(p, "continuity"), shotText(s, "continuity"), shotText(p, "aspect"))
	framing := "Native " + shotText(p, "aspect") + " composition."
	if shotText(p, "aspect") == "9:16" {
		framing = "Vertical 9:16 portrait. Compose natively for the tall frame; no sideways landscape, stretching or landscape image inside bars."
	}
	visual := []string{}
	for _, k := range []string{"start_state", "screen_direction"} {
		if v := shotText(s, k); v != "" {
			visual = append(visual, strings.ToUpper(strings.ReplaceAll(k, "_", " "))+": "+v)
		}
	}
	v := strings.Join(visual, "\n")
	composition := fmt.Sprintf("%s\n%s\n%s\n%s\nCreate a single clear STARTING composition for this story beat. Do not depict a later action or ending state.", framing, shotText(s, "composition"), shared, v)
	enhancement := fmt.Sprintf("%s\n\nAttach the selected composition image and the approved appearance references.\nPreserve the composition, subject placement, pose, framing and important props in the attached sketch.\n%s\nCOMPOSITION TO PRESERVE: %s\nImprove the surface detail and apply the specified visual treatment. Keep the result suitable as the first frame of an animation.", identity, shared, shotText(s, "composition"))
	animation := fmt.Sprintf("%s\n\nAnimate the attached approved starter image.\nTARGET EDIT LENGTH: %g seconds. Choose a supported generation length in %s; trim the selected take to the target during review.\nSUBJECT ACTION AND TIMING: %s\nCAMERA: %s\nSOUND: %s\n%s\nENDING / CUT: %s\nMaintain the subject, wardrobe, objects and environment established in the starter image.", identity, number(s, "duration"), strings.ToUpper(provider[:min(1, len(provider))])+provider[min(1, len(provider)):], shotText(s, "action"), shotText(s, "camera"), shotText(s, "sound"), shared, shotText(s, "transition"))
	if v != "" {
		enhancement += "\nSTART FRAME CONTRACT\n" + v
	}
	enhancement += "\nFRAME REQUIREMENT: " + framing
	if len(list(p, "visual_references")) > 0 {
		enhancement += "\nAttach the shared character/style image files from references/ as actual visual inputs. Match the supplied identity, wardrobe, materials and visual treatment. They are appearance references, not substitutes for the shot composition."
	}
	details := []string{}
	for _, k := range shotFields[9:] {
		if !containsField([]string{"scene", "story_status", "blocking", "local_prompt"}, k) && shotText(s, k) != "" {
			details = append(details, strings.ToUpper(strings.ReplaceAll(k, "_", " "))+": "+shotText(s, k))
		}
	}
	if len(details) > 0 {
		animation += "\nSHOT CONTRACT\n" + strings.Join(details, "\n") + "\nDialogue and narration are editorial notes; do not add subtitles or assume precise lip sync unless explicitly requested."
	}
	if strings.TrimSpace(shotText(s, "feedback")) != "" {
		enhancement += "\nREVIEW NOTES: " + shotText(s, "feedback")
		animation += "\nREVIEW NOTES: " + shotText(s, "feedback")
	}
	optimized := optimizedDirectionReady(p, s)
	review := object(s, "direction_review")
	var reviewStatus any
	if review != nil {
		reviewStatus = shotText(review, "verdict")
		if shotText(review, "current_basis") != directionBasis(p, s) {
			reviewStatus = "stale"
		}
	}
	if provider == "grok" && optimized {
		animation = shotText(review, "grok_prompt")
	}
	if boolean(p, "music_video") {
		animation += "\nMUSIC VIDEO: Generate silent picture only. Disable audio generation; no speech, music or sound effects. The original song is added in the edit."
	}
	return ProjectDocument{"stem": rawValue(stem), "composition": rawValue(composition), "local_draft": rawValue(localDrawingPrompt(p, s)), "enhancement": rawValue(enhancement), "animation": rawValue(animation), "animation_ready": rawValue(imageApproved(p, s)), "provider": rawValue(provider), "optimized_direction_ready": rawValue(optimized), "direction_review": rawValue(review), "direction_review_status": rawValue(reviewStatus), "local_negative": rawOr(s, "local_negative_prompt", "")}
}
