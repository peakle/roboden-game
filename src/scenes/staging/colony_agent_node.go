package staging

import (
	"fmt"
	"math"

	resource "github.com/quasilyte/ebitengine-resource"
	"github.com/quasilyte/ge"
	"github.com/quasilyte/gmath"
	"github.com/quasilyte/gsignal"

	"github.com/quasilyte/roboden-game/assets"
	"github.com/quasilyte/roboden-game/gamedata"
	"github.com/quasilyte/roboden-game/pathing"
	"github.com/quasilyte/roboden-game/viewport"
)

const (
	agentFlightHeight float64 = 40.0
	agentPickupSpeed  float64 = 40.0
)

var turretDamageTextureList = []resource.ImageID{
	assets.ImageTurretDamageMask1,
	assets.ImageTurretDamageMask2,
	assets.ImageTurretDamageMask3,
	assets.ImageTurretDamageMask4,
}

type colonyAgentMode uint8

const (
	agentModeStandby colonyAgentMode = iota
	agentModeAlignStandby
	agentModeCharging
	agentModePosing
	agentModeForcedCharging
	agentModeMineEssence
	agentModeCourierFlight
	agentModeScavenge
	agentModeRepairBase
	agentModeRepairTurret
	agentModeReturn
	agentModePatrol
	agentModeFollow
	agentModeMove
	agentModeCloakHide
	agentModePanic
	agentModeAttack
	agentModeMakeClone
	agentModeWaitCloning
	agentModePickup
	agentModeResourceTakeoff
	agentModeTakeoff
	agentModeRecycleReturn
	agentModeRecycleLanding
	agentModeMerging
	agentModeMergingRoomba
	agentModeBuildBuilding
	agentModeGuardForever
	agentModeRoombaPatrol
	agentModeRoombaWait
	agentModeKamikazeAttack
	agentModeConsumeDrone
)

type agentTraitBits uint64

const (
	traitNeverStop agentTraitBits = 1 << iota
	traitCounterClocwiseOrbiting
	traitWorkaholic
	traitDoOrDie
	traitAdventurer
	traitLowHPBerserk
	traitLowHPRetreat
	traitLowHPRecycle
	traitLowHPPanic
)

type colonyAgentNode struct {
	anim       *ge.Animation
	sprite     *ge.Sprite
	shadow     *ge.Sprite
	diode      *ge.Sprite
	colonyCore *colonyCoreNode

	flashComponent damageFlashComponent

	scene *ge.Scene

	stats *gamedata.AgentStats

	cloningBeam *cloningBeamNode

	pos       gmath.Vec
	spritePos gmath.Vec

	traits agentTraitBits
	path   pathing.GridPath

	mode     colonyAgentMode
	waypoint gmath.Vec
	target   any

	payload         int
	cloneGen        int
	rank            int
	devourerLevel   int
	faction         gamedata.FactionTag
	cargoValue      float64
	cargoEliteValue float64
	reloadRate      float64
	healthRegen     float64

	height float64

	attackDelay  float64
	supportDelay float64
	specialDelay float64
	cloaking     float64

	maxHealth       float64
	health          float64
	maxEnergy       float64
	energy          float64
	energyBill      float64
	energyRegenRate float64
	slow            float64
	lifetime        float64

	tether  bool
	resting bool
	speed   float64

	dist          float64
	waypointsLeft int

	EventDestroyed gsignal.Event[*colonyAgentNode]
}

func newColonyAgentNode(core *colonyCoreNode, stats *gamedata.AgentStats, pos gmath.Vec) *colonyAgentNode {
	a := &colonyAgentNode{
		colonyCore:      core,
		stats:           stats,
		pos:             pos,
		height:          agentFlightHeight,
		reloadRate:      1,
		energyRegenRate: 1,
	}
	return a
}

func (a *colonyAgentNode) AsRecipeSubject() gamedata.RecipeSubject {
	return gamedata.RecipeSubject{Kind: a.stats.Kind, Faction: a.faction}
}

func (a *colonyAgentNode) Clone() *colonyAgentNode {
	// TODO: a clone should have the same current energy/health levels?
	if a.rank > 0 {
		panic("attempted to clone an elite unit")
	}
	cloned := newColonyAgentNode(a.colonyCore, a.stats, a.pos)
	cloned.speed = a.speed
	cloned.devourerLevel = a.devourerLevel
	cloned.maxHealth = a.maxHealth
	cloned.maxEnergy = a.maxEnergy
	cloned.reloadRate = a.reloadRate
	cloned.traits = a.traits
	cloned.cloneGen = a.cloneGen + 1
	cloned.faction = a.faction
	cloned.energyRegenRate = a.energyRegenRate
	cloned.healthRegen = a.healthRegen
	return cloned
}

func (a *colonyAgentNode) Init(scene *ge.Scene) {
	a.scene = scene

	if a.stats.Tier == 1 {
		a.lifetime = scene.Rand().FloatRange(1.5*60, 3*60)
		// If it's a neutral drone, don't hurry to recycle it.
		// It's probably a new base and it may need drones to live for longer.
		// If evolution priority is high, neutral drones will be recycled anyway.
		if a.faction == gamedata.NeutralFactionTag {
			a.lifetime *= 2
		}
	}

	if a.cloneGen == 0 {
		a.energyRegenRate = 1 + a.stats.EnergyRegenRateBonus
		a.healthRegen = a.stats.SelfRepair
		a.maxHealth = a.stats.MaxHealth * scene.Rand().FloatRange(0.9, 1.1)
		a.maxEnergy = scene.Rand().FloatRange(120, 200)
		a.speed = a.stats.Speed * scene.Rand().FloatRange(0.8, 1.1)

		switch a.faction {
		case gamedata.RedFactionTag:
			a.maxHealth *= 1.4
		case gamedata.GreenFactionTag:
			a.speed *= 1.25
		case gamedata.BlueFactionTag:
			a.maxEnergy *= 1.8
			a.energyRegenRate += 0.2
		case gamedata.YellowFactionTag:
			a.energyRegenRate += 0.5
		}
	}

	if a.cloneGen == 0 && !a.IsTurret() {
		// There are 64 random bits in total.
		// Every bit adds 1/64 chance (~1.5%).
		// Number of bits => chance table:
		//   1 => 50%
		//   2 => 25%
		//   3 => 12.5%
		//   4 => 6.25%
		//   5 => 3.125%
		//   6 => 1.5625%
		const (
			chance12                    = 0b111
			chance12bits                = 3
			counterClockwiseBits uint64 = chance12 << (0 * chance12bits)
			workaholicBits       uint64 = chance12 << (1 * chance12bits)
			doOrDieBits          uint64 = chance12 << (2 * chance12bits)
			adventurerBits       uint64 = chance12 << (3 * chance12bits)
		)
		traitBitChance12Roll := scene.Rand().Uint64()
		if traitBitChance12Roll&counterClockwiseBits == counterClockwiseBits {
			a.traits |= traitCounterClocwiseOrbiting
		}
		if traitBitChance12Roll&workaholicBits == workaholicBits {
			a.traits |= traitWorkaholic
		}
		if traitBitChance12Roll&doOrDieBits == doOrDieBits {
			a.traits |= traitDoOrDie
		}
		if traitBitChance12Roll&adventurerBits == adventurerBits {
			a.traits |= traitAdventurer
		}

		if scene.Rand().Chance(0.4) {
			a.traits |= traitNeverStop
		}

		// These trait bits can't be combined.
		// Only one of them will take place.
		roll := scene.Rand().Float()
		switch {
		case roll < 0.10:
			// 10% for retreat.
			a.traits |= traitLowHPRetreat
		case roll < 0.20:
			// 10% for recycle.
			if a.stats.Tier == 1 {
				a.traits |= traitLowHPRecycle
			}
		case roll < 0.25:
			// 5% for berserk.
			a.traits |= traitLowHPBerserk
		case roll < 0.30:
			// 5% for panic.
			a.traits |= traitLowHPPanic
		}
	}

	if a.cloneGen == 0 {
		a.applyRankBonuses()
	}

	a.health = a.maxHealth
	a.energy = a.maxEnergy

	a.sprite = scene.NewSprite(a.stats.Image)
	a.sprite.Pos.Base = &a.spritePos
	if a.IsFlying() {
		a.camera().AddSpriteAbove(a.sprite)
	} else {
		if a.stats.Kind == gamedata.AgentRoomba {
			a.camera().AddSpriteSlightlyBelow(a.sprite)
		} else {
			a.camera().AddSprite(a.sprite)
		}
		// Turret damage is an optional shader.
		if a.IsTurret() && a.world().graphicsSettings.AllShadersEnabled {
			a.sprite.Shader = scene.NewShader(assets.ShaderColonyDamage)
			a.sprite.Shader.SetFloatValue("HP", 1.0)
			a.sprite.Shader.Enabled = false
			damageTexture := gmath.RandElem(a.world().localRand, turretDamageTextureList)
			a.sprite.Shader.Texture1 = scene.LoadImage(damageTexture)
		}
	}

	a.flashComponent.sprite = a.sprite

	if a.faction != gamedata.NeutralFactionTag {
		a.diode = scene.NewSprite(assets.ImageFactionDiode)
		a.diode.Pos.Base = &a.spritePos
		a.diode.Pos.Offset.Y = a.stats.DiodeOffset
		var colorScale ge.ColorScale
		colorScale.SetColor(gamedata.FactionByTag(a.faction).Color)
		a.diode.SetColorScale(colorScale)

		if a.IsFlying() {
			a.camera().AddSpriteAbove(a.diode)
		} else {
			if a.stats.Kind == gamedata.AgentRoomba {
				a.camera().AddSpriteSlightlyBelow(a.diode)
			} else {
				a.camera().AddSprite(a.diode)
			}
		}
	}

	if a.IsFlying() && a.world().graphicsSettings.ShadowsEnabled {
		shadowImage := assets.ImageSmallShadow
		switch a.stats.Size {
		case gamedata.SizeMedium:
			shadowImage = assets.ImageMediumShadow
		case gamedata.SizeLarge:
			shadowImage = assets.ImageBigShadow
		}
		a.shadow = scene.NewSprite(shadowImage)
		a.shadow.Pos.Base = &a.spritePos
		a.camera().AddSprite(a.shadow)
	}

	if a.world().config.ExecMode != gamedata.ExecuteSimulation {
		// If there are no animation frames inside the image, do
		// not create the animation object.
		if a.sprite.FrameWidth != a.sprite.ImageWidth() {
			a.anim = ge.NewRepeatedAnimation(a.sprite, -1)
			a.anim.Tick(a.world().localRand.FloatRange(0, 0.7))
			a.anim.SetOffsetY(float64(a.rank) * a.sprite.FrameHeight)
		}
	}

	a.supportDelay = scene.Rand().FloatRange(0.8, 2)
}

