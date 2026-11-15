package main

import (
	"fmt"
	"math"
	"strconv"
	"sync"

	"m31labs.dev/gosx/scene"
)

const blockingShader = `material StoryboardMatte {
    param baseColor : color = rgb(0.7, 0.5, 0.3)
    surface(geo) -> color {
        let n = normalize(geo.worldNormal)
        let light = max(0.3 * n.x + 0.8 * n.y + 0.45 * n.z, 0)
        return baseColor * (0.55 + 0.45 * light)
    }
}`

var blockingMaterialOnce sync.Once
var blockingMaterial scene.CustomMaterial
var blockingMaterialError error

func blockingMatte(colour string) (scene.CustomMaterial, error) {
	blockingMaterialOnce.Do(func() {
		blockingMaterial, _, blockingMaterialError = scene.CompileSelenaMaterial([]byte(blockingShader), scene.SelenaMaterialOptions{Standard: scene.StandardMaterial{Color: "#ffffff", Roughness: 1}})
	})
	n, _ := strconv.ParseUint(colour[1:], 16, 32)
	material := blockingMaterial
	material.Uniforms = map[string]any{"baseColor": []float32{float32(n>>16&255) / 255, float32(n>>8&255) / 255, float32(n&255) / 255}}
	return material, blockingMaterialError
}

func blockingRender(spec blockingScene, aspect, view string) (scene.Props, error) {
	nodes := []scene.Node{}
	box := func(id, colour string, x, y, z, width, height, depth float64) error {
		mat, err := blockingMatte(colour)
		if err != nil {
			return err
		}
		nodes = append(nodes, scene.Mesh{ID: id, Geometry: scene.BoxGeometry{Width: width, Height: height, Depth: depth}, Material: mat, Position: scene.Vec3(x, y, z)})
		return nil
	}
	if err := box("stage-floor", "#ded7c5", 0, -.09, 0, 16, .15, 10); err != nil {
		return scene.Props{}, err
	}
	_ = box("stage-bench", "#b7b6a5", 0, .42, -2, 2.8, .84, .8)
	for i := -8; i <= 8; i++ {
		_ = box(fmt.Sprintf("grid-x-%d", i), "#c8c2b3", float64(i), -.008, 0, .015, .008, 10)
	}
	for i := -5; i <= 5; i++ {
		_ = box(fmt.Sprintf("grid-z-%d", i), "#c8c2b3", 0, -.008, float64(i), 16, .008, .015)
	}
	for _, actor := range spec.Actors {
		mat, err := blockingMatte(actor.Colour)
		if err != nil {
			return scene.Props{}, err
		}
		scale := actor.Height / 1.8
		yaw := actor.Heading * math.Pi / 180
		part := func(id string, geometry scene.Geometry, x, y, z float64) {
			nodes = append(nodes, scene.Mesh{ID: actor.ID + "-" + id, Geometry: geometry, Material: mat,
				Position: scene.Vec3(actor.X+(x*math.Cos(yaw)+z*math.Sin(yaw))*scale, y*scale, actor.Z+(-x*math.Sin(yaw)+z*math.Cos(yaw))*scale),
				Rotation: scene.Euler{Y: yaw}, Scale: scene.Vec3(scale, scale, scale)})
		}
		part("head", scene.SphereGeometry{Radius: .16, Segments: 16}, 0, 1.62, 0)
		part("body", scene.BoxGeometry{Width: .43, Height: .65, Depth: .26}, 0, 1.1, 0)
		part("left-leg", scene.BoxGeometry{Width: .15, Height: .75, Depth: .18}, -.12, .38, 0)
		part("right-leg", scene.BoxGeometry{Width: .15, Height: .75, Depth: .18}, .12, .38, 0)
		part("facing", scene.BoxGeometry{Width: .09, Height: .08, Depth: .16}, 0, 1.63, .17)
	}
	ratio := blockingAspect(aspect)
	props := scene.Props{Label: "Shot blocking", AriaLabel: "Shot blocking stage", Width: 960, Height: 600,
		Responsive: scene.Bool(true), FillHeight: scene.Bool(true), AutoRotate: scene.Bool(false), DragToRotate: scene.Bool(false), ForceWebGL: scene.Bool(true), RequireWebGL: scene.Bool(true),
		MaxPixels: 960 * 960, Stats: scene.Bool(false), Background: "#eeeadd", Graph: scene.NewGraph(nodes...),
		UnsupportedMessage: "The 3D preview needs WebGL 2. You can still edit and save positions below.",
		Camera: scene.PerspectiveCamera{Position: scene.Vec3(spec.Camera.X, spec.Camera.Y, spec.Camera.Z), Rotation: scene.Euler{X: spec.Camera.Tilt * math.Pi / 180, Y: spec.Camera.Pan * math.Pi / 180},
			FOV: 2 * math.Atan(36/(2*spec.Camera.Lens*ratio)) * 180 / math.Pi, Near: .1, Far: 100}}
	if view == "plan" {
		props.OrthographicCamera = &scene.OrthographicCamera{Position: scene.Vec3(0, 15, 0), Rotation: scene.Euler{X: -math.Pi / 2}, Left: -8, Right: 8, Top: 5, Bottom: -5, Near: .1, Far: 100}
	}
	return props, nil
}

func blockingAspect(aspect string) float64 {
	switch aspect {
	case "9:16":
		return 9.0 / 16
	case "1:1":
		return 1
	}
	return 16.0 / 9
}

// Send zero coordinates explicitly. The compatibility camera loader supplies a
// default position for omitted coordinates, which would offset the Plan view.
func blockingCameraPayload(props scene.Props) map[string]any {
	camera := props.Camera
	result := map[string]any{"kind": "perspective", "x": camera.Position.X, "y": camera.Position.Y, "z": camera.Position.Z,
		"rotationX": camera.Rotation.X, "rotationY": camera.Rotation.Y, "rotationZ": camera.Rotation.Z, "fov": camera.FOV, "near": camera.Near, "far": camera.Far}
	if plan := props.OrthographicCamera; plan != nil {
		result = map[string]any{"kind": "orthographic", "x": plan.Position.X, "y": plan.Position.Y, "z": plan.Position.Z,
			"rotationX": plan.Rotation.X, "rotationY": plan.Rotation.Y, "rotationZ": plan.Rotation.Z, "left": plan.Left, "right": plan.Right, "top": plan.Top, "bottom": plan.Bottom, "zoom": 1, "near": plan.Near, "far": plan.Far}
	}
	return result
}
