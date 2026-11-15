package main

import (
	"math"
	"testing"
)

func figureExtent(figure blockingFigure) (low, high point3) {
	low, high = point3{math.Inf(1), math.Inf(1), math.Inf(1)}, point3{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	grow := func(p point3, r float64) {
		low = point3{math.Min(low.X, p.X-r), math.Min(low.Y, p.Y-r), math.Min(low.Z, p.Z-r)}
		high = point3{math.Max(high.X, p.X+r), math.Max(high.Y, p.Y+r), math.Max(high.Z, p.Z+r)}
	}
	for _, bone := range figure.Bones {
		grow(bone.A, bone.Radius)
		grow(bone.B, bone.Radius)
	}
	for _, ball := range figure.Balls {
		grow(ball.C, ball.Radius)
	}
	return low, high
}

func figureBall(t *testing.T, figure blockingFigure, part string) figureSphere {
	t.Helper()
	for _, ball := range figure.Balls {
		if ball.Part == part {
			return ball
		}
	}
	t.Fatalf("figure has no %q", part)
	return figureSphere{}
}

func TestEveryPoseStandsOnTheFloorWithWholeLimbs(t *testing.T) {
	standing := poseFigure(blockingActor{ID: "a", Height: 1.8})
	lengths := map[string]float64{}
	for _, bone := range standing.Bones {
		lengths[bone.Part] = bone.A.distance(bone.B)
	}
	for _, pose := range blockingPoses {
		for _, arms := range blockingArms {
			for _, look := range blockingLooks {
				actor := blockingActor{ID: "a", X: 2, Z: -1, Heading: 135, Height: 1.8, Pose: pose.Key, Arms: arms.Key, Look: look.Key}
				figure := poseFigure(actor)
				name := pose.Key + "/" + arms.Key + "/" + look.Key
				if len(figure.Bones) != len(standing.Bones) || len(figure.Balls) != len(standing.Balls) {
					t.Fatalf("%s: a pose changed the part list, so a live preview could not patch it", name)
				}
				low, high := figureExtent(figure)
				for _, n := range []float64{low.X, low.Y, low.Z, high.X, high.Y, high.Z} {
					if math.IsNaN(n) || math.IsInf(n, 0) {
						t.Fatalf("%s: not a finite figure", name)
					}
				}
				if low.Y < -0.01 || low.Y > 0.06 {
					t.Errorf("%s: lowest point at %.3f m; the figure should rest on the floor", name, low.Y)
				}
				// Limbs keep their length in every pose: a bone that stretches
				// would read as a different body.
				for _, bone := range figure.Bones {
					if got, want := bone.A.distance(bone.B), lengths[bone.Part]; math.Abs(got-want) > 0.002 {
						t.Errorf("%s: %s is %.3f m long, %.3f m when standing", name, bone.Part, got, want)
					}
				}
				// The figure stays near where the person put it.
				if math.Abs((low.X+high.X)/2-actor.X) > 0.7 || math.Abs((low.Z+high.Z)/2-actor.Z) > 0.7 {
					t.Errorf("%s: centred at %.2f, %.2f, placed at %.2f, %.2f", name, (low.X+high.X)/2, (low.Z+high.Z)/2, actor.X, actor.Z)
				}
			}
		}
	}
}

func TestPosesHaveTheHeightsTheirNamesPromise(t *testing.T) {
	top := func(pose string) float64 {
		_, high := figureExtent(poseFigure(blockingActor{ID: "a", Height: 1.8, Pose: pose}))
		return high.Y
	}
	if got := top(""); math.Abs(got-1.8) > 0.03 {
		t.Errorf("a 1.80 m actor stands %.2f m tall", got)
	}
	if got := top("stand"); got != top("") {
		t.Errorf("an empty pose is not standing: %.2f", got)
	}
	if sit, stand := top("sit"), top("stand"); sit > stand-0.35 || sit < 1.1 {
		t.Errorf("seated head height %.2f m against %.2f m standing", sit, stand)
	}
	if kneel := top("kneel"); kneel > 1.45 || kneel < 1.15 {
		t.Errorf("kneeling head height %.2f m", kneel)
	}
	if crouch := top("crouch"); crouch > 1.15 || crouch < 0.7 {
		t.Errorf("crouching head height %.2f m", crouch)
	}
	if lie := top("lie"); lie > 0.45 {
		t.Errorf("a lying figure reaches %.2f m", lie)
	}
	// Height scales the whole body.
	_, tall := figureExtent(poseFigure(blockingActor{ID: "a", Height: 2.0}))
	_, short := figureExtent(poseFigure(blockingActor{ID: "a", Height: 1.5}))
	if math.Abs(tall.Y-2.0) > 0.03 || math.Abs(short.Y-1.5) > 0.03 {
		t.Errorf("heights 2.00 and 1.50 gave %.2f and %.2f", tall.Y, short.Y)
	}
}

func TestTheNoseShowsWhereTheFigureLooks(t *testing.T) {
	facing := func(actor blockingActor) point3 {
		figure := poseFigure(actor)
		head, nose := figureBall(t, figure, "head"), figureBall(t, figure, "nose")
		return nose.C.minus(head.C).unit()
	}
	// Heading 0 faces +Z, towards the default camera; heading 90 faces +X.
	if d := facing(blockingActor{ID: "a", Height: 1.8}); d.Z < 0.95 {
		t.Errorf("heading 0 looks along %+v", d)
	}
	if d := facing(blockingActor{ID: "a", Height: 1.8, Heading: 90}); d.X < 0.95 {
		t.Errorf("heading 90 looks along %+v", d)
	}
	// Facing +Z with +Y up, the figure's own left is +X.
	if d := facing(blockingActor{ID: "a", Height: 1.8, Look: "left"}); d.X < 0.5 || d.Z < 0.3 {
		t.Errorf("looking left points along %+v", d)
	}
	if d := facing(blockingActor{ID: "a", Height: 1.8, Look: "right"}); d.X > -0.5 || d.Z < 0.3 {
		t.Errorf("looking right points along %+v", d)
	}
	if d := facing(blockingActor{ID: "a", Height: 1.8, Look: "up"}); d.Y < 0.3 {
		t.Errorf("looking up points along %+v", d)
	}
	if d := facing(blockingActor{ID: "a", Height: 1.8, Look: "down"}); d.Y > -0.3 {
		t.Errorf("looking down points along %+v", d)
	}
}

func TestArmPosesPutTheHandsWhereTheirNamesSay(t *testing.T) {
	hand := func(arms, side string) point3 {
		return figureBall(t, poseFigure(blockingActor{ID: "a", Height: 1.8, Arms: arms}), side+"-hand").C
	}
	if h := hand("", "right"); h.Y > 1.0 || h.X > -0.15 {
		t.Errorf("a relaxed right hand at %+v", h)
	}
	if h := hand("raise", "right"); h.Y < 1.8 {
		t.Errorf("a raised right hand at %+v", h)
	}
	if h := hand("raise", "left"); h.Y > 1.0 {
		t.Errorf("raise lifts the left hand too: %+v", h)
	}
	if h := hand("reach", "left"); h.Z < 0.4 || h.Y < 1.1 {
		t.Errorf("a reaching hand at %+v", h)
	}
	if l, r := hand("crossed", "left"), hand("crossed", "right"); l.X > 0.05 || r.X < -0.05 || l.Z < 0.1 {
		t.Errorf("crossed arms leave the hands at %+v and %+v", l, r)
	}
	if h := hand("hips", "left"); h.Y < 0.85 || h.Y > 1.15 || h.X > 0.26 {
		t.Errorf("a hand on the hip at %+v", h)
	}
}

// Scene3D builds a cylinder along +Y and rotates about X first, then about Y.
// A limb drawn with the wrong rotation would point somewhere else than the
// skeleton says, so check the angles against that documented order.
func TestLimbRotationPointsTheCylinderAlongTheBone(t *testing.T) {
	for _, direction := range []point3{{0, 1, 0}, {0, -1, 0}, {1, 0, 0}, {0, 0, 1}, {0, 0, -1}, {-1, 0, 0}, {0.3, -0.8, 0.5}, {-0.6, 0.2, -0.7}} {
		rotation := blockingLimbRotation(direction)
		aboutX := point3{0, math.Cos(rotation.X), math.Sin(rotation.X)} // +Y after the X rotation
		pointed := aboutX.turned(rotation.Y)
		if want := direction.unit(); pointed.distance(want) > 1e-9 {
			t.Errorf("direction %+v: the cylinder points along %+v", want, pointed)
		}
		if rotation.Z != 0 {
			t.Errorf("direction %+v: unexpected roll %v", direction, rotation.Z)
		}
	}
}

func TestKeyLightFollowsTheShotCamera(t *testing.T) {
	scene := defaultBlockingScene() // camera at +Z looking along -Z
	light := blockingKeyLight(scene)
	if light.X > -0.3 || light.Y < 0.5 || light.Z < 0.3 {
		t.Errorf("the default key should come from camera left, above and in front: %+v", light)
	}
	scene.Light = "key-right"
	if light = blockingKeyLight(scene); light.X < 0.3 {
		t.Errorf("key-right comes from %+v", light)
	}
	scene.Light = "back"
	if light = blockingKeyLight(scene); light.Z > -0.5 {
		t.Errorf("a back light should sit behind the figures, away from the camera: %+v", light)
	}
	// Pan the camera a quarter turn to its left: it now looks along -X, so
	// camera left is +Z.
	scene.Light, scene.Camera.Pan = "side-left", 90
	if light = blockingKeyLight(scene); light.Z < 0.8 || math.Abs(light.X) > 0.2 {
		t.Errorf("after a 90 degree pan, side-left comes from %+v", light)
	}
	scene.Light = "no such light"
	if light = blockingKeyLight(scene); math.Abs(light.distance(point3{})-1) > 1e-9 {
		t.Errorf("an unknown light gave a direction of length %v", light.distance(point3{}))
	}
}