func (a *colonyAgentNode) IsDisposed() bool { return a.sprite.IsDisposed() }

func (a *colonyAgentNode) IsTurret() bool {
	switch a.stats.Kind {
	case gamedata.AgentGunpoint, gamedata.AgentBeamTower, gamedata.AgentTetherBeacon:
		return true
	default:
		return false
	}
}

func (a *colonyAgentNode) applyRankBonuses() {
	switch a.rank {
	case 0:
		// A normal unit. No bonuses.

	case 1:
		// An elite unit.
		a.maxHealth *= 1.15
		a.speed *= 1.15
		a.maxEnergy *= 1.4
		a.energyRegenRate += 0.1
		a.reloadRate = 1.3 // +30% attack/special reload speed
		a.healthRegen += 0.25

	case 2:
		// A super elite unit.
		a.maxHealth *= 1.5
		a.speed *= 1.2
		a.maxEnergy *= 2.0
		a.energyRegenRate += 0.3
		a.reloadRate = 1.6 // +60% attack/special reload speed
		a.healthRegen += 0.5
	}
}

func (a *colonyAgentNode) AssignMode(mode colonyAgentMode, pos gmath.Vec, target any) bool {
	if a.IsTurret() {
		panic("assigning a mode to a turret")
	}

	switch mode {
	case agentModeReturn:
		entranceNum := a.scene.Rand().IntRange(0, 2)
		a.waypoint = a.colonyCore.GetStoragePos().Add(gmath.Vec{Y: float64(entranceNum) * 8})
		a.mode = mode
		return true

	case agentModePatrol:
		a.mode = mode
		a.dist = a.colonyCore.PatrolRadius()
		a.waypoint = a.orbitingWaypoint()
		a.waypointsLeft = a.scene.Rand().IntRange(40, 70)
		return true

	case agentModeWaitCloning:
		a.mode = mode
		a.target = target
		a.waypoint = gmath.Vec{}
		return true

	case agentModeMakeClone:
		a.mode = mode
		a.target = target
		a.dist = a.scene.Rand().FloatRange(1.2, 2) // cloning time
		a.energyBill += 20
		targetPos := target.(*colonyAgentNode).pos
		a.waypoint = a.pos.DirectionTo(targetPos).Mulf(110).Add(targetPos).Add(a.scene.Rand().Offset(-20, 20))
		return true

	case agentModeMerging, agentModeMergingRoomba:
		a.mode = mode
		a.target = target
		a.dist = a.scene.Rand().FloatRange(8, 10) // merging time
		if mode == agentModeMergingRoomba {
			a.dist *= 1.5
		}
		return true

	case agentModeAlignStandby:
		if a.shadow != nil && a.mode == agentModeTakeoff {
			a.shadow.Visible = true
		}
		if a.cloningBeam != nil {
			a.cloningBeam.Dispose()
			a.cloningBeam = nil
		}
		a.sprite.SetColorScale(ge.ColorScale{R: 1, G: 1, B: 1, A: 1})
		a.mode = mode
		a.waypoint = a.pos.Sub(gmath.Vec{Y: agentFlightHeight - a.height})
		return true

	case agentModeMove:
		a.mode = mode
		a.waypoint = pos
		return true

	case agentModePanic:
		a.mode = mode
		a.waypoint = a.pos
		a.waypointsLeft = a.scene.Rand().IntRange(4, 9)
		return true

	case agentModeStandby:
		if a.cloningBeam != nil {
			a.cloningBeam.Dispose()
			a.cloningBeam = nil
		}
		a.mode = mode
		maxDist := a.colonyCore.realRadius
		if !a.stats.CanPatrol {
			maxDist *= 0.65
		}
		a.dist = a.scene.Rand().FloatRange(40, maxDist)
		a.waypoint = a.orbitingWaypoint()
		a.waypointsLeft = 0
		return true

	case agentModeFollow, agentModeAttack:
		isPatrol := a.mode == agentModePatrol
		a.mode = agentModeFollow // attack is a long-range follow
		a.target = target
		a.waypoint = a.followWaypoint(target.(*creepNode).pos)
		if isPatrol {
			a.waypointsLeft = a.scene.Rand().IntRange(4, 6)
		} else {
			a.waypointsLeft = a.scene.Rand().IntRange(6, 8)
		}
		if mode == agentModeAttack {
			a.waypointsLeft += 5
			if a.hasTrait(traitDoOrDie) {
				a.waypointsLeft += 15
			}
		}
		return true

	case agentModeCloakHide:
		a.mode = mode
		a.waypoint = gmath.Vec{}
		return true

	case agentModeCharging, agentModeForcedCharging:
		a.mode = mode
		a.waypoint = gmath.Vec{}
		return true

	case agentModePosing:
		a.mode = mode
		a.dist = pos.X // idle time
		a.waypoint = gmath.Vec{}
		return true

	case agentModeCourierFlight:
		colony := target.(*colonyCoreNode)
		energyCost := gmath.ClampMax(colony.pos.DistanceTo(a.pos)*0.33, 100)
		if a.tether {
			energyCost *= 0.5
		}
		if a.stats.Kind == gamedata.AgentTrucker {
			// Truckers consume 20% less energy for flights.
			energyCost *= 0.8
		}
		a.energyBill += energyCost
		a.target = target
		a.mode = mode
		a.waypoint = a.pos
		return true

	case agentModeScavenge:
		source := target.(*essenceSourceNode)
		energyCost := source.pos.DistanceTo(a.pos) * 0.33
		if a.tether {
			energyCost *= 0.5
		}
		if energyCost > a.energy && !a.hasTrait(traitWorkaholic) {
			return false
		}
		a.energyBill += energyCost
		a.mode = agentModeMineEssence
		a.waypoint = roundedPos(source.pos.Sub(gmath.Vec{Y: agentFlightHeight}).Add(a.scene.Rand().Offset(-8, 8)))
		a.target = target
		return true

	case agentModeMineEssence:
		if !a.stats.CanGather {
			return false
		}
		switch a.stats.Kind {
		case gamedata.AgentCourier, gamedata.AgentTrucker:
			// Couriers try to keep their energy for travelling between the bases.
			if a.energy < 120 || a.energyBill > 10 {
				return false
			}
		}
		source := target.(*essenceSourceNode)
		if source.stats == redOilSource && a.stats.Kind != gamedata.AgentRedminer {
			return false
		}
		energyCost := source.pos.DistanceTo(a.pos) * 0.5
		if a.tether {
			energyCost *= 0.5
		}
		if energyCost > a.energy && !a.hasTrait(traitWorkaholic) {
			return false
		}
		a.energyBill += energyCost
		a.mode = mode
		a.waypoint = roundedPos(source.pos.Sub(gmath.Vec{Y: agentFlightHeight}).Add(a.scene.Rand().Offset(-8, 8)))
		a.target = target
		return true

	case agentModeTakeoff:
		a.mode = mode
		a.waypoint = a.pos.Sub(gmath.Vec{Y: agentFlightHeight})
		if a.shadow != nil {
			a.shadow.Visible = false
		}
		return true

	case agentModePickup:
		a.mode = mode
		a.waypoint = a.pos.Add(gmath.Vec{Y: agentFlightHeight})
		return true

	case agentModeRecycleReturn:
		a.mode = mode
		a.waypoint = a.colonyCore.GetEntrancePos().Sub(gmath.Vec{Y: agentFlightHeight})
		return true

	case agentModeRecycleLanding:
		a.mode = mode
		a.waypoint = a.colonyCore.GetEntrancePos()
		return true

	case agentModeRepairTurret:
		energyCost := 40.0
		if energyCost > a.energy && !a.hasTrait(traitWorkaholic) {
			return false
		}
		a.target = target
		a.mode = mode
		a.energyBill += energyCost
		a.dist = a.scene.Rand().FloatRange(3, 4) // repair time
		a.waypoint = gmath.RadToVec(a.scene.Rand().Rad()).Mulf(64.0).Add(target.(*colonyAgentNode).pos)
		return true

	case agentModeRepairBase:
		energyCost := 40.0
		if energyCost > a.energy && !a.hasTrait(traitWorkaholic) {
			return false
		}
		a.mode = mode
		a.energyBill += energyCost
		a.dist = a.scene.Rand().FloatRange(3, 4) // repair time
		a.waypoint = gmath.RadToVec(a.scene.Rand().Rad()).Mulf(64.0).Add(a.colonyCore.pos)
		return true

	case agentModeBuildBuilding:
		construction := target.(*constructionNode)
		energyCost := construction.pos.DistanceTo(a.pos) * 0.6
		if energyCost > a.energy && !a.hasTrait(traitWorkaholic) {
			return false
		}
		a.mode = mode
		a.energyBill += energyCost
		a.dist = a.scene.Rand().FloatRange(5, 7) // build time
		a.target = target
		a.waypoint = gmath.RadToVec(a.scene.Rand().Rad()).Mulf(64.0).Add(construction.pos)
		return true

	case agentModeKamikazeAttack:
		a.waypoint = gmath.Vec{}
		a.mode = mode
		a.target = target
		return true

	case agentModeConsumeDrone:
		a.waypoint = target.(*colonyAgentNode).pos.Add(a.scene.Rand().Offset(-4, 4))
		a.mode = mode
		a.target = target
		return true
	}

	return false
}

