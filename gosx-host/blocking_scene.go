package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
)

type blockingActor struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	X       float64 `json:"x"`
	Z       float64 `json:"z"`
	Heading float64 `json:"heading"`
	Height  float64 `json:"height"`
	Colour  string  `json:"colour"`
}

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
}

var blockingID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
var blockingColour = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func defaultBlockingScene() blockingScene {
	return blockingScene{Version: 1, Material: "selena-matte-v1",
		Actors: []blockingActor{{"actor-a", "Actor A", -1, 0, 20, 1.75, "#bd754b"}, {"actor-b", "Actor B", 1, -0.5, -25, 1.8, "#56857c"}},
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
	}
	put("camera.x", spec.Camera.X)
	put("camera.y", spec.Camera.Y)
	put("camera.z", spec.Camera.Z)
	put("camera.pan", spec.Camera.Pan)
	put("camera.tilt", spec.Camera.Tilt)
	put("camera.lens", spec.Camera.Lens)
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
		spec.Actors = append(spec.Actors, actor)
	}
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
		actors = append(actors, merged)
	}
	previous["version"], previous["material"] = known["version"], known["material"]
	previous["actors"], previous["camera"] = rawValue(actors), rawValue(camera)
	return rawValue(previous)
}
