package menus

import (
	"github.com/ebitenui/ebitenui/widget"
	"github.com/quasilyte/ge"
	"github.com/quasilyte/gsignal"
	"github.com/quasilyte/roboden-game/assets"
	"github.com/quasilyte/roboden-game/clientkit"
	"github.com/quasilyte/roboden-game/gamedata"
	"github.com/quasilyte/roboden-game/gameui/eui"
	"github.com/quasilyte/roboden-game/gtask"
	"github.com/quasilyte/roboden-game/serverapi"
	"github.com/quasilyte/roboden-game/session"
)

type submitScreenController struct {
	scene          *ge.Scene
	state          *session.State
	backController ge.SceneController
	replays        []serverapi.GameReplay
	spinner        *widget.Text
	spinnerFrames  []string
	t              float64
	success        bool
}

func NewSubmitScreenController(state *session.State, backController ge.SceneController, replays []serverapi.GameReplay) ge.SceneController {
	return &submitScreenController{
		state:          state,
		backController: backController,
		replays:        replays,
	}
}

func (c *submitScreenController) Init(scene *ge.Scene) {
	c.scene = scene
	c.initUI()
	c.spawnTask()
	c.spinnerFrames = []string{`\`, `|`, `/`, `--`}
}

func (c *submitScreenController) spawnTask() {
	initTask := gtask.StartTask(func(ctx *gtask.TaskContext) {
		for _, replay := range c.replays {
			if clientkit.SendOrEnqueueScore(c.state, gamedata.SeasonNumber, replay) {
				c.success = true
			}
		}
	})

	initTask.EventCompleted.Connect(nil, func(gsignal.Void) {
		c.scene.Context().ChangeScene(c.backController)
		if c.success {
			if c.state.UnlockAchievement(session.Achievement{Name: "gladiator", Elite: true}) {
				c.scene.Context().SaveGameData("save", c.state.Persistent)
			}
		}
	})

	c.scene.AddObject(initTask)
}

func (c *submitScreenController) initUI() {
	eui.AddBackground(c.state.BackgroundImage, c.scene)
	d := c.scene.Dict()

	root := eui.NewAnchorContainer()
	rowContainer := eui.NewRowLayoutContainer(10, nil)
	root.AddChild(rowContainer)

	rowContainer.AddChild(eui.NewCenteredLabel(d.Get("menu.submit.title"), assets.BitmapFont3))

	c.spinner = eui.NewCenteredLabel("--", assets.BitmapFont2)
	rowContainer.AddChild(c.spinner)

	uiObject := eui.NewSceneObject(root)
	c.scene.AddGraphics(uiObject)
	c.scene.AddObject(uiObject)
}

func (c *submitScreenController) Update(delta float64) {
	c.t += 10 * delta
	c.spinner.Label = c.spinnerFrames[int(c.t)%len(c.spinnerFrames)]
}
