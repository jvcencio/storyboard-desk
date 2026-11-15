package main

import (
	"math"
	"strconv"
	"sync"

	"m31labs.dev/gosx/scene"
)

// The stage has two Selena materials.
//
// BlockingClay shades every figure like a clay maquette under a softbox: a
// wrapped key light, a sky and floor fill, and a rim that separates the figure
// from the backdrop. Scene3D gives it the live camera position and the
// direction towards the key light, so a figure needs one uniform, its colour,
// and a pose change only moves meshes.
//
// BlockingMarks is a transparent sheet just above the floor. It draws the
// metre grid and the stage edge, anti-aliased by the pixel footprint and faded
// with distance, and in Plan view the shot camera with its field of view.
//
// Shadows are geometry, not shadow maps. Under one directional light the
// shadow of a sphere is an ellipse and the shadow of a capsule is the strip
// between two such ellipses, so each figure part gets an exact flat silhouette
// on the floor. At the pinned GoSX version shadow maps were not dependable for
// this stage: the single-map fit drew no shadow at all, and cascades drew
// extra shadows where no figure stood.
const blockingShaders = `material BlockingClay {
    param baseColor : color = rgb(0.74, 0.46, 0.29)
    param skyTint : color = rgb(1.0, 0.98, 0.94)
    param groundTint : color = rgb(0.62, 0.58, 0.52)

    context {
        cameraPos : vec3
        sunDir : vec3
    }

    surface(geo) -> color {
        let n = normalize(geo.worldNormal)
        let v = normalize(cameraPos - geo.worldPos)
        let wrap = clamp((dot(n, sunDir) + 0.4) / 1.4, 0.0, 1.0)
        let key = wrap * wrap
        let fill = mix(groundTint, skyTint, n.y * 0.5 + 0.5)
        let rim = pow(1.0 - clamp(dot(n, v), 0.0, 1.0), 3.0)
        let seat = mix(0.78, 1.0, clamp(geo.worldPos.y / 0.3, 0.0, 1.0))
        let lit = baseColor * (fill * 0.42 + skyTint * (key * 0.72)) * seat
        return lit + skyTint * (rim * 0.16)
    }
}

material BlockingMarks {
    param lineColor : color = rgb(0.43, 0.41, 0.36)
    param accent : color = rgb(0.68, 0.39, 0.23)
    param stage : vec4
    param shotCamera : vec4
    param lens : vec2

    context {
        cameraPos : vec3
    }

    surface(geo) -> vec4 {
        let p = geo.worldPos.xz
        let footprint = fwidth(p)
        let aa = max(footprint.x, footprint.y) * 1.5
        let cell = abs(fract(p - vec2f(0.5, 0.5)) - vec2f(0.5, 0.5))
        let grid = 1.0 - smoothstep(0.012, 0.012 + aa, min(cell.x, cell.y))
        let edge = max(abs(p.x) - stage.x, abs(p.y) - stage.y)
        let border = 1.0 - smoothstep(0.025, 0.025 + aa, abs(edge))
        let inside = 1.0 - step(0.0, edge)
        let plan = stage.w
        let reach = 1.0 - smoothstep(9.0, 30.0, distance(geo.worldPos, cameraPos))
        let fade = mix(reach, 1.0, plan)
        let lines = max(grid * mix(0.16, 0.42, inside), border * 0.7)

        let rel = p - shotCamera.xy
        let forward = shotCamera.zw
        let along = dot(rel, forward)
        let side = dot(rel, vec2f(0.0 - forward.y, forward.x))
        let ray = (abs(side) - along * lens.x) / sqrt(1.0 + lens.x * lens.x)
        let ahead = step(0.0, along) * step(along, lens.y)
        let frustum = (1.0 - smoothstep(0.03, 0.03 + aa, abs(ray))) * ahead
        let view = (1.0 - step(0.0, ray)) * ahead * 0.07
        let body = 1.0 - smoothstep(0.2, 0.2 + aa, length(rel))
        let shot = max(max(frustum * 0.85, view), body) * plan

        let alpha = max(lines * fade, shot)
        let tint = mix(lineColor, accent, clamp(shot / max(alpha, 0.001), 0.0, 1.0))
        return vec4f(tint, alpha)
    }
}`

type blockingMaterialSet struct {
	clay, marks scene.CustomMaterial
	err         error
}