func (a *colonyAgentNode) orbitingWaypoint() gmath.Vec {
	var direction gmath.Vec
	if a.pos == a.colonyCore.pos {
		direction = gmath.RadToVec(a.scene.Rand().Rad())
	} else {
		direction = a.pos.DirectionTo(a.colonyCore.pos)
	}
	angle := gmath.Rad(0.4)
	if a.hasTrait(traitCounterClocwiseOrbiting) {
		angle = -0.4
	}
	return direction.Rotated(angle).Mulf(a.dist).Add(a.colonyCore.pos)
}

func (a *colonyAgentNode) Update(delta float64) {
	if a.anim != nil {
		a.anim.Tick(delta)
	}
	a.flashComponent.Update(delta)

	if a.stats.Tier == 1 {
		a.lifetime -= delta
	}

	if a.shadow != nil {
		a.shadow.Pos.Offset.Y = math.Round(a.height + 4)
		newShadowAlpha := float32(1.0 - ((a.height / agentFlightHeight) * 0.5))
		a.shadow.SetAlpha(newShadowAlpha)
	}

	if !a.world().simulation {
		// FIXME: this should be fixed in the ge package.
		a.spritePos.X = math.Round(a.pos.X)
		a.spritePos.Y = math.Round(a.pos.Y)
	}

	if a.energyBill != 0 {
		a.energy -= delta * 2
		a.energyBill = gmath.ClampMin(a.energyBill-delta*2, 0)
	}

	if a.resting {
		a.energy = gmath.ClampMax(a.energy+(delta*0.5), a.maxEnergy)
		if a.energy > a.maxEnergy*0.6 {
			a.resting = false
		}
	} else {
		if a.mode != agentModeStandby && a.mode != agentModeCharging && a.energy < a.maxEnergy*0.5 {
			a.resting = true
		}
	}

	a.slow = gmath.ClampMin(a.slow-delta, 0)
	a.specialDelay = gmath.ClampMin(a.specialDelay-delta, 0)

	if a.cloaking > 0 {
		a.cloaking -= delta
		if a.cloaking <= 0 {
			a.doUncloak()
		}
	}

	a.processAttack(delta)
	a.processSupport(delta)

	switch a.mode {
	case agentModeStandby:
		a.updateStandby(delta)
	case agentModeAlignStandby:
		a.updateAlignStandby(delta)
	case agentModePosing:
		a.updatePosing(delta)
	case agentModeCharging:
		a.updateCharging(delta)
	case agentModeForcedCharging:
		a.updateForcedCharging(delta)
	case agentModeCloakHide:
		a.updateCloakHide(delta)
	case agentModeMineEssence:
		a.updateMineEssence(delta)
	case agentModePickup:
		a.updatePickup(delta)
	case agentModeReturn:
		a.updateReturn(delta)
	case agentModePatrol:
		a.updatePatrol(delta)
	case agentModeMove:
		a.updateMove(delta)
	case agentModePanic:
		a.updatePanic(delta)
	case agentModeCourierFlight:
		a.updateCourierFlight(delta)
	case agentModeFollow:
		a.updateFollow(delta)
	case agentModeWaitCloning:
		a.updateWaitCloning(delta)
	case agentModeMakeClone:
		a.updateMakeClone(delta)
	case agentModeMerging, agentModeMergingRoomba:
		a.updateMerging(delta)
	case agentModeResourceTakeoff:
		a.updateResourceTakeoff(delta)
	case agentModeTakeoff:
		a.updateTakeoff(delta)
	case agentModeRecycleReturn:
		a.updateRecycleReturn(delta)
	case agentModeRecycleLanding:
		a.updateRecycleLanding(delta)
	case agentModeBuildBuilding:
		a.updateBuildBase(delta)
	case agentModeRepairBase:
		a.updateRepairBase(delta)
	case agentModeRepairTurret:
		a.updateRepairTurret(delta)
	case agentModeKamikazeAttack:
		a.updateKamikazeAttack(delta)
	case agentModeConsumeDrone:
		a.updateConsumeDrone(delta)
	case agentModeRoombaPatrol:
		a.updateRoombaPatrol(delta)
	case agentModeRoombaWait:
		a.updateRoombaWait(delta)
	case agentModeGuardForever:
		// Just chill.
	}
}

func (a *colonyAgentNode) Dispose() {
	a.sprite.Dispose()
	if a.shadow != nil {
		a.shadow.Dispose()
	}
	if a.diode != nil {
		a.diode.Dispose()
	}
	if a.cloningBeam != nil {
		a.cloningBeam.Dispose()
		a.cloningBeam = nil
	}
}

func (a *colonyAgentNode) Destroy() {
	a.EventDestroyed.Emit(a)
	a.Dispose()
}

func (a *colonyAgentNode) IsFlying() bool {
	return a.stats.IsFlying
}

func (a *colonyAgentNode) ReceiveEnergyDamage(damage float64) {
	a.energy = gmath.ClampMin(a.energy-damage, 0)
}

func (a *colonyAgentNode) doUncloak() {
	a.cloaking = 0
	a.sprite.SetAlpha(1)
}

func (a *colonyAgentNode) doCloak(d float64) {
	a.cloaking = d
	a.sprite.SetAlpha(0.2)
	a.world().nodeRunner.AddObject(newEffectNode(a.camera(), a.pos, true, assets.ImageCloakWave))
	playSound(a.world(), assets.AudioStealth, a.pos)
}

func (a *colonyAgentNode) explode() {
	if !a.stats.IsFlying {
		createAreaExplosion(a.world(), spriteRect(a.pos, a.sprite), true)
		if a.IsTurret() || a.scene.Rand().Chance(0.3) {
			scraps := a.world().NewEssenceSourceNode(scrapSource, a.pos.Add(gmath.Vec{Y: 2}))
			a.world().nodeRunner.AddObject(scraps)
		}
		return
	}

	playSound(a.world(), assets.AudioAgentDestroyed, a.pos)

	if a.colonyCore.GetSecurityPriority() < 0.4 {
		a.colonyCore.AddPriority(prioritySecurity, 0.04)
	}
	if a.scene.Rand().Chance(0.6) {
		a.colonyCore.AddPriority(priorityGrowth, 0.01)
	}

	roll := a.scene.Rand().Float()
	if roll < 0.3 {
		createExplosion(a.world(), true, a.pos)
	} else {
		var scraps *essenceSourceStats
		if roll > 0.6 {
			scraps = smallScrapSource
			if a.stats.Size != gamedata.SizeSmall {
				scraps = scrapSource
			}
		}

		shadowImg := assets.ImageNone
		if a.shadow != nil {
			shadowImg = a.shadow.ImageID()
		}

		fall := newDroneFallNode(a.world(), scraps, a.stats.Image, shadowImg, a.pos, a.height)
		fall.FrameOffsetY = float64(a.rank) * a.sprite.FrameHeight
		a.world().nodeRunner.AddObject(fall)
	}
}

func (a *colonyAgentNode) OnBuildingRepair(amount float64) {
	// Turrets are 2 times easier to repair than a base, hence x2 multiplier.
	amount *= 2
	if a.health >= a.maxHealth {
		return
	}
	a.health = gmath.ClampMax(a.health+amount, a.maxHealth)
	a.updateHealthShader()
}

func (a *colonyAgentNode) updateHealthShader() {
	if a.sprite.Shader.IsNil() {
		return
	}
	percentage := a.health / a.maxHealth
	a.sprite.Shader.SetFloatValue("HP", percentage)
	a.sprite.Shader.Enabled = percentage < 0.95
}

func (a *colonyAgentNode) CanAttack(mask gamedata.TargetKind) bool {
	return a.stats.Weapon != nil && a.stats.Weapon.TargetFlags&mask != 0
}

func (a *colonyAgentNode) IsCloaked() bool {
	return a.cloaking > 0
}

func (a *colonyAgentNode) onLowHealthDamage(source targetable) {
	if a.stats.Kind == gamedata.AgentKamikaze && source.IsFlying() {
		switch a.mode {
		case agentModePatrol, agentModeStandby, agentModeMineEssence, agentModeReturn, agentModeFollow:
			if creep, ok := source.(*creepNode); ok {
				a.health = gmath.ClampMax(a.health+10, a.maxHealth)
				a.dist = 0.1 + a.scene.Rand().FloatRange(0.05, 0.25)
				a.AssignMode(agentModeKamikazeAttack, gmath.Vec{}, creep)
				return
			}
		}
	}

	// Don't do anything weird when colony is being relocated.
	if a.colonyCore.mode != colonyModeNormal {
		return
	}

	switch a.mode {
	case agentModeStandby, agentModeFollow, agentModePatrol:
		// OK, can interrupt.
	default:
		// Most modes can't be safely be interrupted like this.
		return
	}

	if a.stats.CanCloak && !a.IsCloaked() && a.specialDelay == 0 {
		a.AssignMode(agentModeCloakHide, gmath.Vec{}, nil)
		a.doCloak(a.scene.Rand().FloatRange(6, 10))
		a.specialDelay = a.scene.Rand().FloatRange(6, 10)
		return
	}

	switch {
	case a.hasTrait(traitLowHPBerserk):
		// Berserks go straight into the danger when low on health.
		a.AssignMode(agentModeMove, source.GetPos().Add(a.scene.Rand().Offset(-20, 20)), nil)
	case a.hasTrait(traitLowHPRecycle):
		// Recycle agents may go to recycle themselves on low health.
		if a.scene.Rand().Chance(0.8) {
			a.AssignMode(agentModeRecycleReturn, gmath.Vec{}, nil)
		}
	case a.hasTrait(traitLowHPRetreat):
		// Agents with retreat trait will try to fly away from a threat on low health.
		pos := retreatPos(a.scene.Rand(), a.scene.Rand().FloatRange(80, 140), a.pos, *source.GetPos())
		a.AssignMode(agentModeMove, pos, nil)
	case a.hasTrait(traitLowHPPanic):
		// Agents with panic trait will stop what they're doing and fly like crazy.
		a.AssignMode(agentModePanic, gmath.Vec{}, nil)
	}
}

