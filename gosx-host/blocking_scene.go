package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

type blockingActor struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	X       float64 `json:"x"`
	Z       float64 `json:"z"`
	Heading float64 `json:"heading"`
	Height  float64 `json:"height"`
	Colour  string  `json:"colour"`
	// Pose, Arms and Look name options of blockingPoses, blockingArms and
	// blockingLooks. The default option is stored as "", so a scene saved
	// before poses existed keeps its bytes, and its poster stays current.
	Pose string `json:"pose,omitempty"`
	Arms string `json:"arms,omitempty"`
	Look string `json:"look,omitempty"`
}

// blockingActorChoices are the optional actor keys this editor owns. A key the
// actor no longer sets is removed from the stored record.
var blockingActorChoices = []string{"pose", "arms", "look"}

type blockingCamera struct {
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Z    float64 `json:"z"`
	Pan  float64 `json:"pan"`
	Tilt float64 `json:"tilt"`
	Lens float64 `json:"lens"`
}

type blockingScene struct {
	Version  int             `json:"version"`
	Material string          `json:"material"`
	Actors   []blockingActor `json:"actors"`
	Camera   blockingCamera  `json:"camera"`
	// Light names an option of blockingLights; the default is stored as "".
	Light string `json:"light,omitempty"`
}

var blockingID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
var blockingColour = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func defaultBlockingScene() blockingScene {
	return blockingScene{Version: 1, Material: "selena-matte-v1",
		Actors: []blockingActor{
			{ID: "actor-a", Name: "Actor A", X: -1, Z: 0, Heading: 20, Height: 1.75, Colour: "#bd754b"},
			{ID: "actor-b", Name: "Actor B", X: 1, Z: -0.5, Heading: -25, Height: 1.8, Colour: "#56857c"}},
		Camera: blockingCamera{Y: 2.2, Z: 7, Tilt: -8, Lens: 35}}
}

func readBlockingScene(shot ProjectDocument) (blockingScene, error) {
	if len(shot["blocking_scene"]) == 0 || nullJSON(shot["blocking_scene"]) {
		return defaultBlockingScene(), nil
	}
	var spec blockingScene
	if err := json.Unmarshal(shot["blocking_scene"], &spec); err != nil {
		return spec, fmt.Errorf("This blocking scene could not be opened")
	}
	if spec.Version != 1 || spec.Material != "selena-matte-v1" {
		return spec, fmt.Errorf("This blocking scene needs a newer editor")
	}
	_, fields := decodeBlockingFields(blockingFields(spec))
	if len(fields) != 0 {
		return spec, fmt.Errorf("This blocking scene has unsupported positions or camera settings")
	}
	return spec, nil
}

func blockingFields(spec blockingScene) map[string]string {
	fields := map[string]string{"actor_count": strconv.Itoa(len(spec.Actors))}
	put := func(name string, n float64) { fields[name] = strconv.FormatFloat(n, 'f', -1, 64) }
	for i, actor := range spec.Actors {
		prefix := fmt.Sprintf("actor.%d.", i)
		fields[prefix+"id"], fields[prefix+"name"], fields[prefix+"colour"] = actor.ID, actor.Name, actor.Colour
		put(prefix+"x", actor.X)
		put(prefix+"z", actor.Z)
		put(prefix+"heading", actor.Heading)
		put(prefix+"height", actor.Height)
		fields[prefix+"pose"] = blockingChoiceKey(blockingPoses, actor.Pose)
		fields[prefix+"arms"] = blockingChoiceKey(blockingArms, actor.Arms)
		fields[prefix+"look"] = blockingChoiceKey(blockingLooks, actor.Look)
	}
	put("camera.x", spec.Camera.X)
	put("camera.y", spec.Camera.Y)
	put("camera.z", spec.Camera.Z)
	put("camera.pan", spec.Camera.Pan)
	put("camera.tilt", spec.Camera.Tilt)
	put("camera.lens", spec.Camera.Lens)
	fields["light"] = blockingChoiceKey(blockingLights, spec.Light)
	return fields
}

