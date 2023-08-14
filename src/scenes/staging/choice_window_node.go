package staging

import (
	"math"

	resource "github.com/quasilyte/ebitengine-resource"
	"github.com/quasilyte/ge"
	"github.com/quasilyte/ge/input"
	"github.com/quasilyte/gmath"

	"github.com/quasilyte/roboden-game/assets"
	"github.com/quasilyte/roboden-game/controls"
	"github.com/quasilyte/roboden-game/gamedata"
	"github.com/quasilyte/roboden-game/gameinput"
	"github.com/quasilyte/roboden-game/gameui"
	"github.com/quasilyte/roboden-game/viewport"
)

type choiceWindowNode struct {
	scene *ge.Scene

	cam *viewport.Camera

	input *gameinput.Handler

	Enabled bool

	specialChoiceEnabled bool

	charging    bool
	creeps      bool
	targetValue float64
	value       float64

	floppyOffsetX float64
	selectedIndex int

	choices []*choiceOptionSlot

	world *worldState

	cursor *gameui.CursorNode
}

type choiceOptionSlot struct {
	flipAnim *ge.Animation
	floppy   *ge.Sprite
	icon     *ge.Sprite
	label1   *ge.Sprite
	label2   *ge.Sprite
	rect     gmath.Rect
	option   choiceOption
}

func newChoiceWindowNode(cam *viewport.Camera, world *worldState, h *gameinput.Handler, cursor *gameui.CursorNode, creeps bool) *choiceWindowNode {
	return &choiceWindowNode{
		cam:                  cam,
		input:                h,
		cursor:               cursor,
		selectedIndex:        -1,
		world:                world,
		creeps:               creeps,
		specialChoiceEnabled: true,
	}
}

func (w *choiceWindowNode) SetSpecialChoiceEnabled(enabled bool) {
	w.specialChoiceEnabled = enabled
	if w.charging == false {
		w.choices[4].icon.Visible = true
		w.choices[4].floppy.Visible = true
	}
}

func (w *choiceWindowNode) Init(scene *ge.Scene) {
	w.scene = scene

	floppies := [...]resource.ImageID{
		assets.ImageFloppyYellow,
		assets.ImageFloppyRed,
		assets.ImageFloppyGreen,
		assets.ImageFloppyBlue,
		assets.ImageFloppyGray,
	}
	flipSprites := [...]resource.ImageID{
		assets.ImageFloppyYellowFlip,
		assets.ImageFloppyRedFlip,
		assets.ImageFloppyGreenFlip,
		assets.ImageFloppyBlueFlip,
		assets.ImageFloppyGrayFlip,
	}
	offsetY := 8.0
	w.floppyOffsetX = (w.cam.Rect.Width() - 86 - 8)
	offset := gmath.Vec{X: w.floppyOffsetX, Y: 8}
	w.choices = make([]*choiceOptionSlot, 5)
	for i := range w.choices {
		floppyImageID := floppies[i]
		floppyFlipImageID := flipSprites[i]
		if w.creeps {
			floppyImageID = assets.ImageFloppyDark
			floppyFlipImageID = assets.ImageFloppyDarkFlip
		}

		floppy := scene.NewSprite(floppyImageID)
		floppy.Centered = false
		floppy.Pos.Offset = offset
		w.cam.UI.AddGraphics(floppy)

		flipSprite := scene.NewSprite(floppyFlipImageID)
		flipSprite.Centered = false
		flipSprite.Pos.Offset = offset
		flipSprite.Visible = false
		w.cam.UI.AddGraphics(flipSprite)

		offset.Y += floppy.ImageHeight() + offsetY

		label1 := scene.NewSprite(assets.ImagePriorityIcons)
		label1.Pos.Base = &floppy.Pos.Offset
		label1.Visible = false

		var label2 *ge.Sprite
		if w.creeps {
			if i != 4 {
				label1.SetAlpha(0.8)
			}
			label2 = scene.NewSprite(assets.ImageAttackDirections)
			label2.Pos.Base = &floppy.Pos.Offset
			label2.Visible = false
		} else {
			label1.Centered = false
			label2 = scene.NewSprite(assets.ImagePriorityIcons)
			label2.Pos.Base = &floppy.Pos.Offset
			label2.Visible = false
		}
		label2.Centered = false

		w.cam.UI.AddGraphics(label1)
		w.cam.UI.AddGraphics(label2)

		var icon *ge.Sprite
		if i == 4 {
			icon = ge.NewSprite(scene.Context())
			icon.Centered = false
			icon.Pos.Base = &floppy.Pos.Offset
			icon.Pos.Offset = gmath.Vec{X: 48, Y: 24}
			w.cam.UI.AddGraphics(icon)
		}

		choice := &choiceOptionSlot{
			flipAnim: ge.NewAnimation(flipSprite, -1),
			label1:   label1,
			label2:   label2,
			floppy:   floppy,
			icon:     icon,
			rect: gmath.Rect{
				Min: floppy.Pos.Resolve(),
				Max: floppy.Pos.Resolve().Add(gmath.Vec{
					X: floppy.ImageWidth(),
					Y: floppy.ImageHeight(),
				}),
			},
		}
		// Hide it behind the camera before Update() starts to drag it in.
		floppy.Pos.Offset.X += 640

		w.choices[i] = choice
	}
}