func (a *colonyAgentNode) OnDamage(damage gamedata.DamageValue, source targetable) {
	a.health -= damage.Health

	if a.health < 0 {
		a.explode()
		a.Destroy()
		return
	}

	if !a.IsTurret() {
		if a.colonyCore.GetSecurityPriority() < 0.3 && a.scene.Rand().Chance(1.0-a.colonyCore.GetSecurityPriority()) {
			a.colonyCore.AddPriority(prioritySecurity, 0.01)
		}
	}

	a.energy = gmath.ClampMin(a.energy-damage.Energy, 0)
	a.slow = gmath.ClampMax(a.slow+damage.Slow, 5)

	if damage.Health != 0 {
		a.flashComponent.flash = 0.2
		if a.IsTurret() {
			a.updateHealthShader()
		}
	}

	if a.health <= (a.maxHealth*0.33) || a.health <= damage.Health {
		a.onLowHealthDamage(source)
	}

	if damage.Morale != 0 && a.stats.IsFlying {
		switch a.mode {
		case agentModeMineEssence:
			a.clearCargo()
			a.AssignMode(agentModeStandby, gmath.Vec{}, nil)

		case agentModePatrol, agentModeStandby, agentModeFollow:
			if !a.scene.Rand().Chance(damage.Morale) {
				break
			}
			effectRoll := a.scene.Rand().Float()
			if effectRoll < 0.4 {
				a.AssignMode(agentModePanic, gmath.Vec{}, nil)
			} else {
				pos := retreatPos(a.scene.Rand(), a.scene.Rand().FloatRange(80, 140), a.pos, *source.GetPos())
				a.AssignMode(agentModeMove, pos, nil)
			}
		}
	}
}

func (a *colonyAgentNode) GetPos() *gmath.Vec { return &a.pos }

func (a *colonyAgentNode) GetVelocity() gmath.Vec {
	if a.waypoint.IsZero() {
		return gmath.Vec{}
	}
	return a.pos.VecTowards(a.waypoint, a.movementSpeed())
}

func (a *colonyAgentNode) processSupport(delta float64) {
	if !a.stats.HasSupport {
		return
	}

	a.supportDelay = gmath.ClampMin(a.supportDelay-(delta*a.reloadRate), 0)

	if a.supportDelay != 0 {
		return
	}

	setDelay := true
	switch a.stats.Kind {
	case gamedata.AgentRecharger:
		a.doRecharge()
	case gamedata.AgentRepair:
		a.doRepair()
	case gamedata.AgentScavenger, gamedata.AgentMarauder:
		a.doScavenge()
	case gamedata.AgentDisintegrator:
		// Reload depends on the target being there or not.
		setDelay = false
		a.doDisintegratorAttack()
	case gamedata.AgentDevourer:
		a.doConsumeDrone()
	case gamedata.AgentTetherBeacon:
		setDelay = false
		a.doTether()
	}
	if setDelay {
		a.supportDelay = a.stats.SupportReload * a.scene.Rand().FloatRange(0.7, 1.4)
	}
}

func (a *colonyAgentNode) doConsumeDrone() {
	if a.colonyCore.mode != colonyModeNormal {
		return
	}
	if a.mode != agentModeStandby && a.mode != agentModePatrol {
		return
	}

	if a.colonyCore.resources < 100 {
		return
	}
	if a.colonyCore.agents.NumAvailableWorkers() < 5 || a.colonyCore.agents.NumAvailableFighters() < 5 {
		return
	}

	if a.devourerLevel >= gamedata.DevourerMaxLevel {
		// A max-developed devourer will only consume for healing.
		if a.health >= (a.maxHealth * 0.6) {
			return
		}
	} else {
		// A developing devourer may consume even on full health, sometimes.
		if a.health >= a.maxHealth && a.scene.Rand().Chance(0.65) {
			return
		}
	}

	// Prefer the kind of drones that is less scarce.
	bestKind := gamedata.AgentWorker
	if a.colonyCore.agents.NumAvailableFighters() > a.colonyCore.agents.NumAvailableWorkers() {
		bestKind = gamedata.AgentScout
	}

	var bestTarget *colonyAgentNode
	bestScore := 0.0
	a.colonyCore.agents.Find(searchWorkers|searchFighters|searchRandomized|searchOnlyAvailable, func(x *colonyAgentNode) bool {
		if x.stats.Tier != 1 {
			return false
		}
		if x.stats.Kind == gamedata.AgentScout && x.health == x.maxHealth {
			return false
		}
		score := 2.0
		if a.stats.Kind == bestKind {
			score += 0.5
		}
		if a.faction == gamedata.NeutralFactionTag {
			score += 0.5
		}
		if a.rank != 0 {
			score -= float64(a.rank) * 0.5
		}
		multiplier := (2.0 - (x.health / x.maxHealth)) + (1.2 - (x.energy / x.maxEnergy))
		score *= multiplier
		if score > bestScore {
			bestTarget = x
			bestScore = score
		}
		return false
	})
	if bestTarget == nil {
		return
	}

	// Make it wait for the devourer to come closer.
	// If anything goes wrong, it will get back to normal after some time.
	// 8 seconds should be enough.
	bestTarget.AssignMode(agentModePosing, gmath.Vec{X: 8}, nil)

	a.AssignMode(agentModeConsumeDrone, gmath.Vec{}, bestTarget)
}

func (a *colonyAgentNode) tetherTarget(target *colonyAgentNode) {
	target.tether = true
	if target.energyBill > 50 {
		target.energyBill = gmath.ClampMin(target.energyBill-20, 50)
	}
	a.world().nodeRunner.AddObject(newTetherNode(a.world(), a, target))
	playSound(a.world(), assets.AudioTetherShot, a.pos)
}

func (a *colonyAgentNode) doTether() {
	// If it's connected to the colony, it can't boost anyone else.
	if a.target != nil {
		if tether, ok := a.target.(*tetherNode); ok {
			a.supportDelay = a.scene.Rand().FloatRange(0.5, 2)
			if !tether.IsDisposed() {
				return
			}
			a.target = nil
		}
	}

	colonyTarget := randIterate(a.scene.Rand(), a.world().colonies, func(colony *colonyCoreNode) bool {
		if !colony.IsFlying() {
			return false
		}
		return colony.pos.DistanceSquaredTo(a.pos) <= a.stats.SupportRangeSqr
	})
	if colonyTarget != nil && a.target != colonyTarget {
		tether := newTetherNode(a.world(), a, colonyTarget)
		a.target = tether
		colonyTarget.tether++
		a.world().nodeRunner.AddObject(tether)
		playSound(a.world(), assets.AudioTetherShot, a.pos)
		a.supportDelay = a.stats.SupportReload * a.scene.Rand().FloatRange(0.95, 1.35)
		return
	}

	const maxNumberOfTargets = 4
	actionsLeft := maxNumberOfTargets
	actionsLeft -= a.walkTetherTargets(a.colonyCore, actionsLeft, func(x *colonyAgentNode) {
		a.tetherTarget(x)
	})
	if actionsLeft != 0 {
		randIterate(a.scene.Rand(), a.world().colonies, func(colony *colonyCoreNode) bool {
			if actionsLeft <= 0 {
				return true
			}
			if colony == a.colonyCore {
				return false
			}
			actionsLeft -= a.walkTetherTargets(colony, actionsLeft, func(x *colonyAgentNode) {
				a.tetherTarget(x)
			})
			return actionsLeft <= 0
		})
	}

	if actionsLeft != maxNumberOfTargets {
		a.supportDelay = a.stats.SupportReload * a.scene.Rand().FloatRange(0.95, 1.35)
		return
	}
	a.supportDelay = a.scene.Rand().FloatRange(0.5, 2)
}

func (a *colonyAgentNode) doDisintegratorAttack() {
	switch a.mode {
	case agentModePatrol, agentModeStandby, agentModeFollow, agentModeMineEssence:
		// OK
	default:
		return
	}

	const attackEnergyCost = 40.0
	if a.energy < attackEnergyCost || a.height != agentFlightHeight {
		return
	}
	targets := a.findAttackTargets()
	if len(targets) == 0 {
		a.supportDelay = a.scene.Rand().FloatRange(0.15, 1.2)
		return
	}
	a.energy -= attackEnergyCost
	a.supportDelay = a.stats.SupportReload * a.scene.Rand().FloatRange(0.8, 1.2)
	target := targets[0]
	toPos := snipePos(a.stats.Weapon.ProjectileSpeed, a.pos, *target.GetPos(), target.GetVelocity())
	p := a.world().newProjectileNode(projectileConfig{
		World:    a.world(),
		Weapon:   a.stats.Weapon,
		Attacker: a,
		ToPos:    toPos,
		Target:   target,
	})
	a.world().nodeRunner.AddProjectile(p)
	a.AssignMode(agentModeForcedCharging, gmath.Vec{}, nil)
	playSound(a.world(), a.stats.Weapon.AttackSound, a.pos)
	a.world().nodeRunner.AddObject(newEffectNode(a.camera(), a.pos, true, assets.ImagePurpleIonZap))
	a.specialDelay = a.scene.Rand().FloatRange(9, 12)
}

func (a *colonyAgentNode) doScavenge() {
	if a.colonyCore.mode != colonyModeNormal {
		return
	}
	if a.mode != agentModeStandby && a.mode != agentModePatrol {
		return
	}
	if a.energy < 20 || a.energyBill > 100 {
		return
	}
	if a.colonyCore.resources > maxVisualResources {
		return
	}

	maxDistSqr := 256.0 * 256.0
	if a.stats.Kind == gamedata.AgentMarauder {
		maxDistSqr = 300.0 * 300.0
	}

	var bestSource *essenceSourceNode
	bestScore := 0.0
	for _, source := range a.world().essenceSources {
		switch source.stats {
		case smallScrapCreepSource, scrapCreepSource, bigScrapCreepSource, smallScrapSource, scrapSource:
			// OK
		default:
			continue // Not a scrap resource
		}
		distSqr := a.pos.DistanceSquaredTo(source.pos)
		if distSqr > maxDistSqr {
			continue
		}
		score := distSqr * a.scene.Rand().FloatRange(0.6, 1.6)
		if score != 0 && score > bestScore {
			bestScore = score
			bestSource = source
		}
	}
	if bestSource != nil {
		a.AssignMode(agentModeScavenge, gmath.Vec{}, bestSource)
		if a.stats.Kind == gamedata.AgentMarauder && a.specialDelay == 0 {
			a.doCloak(20)
			a.specialDelay = 10
		}
	}
}