// blockingMaterials compiles both Selena materials once per process. If a
// browser cannot build a Selena program, Scene3D draws the embedded standard
// material instead: the figure's own colour, and nothing for the marks.
var blockingMaterials = sync.OnceValue(func() blockingMaterialSet {
	set, hidden := blockingMaterialSet{}, 0.0
	set.clay, _, set.err = scene.CompileSelenaMaterial([]byte(blockingShaders), scene.SelenaMaterialOptions{
		Material: "BlockingClay", Standard: scene.StandardMaterial{Color: "#b98a6a", Roughness: 1}})
	if set.err != nil {
		return set
	}
	set.marks, _, set.err = scene.CompileSelenaMaterial([]byte(blockingShaders), scene.SelenaMaterialOptions{
		Material: "BlockingMarks", Standard: scene.StandardMaterial{Color: "#000000", Opacity: &hidden, BlendMode: scene.BlendAlpha}})
	return set
})

const (
	blockingStageHalfWidth = 8.0 // actors stand within ±8 m across
	blockingStageHalfDepth = 5.0 // and ±5 m in depth
	blockingFloorSize      = 80.0
	blockingBackdropColour = "#eeeadd"
	blockingFloorColour    = "#d6cfbe"
	blockingShadowColour   = "#8f8879" // the floor colour, darker; every shadow piece is this one opaque colour
	blockingSeatColour     = "#a79d88"
)

// blockingShadowCaster lays flat silhouettes on the floor for one light. The
// pieces are opaque and share one colour, so where they overlap they merge
// instead of darkening twice.
type blockingShadowCaster struct {
	toLight  point3
	material scene.StandardMaterial
}

// ground follows the light from p down to the floor.
func (c blockingShadowCaster) ground(p point3) point3 {
	fall := p.Y / c.toLight.Y
	return point3{p.X - c.toLight.X*fall, 0, p.Z - c.toLight.Z*fall}
}

// away is the direction shadows fall on the floor, and stretch is how much
// longer than the caster a shadow is along it.
func (c blockingShadowCaster) away() (direction point3, stretch float64) {
	direction = point3{-c.toLight.X, 0, -c.toLight.Z}
	if direction.distance(point3{}) < 1e-9 {
		return point3{0, 0, 1}, 1 // the light is straight overhead
	}
	return direction.unit(), 1 / c.toLight.Y
}

// ellipse is the shadow of a sphere: as wide as the sphere, and stretched
// along the light.
func (c blockingShadowCaster) ellipse(id string, centre point3, radius float64) scene.Mesh {
	away, stretch := c.away()
	spot := c.ground(centre)
	return scene.Mesh{ID: id, Material: c.material, Position: scene.Vec3(spot.X, 0.002, spot.Z),
		Geometry: scene.CylinderGeometry{RadiusTop: radius, RadiusBottom: radius, Height: 0.002, Segments: 20},
		Rotation: scene.Euler{Y: math.Atan2(away.X, away.Z)}, Scale: scene.Vec3(1, 1, stretch)}
}

// strip is the shadow of a limb between its two end ellipses. Its half-width
// is the ellipse's reach across the strip, so the three pieces join exactly.
func (c blockingShadowCaster) strip(id string, bone figureBone) scene.Mesh {
	away, stretch := c.away()
	a, b := c.ground(bone.A), c.ground(bone.B)
	length := a.distance(b)
	along := point3{0, 0, 1}
	if length > 1e-9 {
		along = b.minus(a).unit()
	}
	across := point3{along.Z, 0, -along.X}
	onLight := across.X*away.X + across.Z*away.Z
	offLight := across.X*away.Z - across.Z*away.X
	half := bone.Radius * math.Sqrt(stretch*stretch*onLight*onLight+offLight*offLight)
	middle := a.plus(b).times(0.5)
	// A unit box scaled to size keeps one geometry for every pose and light.
	return scene.Mesh{ID: id, Material: c.material, Position: scene.Vec3(middle.X, 0.002, middle.Z),
		Geometry: scene.BoxGeometry{Width: 1, Height: 0.002, Depth: 1},
		Rotation: scene.Euler{Y: math.Atan2(along.X, along.Z)}, Scale: scene.Vec3(2*half, 1, math.Max(length, 1e-4))}
}

