package menus

import (
	"github.com/ebitenui/ebitenui/widget"
	"github.com/quasilyte/ge"
	"github.com/quasilyte/gmath"

	"github.com/quasilyte/roboden-game/assets"
	"github.com/quasilyte/roboden-game/controls"
	"github.com/quasilyte/roboden-game/gameui/eui"
	"github.com/quasilyte/roboden-game/session"
)

type OptionsExtraMenuController struct {
	state *session.State

	scene *ge.Scene
}

func NewOptionsExtraMenuController(state *session.State) *OptionsExtraMenuController {
	return &OptionsExtraMenuController{state: state}
}

func (c *OptionsExtraMenuController) Init(scene *ge.Scene) {
	c.scene = scene
	c.initUI()
}

func (c *OptionsExtraMenuController) Update(delta float64) {
	if c.state.MainInput.ActionIsJustPressed(controls.ActionBack) {
		c.back()
		return
	}
}

func (c *OptionsExtraMenuController) initUI() {
	uiResources := eui.LoadResources(c.scene.Context().Loader)

	root := eui.NewAnchorContainer()
	rowContainer := eui.NewRowLayoutContainer(10, nil)
	root.AddChild(rowContainer)

	normalFont := c.scene.Context().Loader.LoadFont(assets.FontNormal).Face

	d := c.scene.Dict()
	titleLabel := eui.NewLabel(uiResources, d.Get("menu.main.title")+" -> "+d.Get("menu.main.settings")+" -> "+d.Get("menu.options.extra"), normalFont)
	rowContainer.AddChild(titleLabel)

	options := &c.state.Persistent.Settings

	{
		sliderOptions := []string{
			d.Get("menu.option.off"),
			d.Get("menu.option.on"),
		}
		var slider gmath.Slider
		slider.SetBounds(0, len(sliderOptions)-1)
		if options.Debug {
			slider.TrySetValue(1)
		}
		debugButton := eui.NewButtonSelected(uiResources, d.Get("menu.options.debug")+": "+sliderOptions[slider.Value()])
		debugButton.ClickedEvent.AddHandler(func(args interface{}) {
			slider.Inc()
			options.Debug = slider.Value() != 0
			debugButton.Text().Label = d.Get("menu.options.debug") + ": " + sliderOptions[slider.Value()]
		})
		rowContainer.AddChild(debugButton)
	}

	rowContainer.AddChild(eui.NewSeparator(widget.RowLayoutData{Stretch: true}))

	rowContainer.AddChild(eui.NewButton(uiResources, c.scene, d.Get("menu.back"), func() {
		c.back()
	}))

	uiObject := eui.NewSceneObject(root)
	c.scene.AddGraphics(uiObject)
	c.scene.AddObject(uiObject)
}

func (c *OptionsExtraMenuController) back() {
	c.scene.Context().SaveGameData("save", c.state.Persistent)
	c.scene.Context().ChangeScene(NewOptionsController(c.state))
}