func (a *colonyAgentNode) doRecharge() {
	const rechargerEnergyRecorery float64 = 25.0
	target := a.colonyCore.agents.Find(searchWorkers|searchFighters|searchRandomized, func(x *colonyAgentNode) bool {
		return x != a &&
			x.mode != agentModeKamikazeAttack &&
			(x.energy+rechargerEnergyRecorery) < x.maxEnergy &&
			x.pos.DistanceTo(a.pos) < gamedata.RechargeAgentStats.SupportRange
	})
	if target != nil {
		target.energy = gmath.ClampMax(target.energy+rechargerEnergyRecorery, target.maxEnergy)
		a.world().nodeRunner.AddObject(a.createBeam(target, gamedata.RechargeAgentStats))
		playSound(a.world(), assets.AudioRechargerBeam, a.pos)
	}
}

func (a *colonyAgentNode) createBeam(target targetable, beamStats *gamedata.AgentStats) *beamNode {
	from := ge.Pos{Base: &a.pos, Offset: gmath.Vec{Y: a.stats.FireOffset}}
	to := ge.Pos{Base: target.GetPos(), Offset: gmath.Vec{Y: -2}}
	if beamStats.BeamTexture == nil {
		beam := newBeamNode(a.world(), from, to, beamStats.BeamColor)
		beam.width = beamStats.BeamWidth
		return beam
	}
	return newTextureBeamNode(a.world(), from, to, beamStats.BeamTexture, beamStats.BeamSlideSpeed, beamStats.BeamOpaqueTime)
}

func (a *colonyAgentNode) doRepair() {
	target := a.colonyCore.agents.Find(searchWorkers|searchFighters|searchRandomized, func(x *colonyAgentNode) bool {
		return x != a &&
			x.mode != agentModeKamikazeAttack &&
			x.health < x.maxHealth &&
			x.pos.DistanceTo(a.pos) < gamedata.RepairAgentStats.SupportRange
	})
	if target != nil {
		a.world().nodeRunner.AddObject(a.createBeam(target, gamedata.RepairAgentStats))
		target.health = gmath.ClampMax(target.health+3, target.maxHealth)
		playSound(a.world(), assets.AudioRepairBeam, a.pos)
	}
}

func (a *colonyAgentNode) walkTetherTargets(colony *colonyCoreNode, num int, f func(x *colonyAgentNode)) int {
	targets := a.world().tmpTargetSlice[:0]
	processed := 0
	colony.agents.Find(searchWorkers|searchRandomized, func(x *colonyAgentNode) bool {
		if processed >= num {
			return true
		}
		if x.tether {
			return false
		}
		if x.pos.DistanceSquaredTo(a.pos) > a.stats.SupportRangeSqr {
			return false
		}
		switch x.mode {
		case agentModeKamikazeAttack, agentModeConsumeDrone, agentModeCharging, agentModeForcedCharging, agentModePanic, agentModeWaitCloning, agentModeMakeClone, agentModeRecycleReturn, agentModeRecycleLanding, agentModeMerging, agentModePosing:
			// Modes that are never targeted.
			return false
		}

		switch x.mode {
		case agentModeMineEssence, agentModeReturn, agentModeCourierFlight:
			// The best modes to be hastened.
			processed++
			f(x)
		default:
			if len(targets) < num {
				targets = append(targets, x)
			}
		}
		return processed >= num
	})
	if processed < num {
		for _, target := range targets {
			f(target.(*colonyAgentNode))
			processed++
			if processed >= num {
				break
			}
		}
	}
	return processed
}

func (a *colonyAgentNode) findAttackTargets() []targetable {
	w := a.world()
	creeps := w.creeps
	if len(creeps) == 0 {
		return nil
	}

	// Find a sector that contains this object.
	cellX, cellY := w.GetPosCell(a.pos)
	cellRect := w.GetCellRect(cellX, cellY)

	// Determine how many sectors we need to consider.
	// In the simplest case, it's a single sector,
	// but sometimes we need to check the adjacent sectors too.
	startX := cellX
	startY := cellY
	endX := cellX
	endY := cellY
	attackRange := a.stats.Weapon.AttackRange
	leftmostPos := gmath.Vec{X: a.pos.X - attackRange, Y: a.pos.Y - attackRange}
	rightmostPos := gmath.Vec{X: a.pos.X + attackRange, Y: a.pos.Y + attackRange}
	if leftmostPos.X < cellRect.Min.X {
		delta := cellRect.Min.X - leftmostPos.X
		startX -= int(math.Ceil((attackRange - delta) * w.creepClusterMultiplier))
	}
	if rightmostPos.X > cellRect.Max.X {
		delta := rightmostPos.X - cellRect.Max.X
		endX += int(math.Ceil((attackRange - delta) * w.creepClusterMultiplier))
	}
	if leftmostPos.Y < cellRect.Min.Y {
		delta := cellRect.Min.Y - leftmostPos.Y
		startY -= int(math.Ceil((attackRange - delta) * w.creepClusterMultiplier))
	}
	if rightmostPos.Y > cellRect.Max.Y {
		delta := rightmostPos.Y - cellRect.Max.Y
		endY += int(math.Ceil((attackRange - delta) * w.creepClusterMultiplier))
	}

	maxTargets := a.stats.Weapon.MaxTargets
	targets := w.tmpTargetSlice[:0]
	maybeAppendTarget := func(creep *creepNode) bool {
		if a.isValidTarget(creep) {
			targets = append(targets, creep)
		}
		return len(targets) >= maxTargets
	}

	startX = gmath.Clamp(startX, 0, 7)
	startY = gmath.Clamp(startY, 0, 7)
	endX = gmath.Clamp(endX, 0, 7)
	endY = gmath.Clamp(endY, 0, 7)
	numStepsX := endX - startX + 1
	numStepsY := endY - startY + 1

	// Now decide the sector traversal order.
	// This is needed to add some randomness to the target selection.
	dx := 1
	if a.scene.Rand().Bool() {
		dx = -1
		startX = endX
	}
	dy := 1
	if a.scene.Rand().Bool() {
		dy = -1
		startY = endY
	}

	for i, y := 0, startY; i < numStepsY; i, y = i+1, y+dy {
		for j, x := 0, startX; j < numStepsX; j, x = j+1, x+dx {
			clusterCreeps := w.creepClusters[y][x]
			randIterate(w.rand, clusterCreeps, maybeAppendTarget)
			if len(targets) >= maxTargets {
				return targets
			}
		}
	}

	// New creeps are created outside of the map, so they end up
	// in the fallback cluster that includes everything that is out of bounds.
	if len(w.fallbackCreepCluster) != 0 {
		randIterate(w.rand, w.fallbackCreepCluster, maybeAppendTarget)
	}

	return targets
}

func (a *colonyAgentNode) isValidTarget(creep *creepNode) bool {
	if creep.IsCloaked() {
		return false
	}
	if !a.CanAttack(creep.TargetKind()) {
		return false
	}
	if creep.pos.DistanceSquaredTo(a.pos) > a.stats.Weapon.AttackRangeSqr {
		return false
	}
	return true
}

func (a *colonyAgentNode) attackTargets(targets []targetable, burstSize int) {
	for _, target := range targets {
		if a.stats.Weapon.ProjectileSpeed != 0 {
			toPos := snipePos(a.stats.Weapon.ProjectileSpeed, a.pos, *target.GetPos(), target.GetVelocity())
			j := 0
			attacksPerBurst := a.stats.Weapon.AttacksPerBurst
			for i := 0; i < burstSize; i += attacksPerBurst {
				if i+attacksPerBurst > burstSize {
					// This happens only once for the last burst wave
					// if attacks-per-burst are not aligned with burstSize (like with Devourer).
					attacksPerBurst = burstSize - i
				}
				for i := 0; i < attacksPerBurst; i++ {
					fireDelay := float64(j) * a.stats.Weapon.BurstDelay
					p := a.world().newProjectileNode(projectileConfig{
						World:     a.world(),
						Weapon:    a.stats.Weapon,
						Attacker:  a,
						ToPos:     toPos,
						Target:    target,
						FireDelay: fireDelay,
					})
					a.world().nodeRunner.AddProjectile(p)
				}
				j++
			}
		} else {
			// TODO: this code is duplited with creep node.
			if a.stats.BeamTexture == nil {
				beam := newBeamNode(a.world(), ge.Pos{Base: &a.pos, Offset: a.stats.Weapon.FireOffset}, ge.Pos{Base: target.GetPos()}, a.stats.BeamColor)
				beam.width = a.stats.BeamWidth
				a.world().nodeRunner.AddObject(beam)
			} else {
				beam := newTextureBeamNode(a.world(), ge.Pos{Base: &a.pos, Offset: a.stats.Weapon.FireOffset}, ge.Pos{Base: target.GetPos()}, a.stats.BeamTexture, a.stats.BeamSlideSpeed, a.stats.BeamOpaqueTime)
				a.world().nodeRunner.AddObject(beam)
			}
			target.OnDamage(multipliedDamage(target, a.stats.Weapon), a)
		}
	}
}

