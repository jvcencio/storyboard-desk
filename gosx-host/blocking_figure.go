package main

import (
	"math"
	"strings"
)

// A blocking figure is a jointed mannequin: capsules for the limbs and the
// trunk, spheres for the head, the hands and the hips. Every pose has the same
// parts with the same lengths, so a live preview only moves parts and a figure
// never reads as a different body.

type point3 struct{ X, Y, Z float64 }

func (p point3) plus(q point3) point3   { return point3{p.X + q.X, p.Y + q.Y, p.Z + q.Z} }
func (p point3) minus(q point3) point3  { return point3{p.X - q.X, p.Y - q.Y, p.Z - q.Z} }
func (p point3) times(n float64) point3 { return point3{p.X * n, p.Y * n, p.Z * n} }
func (p point3) distance(q point3) float64 {
	return math.Sqrt((p.X-q.X)*(p.X-q.X) + (p.Y-q.Y)*(p.Y-q.Y) + (p.Z-q.Z)*(p.Z-q.Z))
}
func (p point3) unit() point3 {
	length := p.distance(point3{})
	if length == 0 {
		return point3{0, 1, 0}
	}
	return p.times(1 / length)
}

// pitched tips a point forward about the X axis: a positive angle carries +Y
// towards +Z, the way a body leans forward.
func (p point3) pitched(angle float64) point3 {
	sin, cos := math.Sincos(angle)
	return point3{p.X, p.Y*cos - p.Z*sin, p.Z*cos + p.Y*sin}
}

// turned rotates about the vertical axis. Heading 0 faces +Z, towards the
// default camera; a positive heading turns towards +X.
func (p point3) turned(angle float64) point3 {
	sin, cos := math.Sincos(angle)
	return point3{p.X*cos + p.Z*sin, p.Y, -p.X*sin + p.Z*cos}
}

type figureBone struct {
	Part   string
	A, B   point3
	Radius float64
}

type figureSphere struct {
	Part   string
	C      point3
	Radius float64
}

type blockingFigure struct {
	Bones []figureBone
	Balls []figureSphere
}

// blockingChoice is one option of a pose control. Phrase is how the image
// prompt names it; the default option of each control has no phrase.
type blockingChoice struct{ Key, Label, Phrase string }

var blockingPoses = []blockingChoice{
	{"stand", "Standing", ""},
	{"walk", "Walking", "walking"},
	{"sit", "Sitting", "seated"},
	{"kneel", "Kneeling", "kneeling"},
	{"crouch", "Crouching", "crouching"},
	{"lie", "Lying down", "lying on their back"},
}

var blockingArms = []blockingChoice{
	{"down", "Relaxed", ""},
	{"hips", "Hands on hips", "hands on hips"},
	{"crossed", "Crossed", "arms crossed"},
	{"reach", "Reaching forward", "both arms reaching forward"},
	{"raise", "Right arm raised", "right arm raised"},
	{"hold", "Holding something", "forearms raised as if holding something"},
}

var blockingLooks = []blockingChoice{
	{"ahead", "Ahead", ""},
	{"left", "To their left", "looking to their left"},
	{"right", "To their right", "looking to their right"},
	{"up", "Up", "looking up"},
	{"down", "Down", "looking down"},
}

// blockingChoiceKey returns the option a stored or submitted value names. The
// empty value and an unknown value both mean the first, default option, so a
// scene written by a newer editor still opens.
func blockingChoiceKey(options []blockingChoice, value string) string {
	for _, option := range options {
		if option.Key == value {
			return value
		}
	}
	return options[0].Key
}

// Body measures in metres for a 1.75 m figure; other heights scale them.
const (
	figureHeight      = 1.75
	figureHipHalf     = 0.095
	figureThigh       = 0.44
	figureShin        = 0.42
	figureFoot        = 0.17
	figureSpine       = 0.46
	figureNeck        = 0.14
	figureHeadRise    = 0.25 // chest top to the centre of the head
	figureHeadRadius  = 0.115
	figureShoulder    = 0.19
	figureUpperArm    = 0.28
	figureForearm     = 0.25
	figureThighRadius = 0.07
	figureShinRadius  = 0.05
	figureFootRadius  = 0.045
)

type figureLegs struct {
	// Hip and knee bend in degrees for the left and the right leg. Hip 0 hangs
	// the thigh straight down and 90 points it forward; knee 0 is straight.
	leftHip, leftKnee, rightHip, rightKnee float64
	lean                                   float64 // trunk lean forward, degrees
	feetBack                               bool    // the feet point backwards, as when kneeling
}

var figureLegPoses = map[string]figureLegs{
	"stand":  {},
	"lie":    {},
	"walk":   {leftHip: 22, leftKnee: 8, rightHip: -18, rightKnee: 14, lean: 4},
	"sit":    {leftHip: 90, leftKnee: 90, rightHip: 90, rightKnee: 90},
	"kneel":  {leftKnee: 90, rightKnee: 90, feetBack: true},
	"crouch": {leftHip: 110, leftKnee: 140, rightHip: 110, rightKnee: 140, lean: 35},
}

// Arm poses give the direction of the upper arm and of the forearm for the
// left arm, in trunk space (+X is the figure's left, +Z is forward). The right
// arm mirrors them. "raise" lifts the right arm only.
var figureArmPoses = map[string][2]point3{
	"down":    {{0.10, -1, 0.03}, {0.05, -1, 0.12}},
	"hips":    {{0.55, -0.8, -0.1}, {-0.75, -0.45, 0.3}},
	"crossed": {{0.15, -0.9, 0.35}, {-0.95, 0.12, 0.28}},
	"reach":   {{0, -0.25, 1}, {0, -0.05, 1}},
	"hold":    {{0.08, -1, 0.05}, {-0.15, 0.05, 1}},
	"raise":   {{0.25, 1, 0.1}, {0.1, 1, 0.05}},
}