func decodeBlockingFields(values map[string]string) (blockingScene, map[string]string) {
	spec := blockingScene{Version: 1, Material: "selena-matte-v1"}
	fields := map[string]string{}
	num := func(name string, low, high float64) float64 {
		n, err := strconv.ParseFloat(values[name], 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < low || n > high {
			fields[name] = fmt.Sprintf("Enter a number between %g and %g.", low, high)
		}
		return n
	}
	// choice reads one option. A form that does not send the field keeps the
	// default, and the default is stored as "".
	choice := func(name string, options []blockingChoice) string {
		value := values[name]
		if value == "" || value == options[0].Key {
			return ""
		}
		if blockingChoiceKey(options, value) != value {
			fields[name] = "Choose one of the listed options."
		}
		return value
	}
	count, err := strconv.Atoi(values["actor_count"])
	if err != nil || count < 1 || count > 8 {
		fields["actor_count"] = "A blocking scene supports 1–8 actors."
		return spec, fields
	}
	ids := map[string]bool{}
	for i := 0; i < count; i++ {
		prefix := fmt.Sprintf("actor.%d.", i)
		actor := blockingActor{ID: values[prefix+"id"], Name: values[prefix+"name"], Colour: values[prefix+"colour"]}
		if !blockingID.MatchString(actor.ID) || ids[actor.ID] {
			fields[prefix+"id"] = "Actor identities must be valid and unique."
		}
		ids[actor.ID] = true
		if len(actor.Name) == 0 || len(actor.Name) > 80 {
			fields[prefix+"name"] = "Name the actor using up to 80 characters."
		}
		if !blockingColour.MatchString(actor.Colour) {
			fields[prefix+"colour"] = "Choose an actor colour."
		}
		actor.X = num(prefix+"x", -8, 8)
		actor.Z = num(prefix+"z", -5, 5)
		actor.Heading = num(prefix+"heading", -180, 180)
		actor.Height = num(prefix+"height", 1, 2.5)
		actor.Pose = choice(prefix+"pose", blockingPoses)
		actor.Arms = choice(prefix+"arms", blockingArms)
		actor.Look = choice(prefix+"look", blockingLooks)
		spec.Actors = append(spec.Actors, actor)
	}
	spec.Light = choice("light", blockingLights)
	spec.Camera = blockingCamera{X: num("camera.x", -15, 15), Y: num("camera.y", 0.5, 10), Z: num("camera.z", 1, 18),
		Pan: num("camera.pan", -90, 90), Tilt: num("camera.tilt", -60, 20), Lens: num("camera.lens", 18, 100)}
	return spec, fields
}

func blockingVersion(shot ProjectDocument, aspect string) string {
	digest := sha256.Sum256(rawValue([]json.RawMessage{rawOr(shot, "blocking_scene", nil), rawValue(aspect)}))
	return hex.EncodeToString(digest[:])
}

// Merge owned fields into the stored document so future metadata, including
// unknown large numbers in actors and camera records, retains its JSON value.
func mergeBlockingScene(old json.RawMessage, spec blockingScene) json.RawMessage {
	previous := ProjectDocument{}
	_ = json.Unmarshal(old, &previous)
	if previous == nil {
		previous = ProjectDocument{}
	}
	known := ProjectDocument{}
	_ = json.Unmarshal(rawValue(spec), &known)
	camera := object(previous, "camera")
	if camera == nil {
		camera = ProjectDocument{}
	}
	for key, value := range object(known, "camera") {
		camera[key] = value
	}
	actors := []ProjectDocument{}
	for _, actor := range list(known, "actors") {
		merged := ProjectDocument{}
		for _, prior := range list(previous, "actors") {
			if shotText(prior, "id") == shotText(actor, "id") {
				merged = prior
				break
			}
		}
		for key, value := range actor {
			merged[key] = value
		}
		for _, key := range blockingActorChoices {
			if _, set := actor[key]; !set {
				delete(merged, key) // back to the default option
			}
		}
		actors = append(actors, merged)
	}
	if light, set := known["light"]; set {
		previous["light"] = light
	} else {
		delete(previous, "light") // back to the default light
	}
	previous["version"], previous["material"] = known["version"], known["material"]
	previous["actors"], previous["camera"] = rawValue(actors), rawValue(camera)
	return rawValue(previous)
}

const blockingMaxActors = 8

// blockingPalette gives each new actor a colour with its own plain name, so a
// prompt can say "the teal figure" without ambiguity.
var blockingPalette = []string{"#bd754b", "#56857c", "#4f6fa8", "#8a5a8c", "#b9a13d", "#b04a48", "#6f8f3f", "#c46a94"}

// addBlockingActor appends a standing actor with an unused identity, an unused
// colour name and a free spot on the stage.
func addBlockingActor(spec blockingScene) (blockingScene, error) {
	if len(spec.Actors) >= blockingMaxActors {
		return spec, fmt.Errorf("A blocking scene supports up to %d actors", blockingMaxActors)
	}
	ids, names := map[string]bool{}, map[string]bool{}
	for _, actor := range spec.Actors {
		ids[actor.ID], names[colourName(actor.Colour)] = true, true
	}
	actor := blockingActor{Height: 1.75, Colour: blockingPalette[len(spec.Actors)%len(blockingPalette)]}
	for letter := 'a'; letter <= 'z'; letter++ {
		if id := "actor-" + string(letter); !ids[id] {
			actor.ID, actor.Name = id, "Actor "+strings.ToUpper(string(letter))
			break
		}
	}
	for _, colour := range blockingPalette {
		if !names[colourName(colour)] {
			actor.Colour = colour
			break
		}
	}
	// Try the centre line first, then alternate outwards, nearer the camera.
	for _, spot := range [][2]float64{{0, 1}, {1.5, 1}, {-1.5, 1}, {3, 1}, {-3, 1}, {0, 2.5}, {1.5, 2.5}, {-1.5, 2.5}, {3, 2.5}, {-3, 2.5}} {
		free := true
		for _, other := range spec.Actors {
			free = free && math.Hypot(other.X-spot[0], other.Z-spot[1]) >= .75
		}
		if free {
			actor.X, actor.Z = spot[0], spot[1]
			break
		}
	}
	spec.Actors = append(append([]blockingActor{}, spec.Actors...), actor)
	return spec, nil
}

// removeBlockingActor drops the actor at index. A scene keeps at least one.
func removeBlockingActor(spec blockingScene, index int) (blockingScene, error) {
	if index < 0 || index >= len(spec.Actors) {
		return spec, fmt.Errorf("Choose an actor in this scene to remove")
	}
	if len(spec.Actors) == 1 {
		return spec, fmt.Errorf("A blocking scene keeps at least one actor")
	}
	actors := append([]blockingActor{}, spec.Actors[:index]...)
	spec.Actors = append(actors, spec.Actors[index+1:]...)
	return spec, nil
}

// colourName gives a #rrggbb colour the plain word an image model understands.
func colourName(hexColour string) string {
	if !blockingColour.MatchString(hexColour) {
		return "grey"
	}
	n, _ := strconv.ParseUint(hexColour[1:], 16, 32)
	r, g, b := float64(n>>16&255)/255, float64(n>>8&255)/255, float64(n&255)/255
	high, low := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
	light, spread := (high+low)/2, high-low
	saturation := 0.0
	if spread > 0 {
		saturation = spread / (1 - math.Abs(2*light-1))
	}
	switch {
	case light < .12:
		return "black"
	case saturation < .12 && light > .85:
		return "white"
	case saturation < .12:
		return "grey"
	}
	hue := 0.0
	switch high {
	case r:
		hue = 60 * math.Mod((g-b)/spread, 6)
	case g:
		hue = 60 * ((b-r)/spread + 2)
	default:
		hue = 60 * ((r-g)/spread + 4)
	}
	if hue < 0 {
		hue += 360
	}
	switch {
	case hue < 15 || hue >= 335:
		return "red"
	case hue < 40 && light < .32:
		return "brown"
	case hue < 40:
		return "orange"
	case hue < 65:
		return "yellow"
	case hue < 155:
		return "green"
	case hue < 195:
		return "teal"
	case hue < 255:
		return "blue"
	case hue < 310:
		return "purple"
	}
	return "pink"
}

// blockingPosterState says what a shot's saved camera poster is worth. The
// poster record outlives scene edits, so "has a poster" and "the poster shows
// the saved scene" are different facts; every reader goes through here.
type blockingPosterState int

const (
	blockingPosterNone    blockingPosterState = iota // nothing captured
	blockingPosterStale                              // captured from an earlier scene or format
	blockingPosterCurrent                            // shows the saved scene in the project format
)

func blockingPoster(project, shot ProjectDocument) (ProjectDocument, blockingPosterState) {
	poster := object(shot, "blocking_poster")
	if shotText(poster, "path") == "" {
		return nil, blockingPosterNone
	}
	if len(shot["blocking_scene"]) == 0 || shotText(poster, "scene_version") != blockingVersion(shot, shotText(project, "aspect")) {
		return poster, blockingPosterStale
	}
	return poster, blockingPosterCurrent
}

// blockingStudy returns the active sketch take that was made from a poster
// with this identity, or nil.
func blockingStudy(shot ProjectDocument, job string) ProjectDocument {
	for _, asset := range list(shot, "assets") {
		if shotText(object(asset, "blocking_study"), "job_id") == job {
			return asset
		}
	}
	return nil
}

func blockingStudyJob(poster ProjectDocument) string {
	version, hash := shotText(poster, "scene_version"), shotText(poster, "sha256")
	if len(version) < 16 || len(hash) < 16 {
		return ""
	}
	return "blocking-" + version[:16] + "-" + hash[:16]
}

// blockingLegend tells an image model how to read the shot's selected
// blocking study: which placeholder figure is which actor, and what to keep.
// It is empty unless the selected sketch came from the blocking editor, so
// prompts for every other shot stay byte-identical.
func blockingLegend(shot ProjectDocument) string {
	selected := shotText(shot, "selected_sketch")
	if selected == "" {
		return ""
	}
	for _, asset := range list(shot, "assets") {
		study := object(asset, "blocking_study")
		if shotText(asset, "id") != selected || study == nil || shotText(asset, "binned_at") != "" {
			continue
		}
		figures := []string{}
		for _, actor := range list(study, "actors") {
			figure := fmt.Sprintf("the %s figure is %s", shotText(actor, "colour_name"), shotText(actor, "name"))
			if stance := shotText(actor, "stance"); stance != "" {
				figure += " (" + stance + ")"
			}
			figures = append(figures, figure)
		}
		if len(figures) == 0 {
			return ""
		}
		legend := "BLOCKING STUDY: The attached blocking study fixes the camera angle and where each figure stands and faces. " +
			"Its figures are placeholders, not character designs: " + strings.Join(figures, "; ") + ". " +
			"Replace each figure with that character and keep its position, pose, scale and facing. Do not draw the floor grid or the placeholder shapes."
		if light := shotText(study, "light"); light != "" {
			legend += " " + light
		}
		return legend
	}
	return ""
}

// blockingStance says in words what a figure is doing, for the image prompt.
// A figure in every default option needs no words.
func blockingStance(actor blockingActor) string {
	words := []string{}
	for _, control := range []struct {
		options []blockingChoice
		value   string
	}{{blockingPoses, actor.Pose}, {blockingArms, actor.Arms}, {blockingLooks, actor.Look}} {
		key := blockingChoiceKey(control.options, control.value)
		for _, option := range control.options {
			if option.Key == key && option.Phrase != "" {
				words = append(words, option.Phrase)
			}
		}
	}
	return strings.Join(words, ", ")
}

// blockingLightPhrase says where the key light is, for the image prompt. The
// default light needs no words.
func blockingLightPhrase(spec blockingScene) string {
	key := blockingChoiceKey(blockingLights, spec.Light)
	for _, light := range blockingLights {
		if light.Key == key {
			return light.Phrase
		}
	}
	return ""
}