func (a *colonyAgentNode) processAttack(delta float64) {
	if a.stats.Weapon == nil || a.stats.Kind == gamedata.AgentDisintegrator {
		return
	}

	a.attackDelay = gmath.ClampMin(a.attackDelay-(delta*a.reloadRate), 0)
	if a.attackDelay != 0 {
		return
	}
	if a.IsCloaked() {
		return
	}

	targets := a.findAttackTargets()
	if len(targets) == 0 {
		a.attackDelay = 0.75 * a.scene.Rand().FloatRange(0.8, 1.4)
		return
	}

	reloadMultiplier := a.scene.Rand().FloatRange(0.8, 1.2)
	if a.stats == gamedata.BeamTowerAgentStats {
		reloadMultiplier += (a.specialDelay * 0.3)
		a.specialDelay += ((a.stats.Weapon.Reload) + 1.75) * reloadMultiplier
	}

	a.attackDelay = a.stats.Weapon.Reload * reloadMultiplier

	switch a.stats.Kind {
	case gamedata.AgentDestroyer:
		target := targets[0]
		offset := gmath.Vec{X: -7, Y: 2}
		offsetStep := gmath.Vec{X: 14}
		targetOffset := gmath.Vec{X: -4}
		targetOffsetStep := gmath.Vec{X: 8}
		for i := 0; i < 2; i++ {
			pos1 := ge.Pos{Base: &a.pos, Offset: offset}
			pos2 := ge.Pos{Base: target.GetPos(), Offset: targetOffset}
			beam := newBeamNode(a.world(), pos1, pos2, destroyerBeamColor)
			beam.width = 2
			a.world().nodeRunner.AddObject(beam)
			offset = offset.Add(offsetStep)
			targetOffset = targetOffset.Add(targetOffsetStep)
		}
		target.OnDamage(multipliedDamage(target, a.stats.Weapon), a)

	case gamedata.AgentPrism:
		target := targets[0]
		damage := a.stats.Weapon.Damage
		width := 1.0
		numReflections := 0
		pos := &a.pos
		const maxReflections = 4
		a.colonyCore.agents.Find(searchFighters|searchRandomized, func(ally *colonyAgentNode) bool {
			if ally.stats.Kind != gamedata.AgentPrism || ally == a {
				return false
			}
			if ally.pos.DistanceSquaredTo(*pos) > (196 * 196) {
				return false
			}
			ally.attackDelay += float64(numReflections) * 0.1
			beam := newBeamNode(a.world(), ge.Pos{Base: pos}, ge.Pos{Base: &ally.pos}, prismBeamColors[numReflections])
			beam.width = width
			a.world().nodeRunner.AddObject(beam)
			numReflections++
			damage.Health++
			if numReflections < maxReflections {
				width++
			}
			pos = &ally.pos
			return numReflections >= maxReflections
		})
		if numReflections == maxReflections {
			damage.Health++
		}
		beam := newBeamNode(a.world(), ge.Pos{Base: pos}, ge.Pos{Base: target.GetPos()}, prismBeamColors[numReflections])
		beam.width = width
		a.world().nodeRunner.AddObject(beam)
		damage.Health *= damageMultiplier(target, a.stats.Weapon)
		target.OnDamage(damage, a)

	case gamedata.AgentDevourer:
		// Every consumed drone gives +1 to the power level.
		// Every power level gives +1 projectile (burst size).
		burstSize := a.stats.Weapon.BurstSize + a.devourerLevel
		a.attackTargets(targets, burstSize)

	default:
		a.attackTargets(targets, a.stats.Weapon.BurstSize)
	}

	playSound(a.world(), a.stats.Weapon.AttackSound, a.pos)
}

func (a *colonyAgentNode) movementSpeed() float64 {
	var baseSpeed float64
	switch a.mode {
	case agentModeKamikazeAttack:
		return 2 * a.speed
	case agentModeTakeoff, agentModeRecycleLanding:
		return 30
	case agentModePickup, agentModeResourceTakeoff, agentModeAlignStandby:
		baseSpeed = agentPickupSpeed
	default:
		baseSpeed = a.speed
	}
	multiplier := 1.0
	if a.resting {
		multiplier = 0.5
	}
	if a.slow > 0 {
		multiplier *= 0.55
	}
	if a.tether {
		multiplier *= 2.0
	}
	return baseSpeed * multiplier
}

func (a *colonyAgentNode) moveTowardsWithSpeed(delta, speed float64, pos gmath.Vec) bool {
	var reached bool
	a.pos, reached = moveTowardsWithSpeed(a.pos, pos, delta, speed)
	return reached
}

func (a *colonyAgentNode) moveTowards(delta float64, pos gmath.Vec) bool {
	return a.moveTowardsWithSpeed(delta, a.movementSpeed(), pos)
}

func (a *colonyAgentNode) updatePatrol(delta float64) {
	if a.moveTowards(delta, a.waypoint) {
		a.dist = a.colonyCore.PatrolRadius()
		a.waypoint = a.orbitingWaypoint()
		a.waypointsLeft--
		if a.waypointsLeft == 0 {
			a.AssignMode(agentModeStandby, gmath.Vec{}, nil)
			return
		}
	}
}

func (a *colonyAgentNode) updateWaitCloning(delta float64) {
	cloner := a.target.(*colonyAgentNode)
	if cloner.IsDisposed() {
		a.AssignMode(agentModeStandby, gmath.Vec{}, nil)
		return
	}
}

func (a *colonyAgentNode) updateTakeoff(delta float64) {
	a.height += delta * 30
	if a.moveTowards(delta, a.waypoint) {
		a.height = agentFlightHeight
		if a.shadow != nil {
			a.shadow.Visible = true
		}
		a.AssignMode(agentModeStandby, gmath.Vec{}, nil)
	}
}

func (a *colonyAgentNode) updateRecycleReturn(delta float64) {
	if a.moveTowards(delta, a.waypoint) {
		a.colonyCore.openHatchTime = 1.5
		a.AssignMode(agentModeRecycleLanding, gmath.Vec{}, nil)
	}
}

func (a *colonyAgentNode) updateRepairBase(delta float64) {
	if !a.waypoint.IsZero() {
		if a.moveTowards(delta, a.waypoint) {
			a.waypoint = gmath.Vec{}
			buildPos := ge.Pos{
				Base:   &a.colonyCore.pos,
				Offset: gmath.Vec{X: a.scene.Rand().FloatRange(-18, 18)},
			}
			beam := newCloningBeamNode(a.world(), false, &a.pos, buildPos)
			a.cloningBeam = beam
			a.world().nodeRunner.AddObject(beam)
			return
		}
		return
	}
	a.dist -= delta
	if a.dist <= 0 {
		a.AssignMode(agentModeStandby, gmath.Vec{}, nil)
		a.colonyCore.OnHeal(a.scene.Rand().FloatRange(3, 5))
		return
	}
}

func (a *colonyAgentNode) updateRoombaWait(delta float64) {
	a.dist -= delta
	a.health = gmath.ClampMax(a.health+(delta*0.2), a.maxHealth)
	a.energy = gmath.ClampMax(a.energy+(delta*2), a.maxEnergy)
	if a.dist <= 0 {
		a.mode = agentModeRoombaPatrol
	}
}

func (a *colonyAgentNode) sendTo(pos gmath.Vec) {
	p := a.world().BuildPath(a.pos, pos)
	a.path = p.Steps
	a.waypoint = a.world().pathgrid.AlignPos(a.pos)
}

func (a *colonyAgentNode) updateRoombaPatrol(delta float64) {
	if a.energy < 0 {
		// Discharged. It needs some time to recover.
		a.mode = agentModeRoombaWait
		a.dist = a.scene.Rand().FloatRange(5, 25)
		a.waypoint = gmath.Vec{}
		a.energy = a.scene.Rand().FloatRange(10, 20)
		return
	}

	// Moving towards destination (or a target).
	if !a.waypoint.IsZero() {
		a.energy -= 3 * delta
		if a.moveTowards(delta, a.waypoint) {
			if a.target != nil {
				target := a.target.(*creepNode)
				if target.IsDisposed() {
					a.target = nil
				} else if a.pos.DistanceSquaredTo(target.pos) <= (a.stats.Weapon.AttackRangeSqr * a.supportDelay) {
					a.mode = agentModeRoombaWait
					a.dist = a.scene.Rand().FloatRange(7, 11)
					a.waypoint = gmath.Vec{}
					a.target = nil
					return
				}
			}
			if a.path.HasNext() {
				if a.health < a.maxHealth*0.8 && a.scene.Rand().Chance(0.1) {
					a.mode = agentModeRoombaWait
					a.dist = a.scene.Rand().FloatRange(1, 10)
					a.waypoint = gmath.Vec{}
					return
				}
				// TODO: remove code duplication with crawlers.
				d := a.path.Next()
				aligned := a.world().pathgrid.AlignPos(a.pos)
				a.waypoint = posMove(aligned, d).Add(a.world().rand.Offset(-4, 4))
				return
			}
			a.waypoint = gmath.Vec{}
		}
		return
	}

	if a.target != nil {
		target := a.target.(*creepNode)
		if target.IsDisposed() {
			a.target = nil
		} else {
			a.sendTo(target.pos.Add(a.scene.Rand().Offset(-80, 80)))
			return
		}
	}

	// Try to find a new target.
	newTarget := randIterate(a.scene.Rand(), a.world().creeps, func(creep *creepNode) bool {
		switch creep.stats.kind {
		case creepBase, creepCrawlerBase, creepTurret, creepHowitzer:
			return true
		default:
			return false
		}
	})
	if newTarget != nil {
		a.supportDelay = a.scene.Rand().FloatRange(0.4, 0.95)
		a.target = newTarget
		a.sendTo(newTarget.pos.Add(a.scene.Rand().Offset(-80, 80)))
	} else {
		if a.scene.Rand().Chance(0.4) {
			targetPos := correctedPos(a.world().rect, randomSectorPos(a.scene.Rand(), a.world().rect), 480)
			a.sendTo(targetPos)
		} else {
			a.mode = agentModeRoombaWait
			a.dist = a.scene.Rand().FloatRange(2, 5)
		}
	}
}

func (a *colonyAgentNode) updateConsumeDrone(delta float64) {
	target := a.target.(*colonyAgentNode)
	if target.IsDisposed() || target.mode != agentModePosing {
		a.AssignMode(agentModeStandby, gmath.Vec{}, nil)
		return
	}

	if a.moveTowards(delta, a.waypoint) {
		// Give a partial drone cost refund.
		playSound(a.world(), assets.AudioAgentConsumed, a.pos)
		a.colonyCore.resources += target.stats.Cost * 0.5
		a.colonyCore.eliteResources += float64(target.rank)
		if a.devourerLevel < gamedata.DevourerMaxLevel {
			a.devourerLevel++
			a.maxHealth += 5
		}
		a.health = gmath.ClampMax(a.health+target.maxHealth*2, a.maxHealth)
		target.Destroy()
		a.world().nodeRunner.AddObject(newEffectNode(a.camera(), a.pos, true, assets.ImageDroneConsumed))
	}
}