func (w *choiceWindowNode) IsDisposed() bool {
	return false
}

func (w *choiceWindowNode) getFloppyVisibility(i int) bool {
	if i < 4 {
		return true
	}
	return w.specialChoiceEnabled
}

func (w *choiceWindowNode) RevealChoices(selection choiceSelection) {
	w.charging = false

	for i, o := range w.choices {
		o.floppy.Pos.Offset.X = w.floppyOffsetX
		o.floppy.Visible = w.getFloppyVisibility(i)
		o.label1.Visible = false
		o.label2.Visible = false
		o.flipAnim.Sprite().Visible = false
		if o.icon != nil {
			o.icon.Visible = w.getFloppyVisibility(i)
		}
	}

	if w.creeps {
		for i, o := range selection.cards {
			choice := w.choices[i]
			choice.option = o
			choice.label1.Visible = true
			choice.label1.Pos.Offset = gmath.Vec{X: 66, Y: 40}
			choice.label1.SetImage(w.scene.LoadImage(o.icon))
			choice.label2.Visible = true
			choice.label2.Pos.Offset = gmath.Vec{X: 6, Y: 28}
			choice.label2.FrameOffset.X = float64(o.direction) * choice.label2.FrameWidth
		}
	} else {
		for i, o := range selection.cards {
			faction := gamedata.FactionTag(i + 1)
			choice := w.choices[i]
			choice.option = o
			if len(o.effects) == 1 {
				choice.label1.Visible = true
				choice.label1.Pos.Offset = gmath.Vec{X: 55, Y: 32}
				setPriorityIconFrame(choice.label1, o.effects[0].priority, faction)
			} else {
				choice.label1.Visible = true
				choice.label1.Pos.Offset = gmath.Vec{X: 55, Y: 32 - 10}
				setPriorityIconFrame(choice.label1, o.effects[0].priority, faction)
				choice.label2.Visible = true
				choice.label2.Pos.Offset = gmath.Vec{X: 55, Y: 32 + 10}
				setPriorityIconFrame(choice.label2, o.effects[1].priority, faction)
			}
		}
	}

	w.choices[4].option = selection.special
	w.choices[4].icon.SetImage(w.scene.LoadImage(selection.special.icon))

	w.scene.Audio().PlaySound(assets.AudioChoiceReady)
}

func (w *choiceWindowNode) StartCharging(targetValue float64, cardIndex int) {
	w.charging = true
	w.targetValue = targetValue
	w.value = 0
	w.selectedIndex = cardIndex

	for i, o := range w.choices {
		if i == w.selectedIndex {
			continue
		}
		o.flipAnim.Rewind()
		o.flipAnim.Sprite().Visible = w.getFloppyVisibility(i)
		o.floppy.Visible = false
		o.label1.Visible = false
		o.label2.Visible = false
		if o.icon != nil {
			o.icon.Visible = false
		}
	}
}

func (w *choiceWindowNode) Update(delta float64) {
	if !w.charging {
		return
	}
	w.value += delta

	percentage := w.value / w.targetValue
	const maxSlideOffset float64 = 86 + 8
	for i, o := range w.choices {
		if i == w.selectedIndex {
			o.floppy.Pos.Offset.X = math.Round(w.floppyOffsetX + maxSlideOffset*(1.05*percentage))
			continue
		}

		if o.flipAnim.Tick(delta) {
			o.flipAnim.Sprite().Visible = false
		}
	}
}

func (w *choiceWindowNode) GetChoiceUnderCursor(pos gmath.Vec) *choiceOptionSlot {
	for i, choice := range w.choices {
		if !w.specialChoiceEnabled && i == 4 {
			continue
		}
		if choice.rect.Contains(pos) {
			return choice
		}
	}
	return nil
}

func (w *choiceWindowNode) HandleInput() int {
	if w.charging {
		return -1
	}

	if pos, ok := w.cursor.ClickPos(controls.ActionClick); ok {
		pos = pos.Sub(w.cam.ScreenPos)
		for i, choice := range w.choices {
			if !w.specialChoiceEnabled && i == 4 {
				continue
			}
			if choice.rect.Contains(pos) {
				return i
			}
		}
	}

	actions := [...]input.Action{
		controls.ActionChoice1,
		controls.ActionChoice2,
		controls.ActionChoice3,
		controls.ActionChoice4,
		controls.ActionChoice5,
	}
	for i, a := range actions {
		if !w.specialChoiceEnabled && i == 4 {
			continue
		}
		if w.input.ActionIsJustPressed(a) {
			return i
		}
	}

	return -1
}