var figureLookDirections = map[string]point3{
	"ahead": {0, 0, 1},
	"left":  {0.766, 0, 0.643},
	"right": {-0.766, 0, 0.643},
	"up":    {0, 0.574, 0.819},
	"down":  {0, -0.574, 0.819},
}

// poseFigure builds the actor's figure in stage coordinates.
func poseFigure(actor blockingActor) blockingFigure {
	pose := blockingChoiceKey(blockingPoses, actor.Pose)
	arms := blockingChoiceKey(blockingArms, actor.Arms)
	look := blockingChoiceKey(blockingLooks, actor.Look)
	legs := figureLegPoses[pose]
	figure := blockingFigure{}
	bone := func(part string, a, b point3, radius float64) {
		figure.Bones = append(figure.Bones, figureBone{part, a, b, radius})
	}
	ball := func(part string, c point3, radius float64) {
		figure.Balls = append(figure.Balls, figureSphere{part, c, radius})
	}

	// The trunk hangs from the pelvis, which starts at the origin.
	lean := legs.lean * math.Pi / 180
	trunk := func(p point3) point3 { return p.pitched(lean) }
	chest := trunk(point3{0, figureSpine, 0})
	head := trunk(point3{0, figureSpine + figureHeadRise, 0})
	ball("pelvis", point3{}, 0.135)
	bone("torso", point3{}, chest, 0.125)
	bone("neck", chest, trunk(point3{0, figureSpine + figureNeck, 0}), 0.05)
	ball("head", head, figureHeadRadius)
	ball("nose", head.plus(trunk(figureLookDirections[look]).times(figureHeadRadius)), 0.032)
	bone("shoulders", trunk(point3{figureShoulder, figureSpine - 0.02, 0}), trunk(point3{-figureShoulder, figureSpine - 0.02, 0}), 0.065)

	for _, side := range []struct {
		name string
		sign float64
	}{{"left", 1}, {"right", -1}} {
		armPose := figureArmPoses[arms]
		if arms == "raise" && side.name == "left" {
			armPose = figureArmPoses["down"]
		}
		mirror := func(p point3) point3 { return point3{p.X * side.sign, p.Y, p.Z}.unit() }
		shoulder := point3{figureShoulder * side.sign, figureSpine - 0.02, 0}
		elbow := shoulder.plus(mirror(armPose[0]).times(figureUpperArm))
		hand := elbow.plus(mirror(armPose[1]).times(figureForearm))
		bone(side.name+"-upper-arm", trunk(shoulder), trunk(elbow), 0.05)
		bone(side.name+"-forearm", trunk(elbow), trunk(hand), 0.042)
		ball(side.name+"-hand", trunk(hand), 0.048)

		hipBend, kneeBend := legs.leftHip, legs.leftKnee
		if side.name == "right" {
			hipBend, kneeBend = legs.rightHip, legs.rightKnee
		}
		thigh, shin := hipBend*math.Pi/180, (hipBend-kneeBend)*math.Pi/180
		hip := point3{figureHipHalf * side.sign, 0, 0}
		knee := hip.plus(point3{0, -math.Cos(thigh), math.Sin(thigh)}.times(figureThigh))
		ankle := knee.plus(point3{0, -math.Cos(shin), math.Sin(shin)}.times(figureShin))
		toe := ankle.plus(point3{0, 0, figureFoot})
		if legs.feetBack {
			toe = ankle.plus(point3{0, 0, -figureFoot})
		}
		bone(side.name+"-thigh", hip, knee, figureThighRadius)
		bone(side.name+"-shin", knee, ankle, figureShinRadius)
		bone(side.name+"-foot", ankle, toe, figureFootRadius)
	}

	move := func(change func(point3) point3) {
		for i := range figure.Bones {
			figure.Bones[i].A, figure.Bones[i].B = change(figure.Bones[i].A), change(figure.Bones[i].B)
		}
		for i := range figure.Balls {
			figure.Balls[i].C = change(figure.Balls[i].C)
		}
	}
	if pose == "lie" {
		// On the back: what faced forward now faces up.
		move(func(p point3) point3 { return p.pitched(-math.Pi / 2) })
	}

	// Rest the lowest part on the floor, and centre the figure on the spot the
	// person chose: by the legs and hips when upright, so that moving an arm
	// never shifts the feet, and by the whole body when lying down.
	floor, back, front := math.Inf(1), math.Inf(1), math.Inf(-1)
	measure := func(part string, p point3, radius float64) {
		floor = math.Min(floor, p.Y-radius)
		if pose == "lie" || part == "pelvis" || strings.HasSuffix(part, "-thigh") || strings.HasSuffix(part, "-shin") || strings.HasSuffix(part, "-foot") {
			back, front = math.Min(back, p.Z-radius), math.Max(front, p.Z+radius)
		}
	}
	for _, b := range figure.Bones {
		measure(b.Part, b.A, b.Radius)
		measure(b.Part, b.B, b.Radius)
	}
	for _, b := range figure.Balls {
		measure(b.Part, b.C, b.Radius)
	}
	settle := point3{0, 0.012 - floor, -(back + front) / 2}

	scale := actor.Height / figureHeight
	if !(scale > 0) {
		scale = 1
	}
	heading := actor.Heading * math.Pi / 180
	move(func(p point3) point3 {
		return p.plus(settle).times(scale).turned(heading).plus(point3{actor.X, 0, actor.Z})
	})
	for i := range figure.Bones {
		figure.Bones[i].Radius *= scale
	}
	for i := range figure.Balls {
		figure.Balls[i].Radius *= scale
	}
	return figure
}