func (a *colonyAgentNode) updateKamikazeAttack(delta float64) {
	creep := a.target.(*creepNode)

	if creep.IsDisposed() {
		a.AssignMode(agentModeStandby, gmath.Vec{}, nil)
		return
	}

	if a.dist > 0 {
		a.dist -= delta
		if a.dist <= 0 {
			a.dist = -1
			playSound(a.world(), assets.AudioKamizakeAttack, a.pos)
		}
	}

	if a.waypoint.IsZero() {
		a.waypoint = a.getCloserWaypoint(creep.pos, 4, 16)
	}

	const explosionRangeSqr float64 = 34 * 34
	const explosionDamage float64 = 35.0
	if a.moveTowards(delta, a.waypoint) {
		if a.pos.DistanceSquaredTo(creep.pos) > explosionRangeSqr {
			a.waypoint = gmath.Vec{}
			return
		}
		a.world().nodeRunner.AddObject(newEffectNode(a.world().camera, a.pos, true, assets.ImageBigVerticalExplosion))
		playExplosionSound(a.world(), a.pos)
		creep.OnDamage(gamedata.DamageValue{Health: explosionDamage}, a)
		for _, otherCreep := range a.world().creeps {
			if !otherCreep.IsFlying() || otherCreep == creep {
				continue
			}
			distSqr := otherCreep.pos.DistanceSquaredTo(a.pos)
			if distSqr > explosionRangeSqr {
				continue
			}
			otherCreep.OnDamage(gamedata.DamageValue{Health: explosionDamage * 0.5}, a)
		}
		a.Destroy()
		return
	}
}

func (a *colonyAgentNode) updateRepairTurret(delta float64) {
	target := a.target.(*colonyAgentNode)
	if target.IsDisposed() {
		a.AssignMode(agentModeStandby, gmath.Vec{}, nil)
		return
	}
	if !a.waypoint.IsZero() {
		if a.moveTowards(delta, a.waypoint) {
			a.waypoint = gmath.Vec{}
			buildPos := ge.Pos{
				Base:   &target.pos,
				Offset: gmath.Vec{X: a.scene.Rand().FloatRange(-10, 10)},
			}
			beam := newCloningBeamNode(a.world(), false, &a.pos, buildPos)
			a.cloningBeam = beam
			a.world().nodeRunner.AddObject(beam)
			return
		}
		return
	}
	a.dist -= delta
	if a.dist <= 0 {
		a.AssignMode(agentModeStandby, gmath.Vec{}, nil)
		amountRepaired := a.scene.Rand().FloatRange(5, 8)
		if a.faction == gamedata.GreenFactionTag {
			amountRepaired *= 1.5
		}
		target.OnBuildingRepair(amountRepaired)
		return
	}
}

func (a *colonyAgentNode) updateBuildBase(delta float64) {
	target := a.target.(*constructionNode)
	if target.IsDisposed() {
		if a.cloningBeam != nil {
			a.cloningBeam.Dispose()
		}
		a.AssignMode(agentModeStandby, gmath.Vec{}, nil)
		return
	}
	if !a.waypoint.IsZero() {
		if a.moveTowards(delta, a.waypoint) {
			target.attention += 2
			a.waypoint = gmath.Vec{}
			buildPos := target.GetConstructPos()
			beam := newCloningBeamNode(a.world(), false, &a.pos, buildPos)
			a.cloningBeam = beam
			a.world().nodeRunner.AddObject(beam)
			return
		}
		return
	}
	amountConstructed := delta
	if a.faction == gamedata.GreenFactionTag {
		amountConstructed *= 1.5
	}
	if target.Construct(amountConstructed, a.colonyCore) {
		return
	}
	a.dist -= delta
	if a.dist <= 0 || a.energy < 20 {
		a.AssignMode(agentModeStandby, gmath.Vec{}, nil)
		return
	}
}

func (a *colonyAgentNode) updateRecycleLanding(delta float64) {
	height := a.height
	a.height -= delta * 30
	if height >= 3 && a.height < 3 {
		a.sprite.SetColorScaleRGBA(200, 200, 200, 255)
	}
	if a.moveTowards(delta, a.waypoint) {
		a.colonyCore.resources += a.stats.Cost * 0.9
		if a.rank != 0 {
			a.colonyCore.eliteResources += float64(a.rank)
		}
		playSound(a.world(), assets.AudioAgentRecycled, a.pos)
		a.Destroy()
	}
}

func (a *colonyAgentNode) updateMerging(delta float64) {
	target := a.target.(*colonyAgentNode)
	if target.IsDisposed() {
		if a.cloningBeam != nil {
			a.cloningBeam.Dispose()
		}
		a.AssignMode(agentModeStandby, gmath.Vec{}, nil)
		return
	}
	if a.waypoint.IsZero() {
		dist := target.pos.DistanceTo(a.pos)
		if dist > 64 {
			a.waypoint = a.pos.MoveTowards(target.pos, dist-20).Add(a.scene.Rand().Offset(-8, 8))
			return
		}
	}
	if !a.waypoint.IsZero() {
		if a.moveTowards(delta, a.waypoint) {
			a.waypoint = gmath.Vec{}
		}
		return
	}
	if a.cloningBeam == nil {
		beam := newCloningBeamNode(a.world(), true, &a.pos, ge.Pos{Base: &target.pos})
		a.cloningBeam = beam
		a.world().nodeRunner.AddObject(beam)
	}
	a.dist -= delta
	if a.pos.DistanceSquaredTo(target.pos) > (10 * 10) {
		a.pos = a.pos.MoveTowards(target.pos, delta*12)
	} else {
		// Merging is x3 faster when units are next to each other.
		a.dist -= delta * 2
		if a.mode == agentModeMergingRoomba {
			if a.height > 5 {
				descent := gmath.ClampMax(20*delta, a.height-5)
				a.pos.Y += descent
				a.height -= descent
			}
		}
	}
	if a.dist <= 0 {
		a.cloningBeam.Dispose()
		a.cloningBeam = nil
		if a.mode == agentModeMergingRoomba && !posIsFreeWithFlags(a.world(), nil, a.pos, 2, collisionSkipSmallCrawlers|collisionSkipTeleporters) {
			a.AssignMode(agentModeAlignStandby, gmath.Vec{}, nil)
			target.AssignMode(agentModeAlignStandby, gmath.Vec{}, nil)
			return
		}

		newStats := mergeAgents(a.world(), a, target)
		if newStats == nil {
			panic(fmt.Sprintf("empty merge result for %s %s + %s %s", a.faction, a.stats.Kind, target.faction, target.stats.Kind))
		}
		var newAgent *colonyAgentNode
		if a.mode == agentModeMergingRoomba {
			newAgent = newColonyAgentNode(a.colonyCore, newStats, target.pos)
			a.colonyCore.AcceptRoomba(newAgent)
		} else {
			newAgent = a.colonyCore.NewColonyAgentNode(newStats, target.pos)
		}
		var newFaction gamedata.FactionTag
		rankScore := a.rank + target.rank
		switch rankScore {
		case 0:
			// Two normal units => normal unit.
		case 1:
			// Only one elite unit => a chance to get an elite.
			if a.scene.Rand().Chance(0.75) {
				newAgent.rank = 1
			}
		case 2:
			// Only one super elite unit or two elite units => a super elite or normal elite.
			if a.scene.Rand().Chance(0.75) {
				newAgent.rank = 2
			} else {
				newAgent.rank = 1
			}
		default:
			// Anything better is capped at rank 2.
			newAgent.rank = 2
		}
		if newStats.Tier == 2 {
			newFaction = a.colonyCore.pickAgentFaction()
		} else {
			newFaction = a.faction
			if newFaction == gamedata.NeutralFactionTag || (target.faction != gamedata.NeutralFactionTag && a.faction != target.faction && a.scene.Rand().Bool()) {
				newFaction = target.faction
			}
		}
		newAgent.faction = newFaction
		a.world().nodeRunner.AddObject(newAgent)
		if newAgent.stats == gamedata.RoombaAgentStats {
			newAgent.mode = agentModeRoombaWait
			newAgent.dist = 1
		} else {
			newAgent.AssignMode(agentModeStandby, gmath.Vec{}, nil)
		}
		target.Destroy()
		a.Destroy()
		effect := newEffectNode(a.world().camera, newAgent.pos, newAgent.stats != gamedata.RoombaAgentStats, assets.ImageMergingComplete)
		effect.rotates = true
		a.world().nodeRunner.AddObject(effect)
		return
	}
}

func (a *colonyAgentNode) updateMakeClone(delta float64) {
	target := a.target.(*colonyAgentNode)
	if target.IsDisposed() {
		if a.cloningBeam != nil {
			a.cloningBeam.Dispose()
		}
		a.AssignMode(agentModeStandby, gmath.Vec{}, nil)
		return
	}
	if !a.waypoint.IsZero() {
		if a.moveTowards(delta, a.waypoint) {
			a.waypoint = gmath.Vec{}
			beam := newCloningBeamNode(a.world(), false, &a.pos, ge.Pos{Base: &target.pos})
			a.cloningBeam = beam
			a.world().nodeRunner.AddObject(beam)
			return
		}
		return
	}
	a.dist -= delta
	if a.dist <= 0 {
		a.cloningBeam.Dispose()
		a.cloningBeam = nil
		a.AssignMode(agentModeStandby, gmath.Vec{}, nil)
		target.AssignMode(agentModeStandby, gmath.Vec{}, nil)
		clone := a.colonyCore.CloneAgentNode(target)
		a.world().nodeRunner.AddObject(clone)
		clone.AssignMode(agentModeStandby, gmath.Vec{}, nil)
		a.world().result.DronesCloned++
		return
	}
}