// blockingLights are the key-light positions, named the way a lighting diagram
// names them: relative to the shot camera. Azimuth 0 is at the camera, -90 at
// camera left, 180 behind the subject. Because the light follows the camera,
// the phrase the prompt uses stays true when the camera pans.
var blockingLights = []blockingChoice{
	{"key-left", "Key from camera left", ""},
	{"key-right", "Key from camera right", "The key light comes from camera right, above."},
	{"front", "Front light", "The light comes from the camera position."},
	{"side-left", "Side light from the left", "A low side light comes from camera left."},
	{"side-right", "Side light from the right", "A low side light comes from camera right."},
	{"back", "Back light", "The figures are backlit: the light comes from behind them, towards the camera."},
	{"top", "Top light", "The light comes from directly above."},
}

var blockingLightAngles = map[string][2]float64{
	"key-left": {-45, 42}, "key-right": {45, 42}, "front": {0, 30}, "side-left": {-90, 22},
	"side-right": {90, 22}, "back": {155, 35}, "top": {0, 82},
}

// blockingCameraGround returns the shot camera's forward and right directions
// on the floor. Pan 0 looks along -Z; a positive pan turns towards -X.
func blockingCameraGround(camera blockingCamera) (forward, right point3) {
	sin, cos := math.Sincos(camera.Pan * math.Pi / 180)
	return point3{-sin, 0, -cos}, point3{cos, 0, -sin}
}

// blockingKeyLight returns the direction from the stage towards the key light.
func blockingKeyLight(spec blockingScene) point3 {
	angles := blockingLightAngles[blockingChoiceKey(blockingLights, spec.Light)]
	azimuth, elevation := angles[0]*math.Pi/180, angles[1]*math.Pi/180
	forward, right := blockingCameraGround(spec.Camera)
	flat := right.times(math.Sin(azimuth)).minus(forward.times(math.Cos(azimuth)))
	return flat.times(math.Cos(elevation)).plus(point3{0, math.Sin(elevation), 0})
}

// blockingLimbRotation turns a cylinder, which Scene3D builds along +Y, to lie
// along direction. Scene3D rotates about X first and then about Y.
func blockingLimbRotation(direction point3) scene.Euler {
	u := direction.unit()
	return scene.Euler{X: math.Acos(math.Max(-1, math.Min(1, u.Y))), Y: math.Atan2(u.X, u.Z)}
}

func blockingColourUniform(colour string) []float32 {
	n, _ := strconv.ParseUint(colour[1:], 16, 32)
	return []float32{float32(n>>16&255) / 255, float32(n>>8&255) / 255, float32(n&255) / 255}
}