func (a *colonyAgentNode) getCloserWaypoint(targetPos gmath.Vec, spread, preferredDist float64) gmath.Vec {
	currentDist := a.pos.DistanceTo(targetPos)
	if currentDist <= preferredDist {
		return targetPos.Add(a.scene.Rand().Offset(-spread, spread))
	}
	const maxMoveDist float64 = 96.0
	dist := gmath.ClampMax(maxMoveDist*a.scene.Rand().FloatRange(0.8, 1.2), gmath.ClampMin(currentDist-preferredDist, maxMoveDist*0.25))
	result := targetPos.DirectionTo(a.pos).Mulf(dist).Add(a.pos).Add(a.scene.Rand().Offset(-28, 28))
	return result
}

func (a *colonyAgentNode) followWaypoint(targetPos gmath.Vec) gmath.Vec {
	rng := a.stats.Weapon.AttackRange * 0.8
	preferredDist := gmath.ClampMin(rng, 80)
	return a.getCloserWaypoint(targetPos, rng, preferredDist)
}

func (a *colonyAgentNode) updateMove(delta float64) {
	if a.moveTowards(delta, a.waypoint) {
		a.AssignMode(agentModeStandby, gmath.Vec{}, nil)
	}
}

func (a *colonyAgentNode) updatePanic(delta float64) {
	if a.moveTowards(delta, a.waypoint) {
		a.waypointsLeft--
		a.waypoint = gmath.Vec{}
	}

	if a.waypoint.IsZero() {
		if a.waypointsLeft <= 0 {
			a.AssignMode(agentModeStandby, gmath.Vec{}, nil)
			return
		}
		waypoint := a.pos.Add(a.scene.Rand().Offset(-32, 32))
		a.waypoint = correctedPos(a.world().rect, waypoint, 64)
	}
}

func (a *colonyAgentNode) updateCourierFlight(delta float64) {
	if a.moveTowards(delta, a.waypoint) {
		target := a.target.(*colonyCoreNode)
		if target.IsDisposed() || target.mode != colonyModeNormal {
			if a.payload != 0 {
				// Has some payload, should return it back.
				a.AssignMode(agentModeReturn, gmath.Vec{}, nil)
			} else {
				a.AssignMode(agentModeStandby, gmath.Vec{}, nil)
			}
			return
		}
		if target.pos.DistanceSquaredTo(a.pos) < (70 * 70) {
			if a.payload != 0 {
				target.resources += a.cargoValue
				a.clearCargo()
			}
			beam := newBeamNode(a.world(), ge.Pos{Base: &a.pos}, ge.Pos{Base: &target.pos}, courierResourceBeamColor)
			beam.width = 2
			a.world().nodeRunner.AddObject(beam)
			playSound(a.world(), assets.AudioCourierResourceBeam, a.pos)
			dist := target.pos.DistanceTo(a.colonyCore.pos)
			// Now go back and bring some resources.
			if dist > 115 {
				a.payload = a.maxPayload()
				a.cargoValue = float64(a.payload) * (math.Trunc((dist-100)/15) * 0.08)
				a.cargoValue = gmath.ClampMax(a.cargoValue, 10)
				a.AssignMode(agentModeReturn, gmath.Vec{}, nil)
			} else {
				a.AssignMode(agentModeStandby, gmath.Vec{}, nil)
			}
			// Get a minor repair+recharge.
			a.health = gmath.ClampMax(a.health+5, a.maxHealth)
			a.energy = gmath.ClampMax(a.energy+15, a.maxEnergy)
			return
		}
		// TODO: use a followPos here?
		a.waypoint = a.pos.DirectionTo(target.pos).Mulf(60).Add(target.pos).Add(a.scene.Rand().Offset(-20, 20))
	}
}

func (a *colonyAgentNode) updateFollow(delta float64) {
	if a.moveTowards(delta, a.waypoint) {
		target := a.target.(*creepNode)
		if a.waypointsLeft == 0 || target.IsDisposed() {
			a.AssignMode(agentModeStandby, gmath.Vec{}, nil)
			return
		}
		a.waypointsLeft--
		a.waypoint = a.followWaypoint(target.pos)
	}
}

func (a *colonyAgentNode) updateAlignStandby(delta float64) {
	speed := a.movementSpeed()
	a.height += delta * speed
	if a.moveTowardsWithSpeed(delta, speed, a.waypoint) {
		a.height = agentFlightHeight
		a.AssignMode(agentModeStandby, gmath.Vec{}, nil)
	}
}

func (a *colonyAgentNode) updateStandby(delta float64) {
	if a.healthRegen != 0 {
		a.health = gmath.ClampMax(a.health+(delta*a.healthRegen), a.maxHealth)
	}

	a.energy = gmath.ClampMax(a.energy+delta*0.5*a.energyRegenRate, a.maxEnergy)
	if a.moveTowards(delta, a.waypoint) {
		if a.stats.Tier == 1 && a.lifetime < 0 && a.colonyCore.mode == colonyModeNormal {
			a.AssignMode(agentModeRecycleReturn, gmath.Vec{}, nil)
			return
		}
		a.waypoint = a.orbitingWaypoint()
		if a.hasTrait(traitAdventurer) {
			a.waypointsLeft++
			if a.waypointsLeft > 10 {
				a.waypointsLeft = 0
				traitRoll := a.scene.Rand().Float()
				if a.hasTrait(traitAdventurer) && traitRoll <= 0.4 {
					pos := a.pos.Add(a.scene.Rand().Offset(-100, 100))
					a.AssignMode(agentModeMove, pos, nil)
					// Add some energy to compensate for this unproductive behavior.
					a.energy = gmath.ClampMax(a.energy+5, a.maxEnergy)
					a.energyBill = gmath.ClampMin(a.energyBill-5, 0)
					return
				}
			}
		}
		if a.colonyCore.mode == colonyModeNormal && !a.hasTrait(traitNeverStop) && a.energy < 40 && a.scene.Rand().Chance(0.2) {
			a.AssignMode(agentModeCharging, gmath.Vec{}, nil)
			return
		}
	}
}

func (a *colonyAgentNode) updateCloakHide(delta float64) {
	a.energy = gmath.ClampMax(a.energy+delta*a.energyRegenRate, a.maxEnergy)
	if a.cloaking <= 0 {
		a.health = gmath.ClampMax(a.health+2, a.maxHealth)
		a.AssignMode(agentModeStandby, gmath.Vec{}, nil)
	}
}

func (a *colonyAgentNode) updateCharging(delta float64) {
	a.energy = gmath.ClampMax(a.energy+delta*4*a.energyRegenRate, a.maxEnergy)
	if a.energy >= a.maxEnergy*0.55 {
		a.energyBill = 0
		a.AssignMode(agentModeStandby, gmath.Vec{}, nil)
	}
}

func (a *colonyAgentNode) updatePosing(delta float64) {
	a.dist -= delta
	if a.dist <= 0 {
		a.dist = 0
		a.AssignMode(agentModeStandby, gmath.Vec{}, nil)
	}
}

func (a *colonyAgentNode) updateForcedCharging(delta float64) {
	a.energy = gmath.ClampMax(a.energy+delta*2.0*a.energyRegenRate, a.maxEnergy)
	a.specialDelay -= delta
	if a.specialDelay <= 0 {
		a.specialDelay = 0
		a.AssignMode(agentModeStandby, gmath.Vec{}, nil)
	}
}

func (a *colonyAgentNode) updateMineEssence(delta float64) {
	if a.moveTowards(delta, a.waypoint) {
		source := a.target.(*essenceSourceNode)
		if source.IsDisposed() {
			if a.IsCloaked() {
				a.doUncloak()
			}
			a.AssignMode(agentModeStandby, gmath.Vec{}, nil)
		} else {
			a.AssignMode(agentModePickup, gmath.Vec{}, nil)
		}
	}
}

func (a *colonyAgentNode) updatePickup(delta float64) {
	speed := a.movementSpeed()
	a.height -= delta * speed
	if a.moveTowardsWithSpeed(delta, speed, a.waypoint) {
		a.height = 0
		a.mode = agentModeResourceTakeoff
		a.waypoint = a.pos.Sub(gmath.Vec{Y: agentFlightHeight})
		source := a.target.(*essenceSourceNode)
		harvested := source.Harvest(a.maxPayload())
		a.payload = harvested
		a.cargoValue = float64(harvested) * source.stats.value
		a.cargoEliteValue = float64(harvested) * source.stats.eliteValue
	}
}

func (a *colonyAgentNode) updateResourceTakeoff(delta float64) {
	speed := a.movementSpeed()
	a.height += delta * speed
	if a.moveTowardsWithSpeed(delta, speed, a.waypoint) {
		a.height = agentFlightHeight
		a.AssignMode(agentModeReturn, gmath.Vec{}, nil)
	}
}

func (a *colonyAgentNode) clearCargo() {
	a.payload = 0
	a.cargoValue = 0
	a.cargoEliteValue = 0
}

func (a *colonyAgentNode) updateReturn(delta float64) {
	if a.moveTowards(delta, a.waypoint) {
		if a.IsCloaked() {
			a.doUncloak()
		}
		if a.payload != 0 {
			a.colonyCore.resources += a.cargoValue
			a.world().result.ResourcesGathered += a.cargoValue
			a.colonyCore.eliteResources += a.cargoEliteValue
			a.world().result.EliteResourcesGathered = a.cargoEliteValue
			a.clearCargo()
			playSound(a.world(), assets.AudioEssenceCollected, a.pos)
		}
		a.AssignMode(agentModeStandby, gmath.Vec{}, nil)
	}
}

func (a *colonyAgentNode) camera() *viewport.Camera {
	return a.world().camera
}

func (a *colonyAgentNode) world() *worldState {
	return a.colonyCore.world
}

func (a *colonyAgentNode) hasTrait(t agentTraitBits) bool {
	return a.traits&t != 0
}

func (a *colonyAgentNode) maxPayload() int {
	n := a.stats.MaxPayload
	if a.faction == gamedata.YellowFactionTag {
		n += 2
	}
	return n
}