func blockingRender(spec blockingScene, aspect, view string) (scene.Props, error) {
	materials := blockingMaterials()
	if materials.err != nil {
		return scene.Props{}, materials.err
	}
	at := func(p point3) scene.Vector3 { return scene.Vec3(p.X, p.Y, p.Z) }

	plan := 0.0
	if view == "plan" {
		plan = 1
	}
	forward, _ := blockingCameraGround(spec.Camera)
	marks := materials.marks
	marks.Uniforms = map[string]any{
		"stage":      []float32{blockingStageHalfWidth, blockingStageHalfDepth, 0, float32(plan)},
		"shotCamera": []float32{float32(spec.Camera.X), float32(spec.Camera.Z), float32(forward.X), float32(forward.Z)},
		// Half the horizontal view of a 36 mm wide frame, and how far to draw it.
		"lens": []float32{float32(18 / spec.Camera.Lens), 14},
	}
	toLight := blockingKeyLight(spec)
	nodes := []scene.Node{
		scene.DirectionalLight{ID: "key-light", Color: "#fff4e4", Intensity: 3, Direction: at(toLight.times(-1))},
		scene.HemisphereLight{ID: "fill-light", SkyColor: "#f6f3ec", GroundColor: "#b3ab9b", Intensity: 0.22},
		scene.Mesh{ID: "stage-floor", Geometry: scene.PlaneGeometry{Width: blockingFloorSize, Height: blockingFloorSize},
			Material: scene.StandardMaterial{Color: blockingFloorColour, Roughness: 1}},
		scene.Mesh{ID: "stage-marks", Geometry: scene.PlaneGeometry{Width: blockingFloorSize, Height: blockingFloorSize},
			Material: marks, Position: scene.Vec3(0, 0.004, 0)},
	}
	shadows := blockingShadowCaster{toLight: toLight, material: scene.StandardMaterial{Color: blockingShadowColour, Roughness: 1}}

	for _, actor := range spec.Actors {
		clay := materials.clay
		clay.Color = actor.Colour
		clay.Uniforms = map[string]any{"baseColor": blockingColourUniform(actor.Colour)}
		figure := poseFigure(actor)
		ball := func(id string, centre point3, radius float64, segments int) {
			nodes = append(nodes,
				scene.Mesh{ID: id, Material: clay, Position: at(centre), Geometry: scene.SphereGeometry{Radius: radius, Segments: segments}},
				shadows.ellipse(id+"-shadow", centre, radius))
		}
		for _, bone := range figure.Bones {
			id := actor.ID + "-" + bone.Part
			nodes = append(nodes,
				scene.Mesh{ID: id, Material: clay, Position: at(bone.A.plus(bone.B).times(0.5)), Rotation: blockingLimbRotation(bone.B.minus(bone.A)),
					// Rounded, so the same limb in another pose is the same
					// geometry and the preview can send a transform alone.
					Geometry: scene.CylinderGeometry{RadiusTop: bone.Radius, RadiusBottom: bone.Radius, Height: math.Round(bone.A.distance(bone.B)*1e4) / 1e4, Segments: 12}},
				shadows.strip(id+"-shadow", bone))
			// A ball at the far end rounds the limb into the next one.
			ball(id+"-joint", bone.B, bone.Radius, 12)
			if bone.Part == "shoulders" {
				ball(actor.ID+"-shoulders-cap", bone.A, bone.Radius, 12)
			}
		}
		for _, part := range figure.Balls {
			ball(actor.ID+"-"+part.Part, part.C, part.Radius, 16)
		}
		if blockingChoiceKey(blockingPoses, actor.Pose) == "sit" {
			// A plain block to sit on, so a seated figure does not float. It
			// is a stand-in: the shot's own text says what the seat is.
			for _, part := range figure.Balls {
				if part.Part != "pelvis" {
					continue
				}
				side, top := 0.44*actor.Height/figureHeight, part.C.Y-part.Radius*0.8
				nodes = append(nodes,
					scene.Mesh{ID: actor.ID + "-seat", Position: scene.Vec3(part.C.X, top/2, part.C.Z), Rotation: scene.Euler{Y: actor.Heading * math.Pi / 180},
						Geometry: scene.BoxGeometry{Width: 1, Height: 1, Depth: 1}, Scale: scene.Vec3(side, top, side),
						Material: scene.StandardMaterial{Color: blockingSeatColour, Roughness: 1}},
					shadows.strip(actor.ID+"-seat-shadow", figureBone{A: point3{part.C.X, 0, part.C.Z}, B: point3{part.C.X, top, part.C.Z}, Radius: side * 0.56}),
					shadows.ellipse(actor.ID+"-seat-shadow-top", point3{part.C.X, top, part.C.Z}, side*0.56),
					shadows.ellipse(actor.ID+"-seat-shadow-base", point3{part.C.X, 0, part.C.Z}, side*0.56))
			}
		}
		// A ring in the actor's colour marks the spot, as tape marks a stage.
		reach := 0.36 * actor.Height / figureHeight
		nodes = append(nodes, scene.Mesh{ID: actor.ID + "-mark", Position: scene.Vec3(actor.X, 0.008, actor.Z),
			Geometry: scene.RingGeometry(reach, reach+0.045, 40, 1, 0, 2*math.Pi),
			Material: scene.StandardMaterial{Color: actor.Colour, Roughness: 1, Emissive: 0.5}})
	}

	ratio := blockingAspect(aspect)
	props := scene.Props{Label: "Shot blocking", AriaLabel: "Shot blocking stage", Width: 960, Height: 600,
		Responsive: scene.Bool(true), FillHeight: scene.Bool(true), AutoRotate: scene.Bool(false), DragToRotate: scene.Bool(false), ForceWebGL: scene.Bool(true), RequireWebGL: scene.Bool(true),
		// A poster must look the same on every machine, so quality never adapts.
		AdaptiveQuality: scene.Bool(false),
		MaxPixels:       960 * 960, Stats: scene.Bool(false), Background: blockingBackdropColour, Graph: scene.NewGraph(nodes...),
		// Naming the fill here also stops Scene3D adding its own cool fill
		// light. The fog melts the far floor into the backdrop, like a studio
		// cyclorama; Plan view looks straight down and has none.
		Environment: scene.Environment{AmbientColor: "#ffffff", AmbientIntensity: 0.1, SkyColor: "#f6f3ec", SkyIntensity: 0.08,
			GroundColor: "#b3ab9b", GroundIntensity: 0.04, FogColor: blockingBackdropColour, FogDensity: 0.03 * (1 - plan)},
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
