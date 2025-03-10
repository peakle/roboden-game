package staging

import (
	"image"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	resource "github.com/quasilyte/ebitengine-resource"
	"github.com/quasilyte/ge"
	"github.com/quasilyte/gmath"
	"github.com/quasilyte/roboden-game/assets"
	"github.com/quasilyte/roboden-game/gamedata"
	"github.com/quasilyte/roboden-game/pathing"
)

func resizedRect(rect gmath.Rect, delta float64) gmath.Rect {
	delta = -delta // Negative value should make the rect smaller
	offset := gmath.Vec{X: delta, Y: delta}
	return gmath.Rect{
		Min: rect.Min.Add(offset),
		Max: rect.Max.Sub(offset),
	}
}

type tinyCoord struct {
	data uint8
}

func makeTinyCoord(x, y int) tinyCoord {
	xsign := uint8(x & (1 << 7))
	xvalue := uint8(x&1) << 6
	ysign := uint8(y&(1<<7)) >> 2
	yvalue := uint8(y&1) << 4
	data := (xsign | xvalue | ysign | yvalue)
	return tinyCoord{data: data}
}

func (c tinyCoord) X() int8 {
	return int8(c.data&0b11000000) >> 6
}

func (c tinyCoord) Y() int8 {
	return int8((c.data<<2)&0b11000000) >> 6
}

func (c tinyCoord) IsZero() bool { return c.data == 0 }

var diagonalMoveTab = [32]tinyCoord{
	pathing.DirRight | (pathing.DirUp << 2):   makeTinyCoord(1, -1),
	pathing.DirRight | (pathing.DirDown << 2): makeTinyCoord(1, 1),
	pathing.DirDown | (pathing.DirRight << 2): makeTinyCoord(1, 1),
	pathing.DirDown | (pathing.DirLeft << 2):  makeTinyCoord(-1, 1),
	pathing.DirLeft | (pathing.DirDown << 2):  makeTinyCoord(-1, 1),
	pathing.DirLeft | (pathing.DirUp << 2):    makeTinyCoord(-1, -1),
	pathing.DirUp | (pathing.DirLeft << 2):    makeTinyCoord(-1, -1),
	pathing.DirUp | (pathing.DirRight << 2):   makeTinyCoord(1, -1),
}

func nextPathWaypoint(world *worldState, pos gmath.Vec, p *pathing.GridPath, l pathing.GridLayer) gmath.Vec {
	cell := world.pathgrid.PosToCoord(pos)
	d1, d2 := p.Peek2()
	diagOffset := diagonalMoveTab[(d1|(d2<<2))&0b11111]
	if !diagOffset.IsZero() {
		// Can make a diagonal move, if second cell is free too.
		otherCell := cell.Move(d2)
		if world.CellIsFree(otherCell, l) {
			p.Skip(2)
			return world.pathgrid.CoordToPos(cell.Move(d1).Move(d2))
		}
	}
	p.Skip(1)
	return world.pathgrid.CoordToPos(cell.Move(d1))
}

func isValidCreepTarget(pos gmath.Vec, creep *creepNode, weapon *gamedata.WeaponStats) bool {
	if !creep.CanBeTargeted() {
		return false
	}
	if weapon.TargetFlags&creep.TargetKind() == 0 {
		return false
	}
	attackRangeSqr := weapon.AttackRangeSqr
	if creep.marked > 0 {
		attackRangeSqr *= weapon.AttackRangeMarkMultiplier
	}
	if creep.pos.DistanceSquaredTo(pos) > attackRangeSqr {
		return false
	}
	return true
}

func attackWithProjectile(world *worldState, weapon *gamedata.WeaponStats, attacker, target targetable, burstSize int, guided bool) {
	toPos := snipePos(weapon.ProjectileSpeed, *attacker.GetPos(), *target.GetPos(), target.GetVelocity())
	j := 0
	attacksPerBurst := weapon.AttacksPerBurst
	seq := uint8(0)
	for i := 0; i < burstSize; i += attacksPerBurst {
		if i+attacksPerBurst > burstSize {
			// This happens only once for the last burst wave
			// if attacks-per-burst are not aligned with burstSize (like with Devourer).
			attacksPerBurst = burstSize - i
		}
		for i := 0; i < attacksPerBurst; i++ {
			fireDelay := float64(j) * weapon.BurstDelay
			p := world.newProjectileNode(projectileConfig{
				Seq:       seq,
				World:     world,
				Weapon:    weapon,
				Attacker:  attacker,
				ToPos:     toPos,
				Target:    target,
				FireDelay: fireDelay,
				Guided:    guided,
			})
			world.nodeRunner.AddProjectile(p)
			seq++
		}
		j++
	}
}

type forestCheckResult int

const (
	forestStateUnchanged forestCheckResult = iota
	forestStateEnter
	forestStateLeave
)

func checkForestState(world *worldState, insideForest bool, currentPos, nextWaypoint gmath.Vec) forestCheckResult {
	if insideForest {
		if !world.HasTreesAt(currentPos, 0) && !world.HasTreesAt(nextWaypoint, 0) {
			return forestStateLeave
		}
	} else {
		if world.HasTreesAt(nextWaypoint, 0) {
			return forestStateEnter
		}
	}
	return forestStateUnchanged
}

func createSubImage(img resource.Image, offsetX int) *ebiten.Image {
	_, height := img.Data.Size()
	min := image.Point{
		X: offsetX,
		Y: 0,
	}
	return img.Data.SubImage(image.Rectangle{
		Min: min,
		Max: image.Point{X: min.X + int(img.DefaultFrameWidth), Y: height},
	}).(*ebiten.Image)
}

func pointToLineDistance(point, a, b gmath.Vec) float64 {
	s1 := -b.Y + a.Y
	s2 := b.X - a.X
	return math.Abs((point.X-a.X)*s1+(point.Y-a.Y)*s2) / math.Sqrt(s1*s1+s2*s2)
}

func midpoint(a, b gmath.Vec) gmath.Vec {
	return a.Add(b).Mulf(0.5)
}

func sideName(side int) string {
	switch side {
	case 0:
		return "game.side.east"
	case 1:
		return "game.side.south"
	case 2:
		return "game.side.west"
	default:
		return "game.side.north"
	}
}

func moveTowardsWithSpeed(from, to gmath.Vec, delta, speed float64) (gmath.Vec, bool) {
	travelled := speed * delta
	result := from.MoveTowards(to, travelled)
	return result, result == to
}

func randIterate[T any](rand *gmath.Rand, slice []T, f func(x T) bool) T {
	var result T
	if len(slice) == 0 {
		return result
	}
	if len(slice) == 1 {
		// Don't use rand() if there is only 1 element.
		x := slice[0]
		if f(x) {
			result = x
		}
		return result
	}

	var slider gmath.Slider
	slider.SetBounds(0, len(slice)-1)
	slider.TrySetValue(rand.IntRange(0, len(slice)-1))
	inc := rand.Bool()
	for i := 0; i < len(slice); i++ {
		x := slice[slider.Value()]
		if inc {
			slider.Inc()
		} else {
			slider.Dec()
		}
		if f(x) {
			result = x
			break
		}
	}
	return result
}

func randomSectorPos(rng *gmath.Rand, sector gmath.Rect) gmath.Vec {
	return gmath.Vec{
		X: rng.FloatRange(sector.Min.X, sector.Max.X),
		Y: rng.FloatRange(sector.Min.Y, sector.Max.Y),
	}
}

func posMove(pos gmath.Vec, d pathing.Direction) gmath.Vec {
	switch d {
	case pathing.DirRight:
		return pos.Add(gmath.Vec{X: pathing.CellSize})
	case pathing.DirDown:
		return pos.Add(gmath.Vec{Y: pathing.CellSize})
	case pathing.DirLeft:
		return pos.Add(gmath.Vec{X: -pathing.CellSize})
	case pathing.DirUp:
		return pos.Add(gmath.Vec{Y: -pathing.CellSize})
	default:
		return pos
	}
}

func orbitingWaypoint(world *worldState, pos, center gmath.Vec, dist float64, clockwise bool) gmath.Vec {
	var direction gmath.Vec
	if pos == center {
		direction = gmath.RadToVec(world.rand.Rad())
	} else {
		direction = pos.DirectionTo(center)
	}
	angle := gmath.Rad(0.4)
	if !clockwise {
		angle = -0.4
	}
	return direction.Rotated(angle).Mulf(dist).Add(center)
}

type collisionFlags int

const (
	collisionSkipSmallCrawlers collisionFlags = 1 << iota
	collisionSkipTeleporters
	collisionSkipForest
)

func posIsFree(world *worldState, skipColony *colonyCoreNode, pos gmath.Vec, radius float64) bool {
	return posIsFreeWithFlags(world, skipColony, pos, radius, 0)
}

func posIsFreeWithFlags(world *worldState, skipColony *colonyCoreNode, pos gmath.Vec, radius float64, flags collisionFlags) bool {
	// TODO: why we're not using a pathgrid here?

	wallCheckRadius := radius + 24
	for _, wall := range world.walls {
		if wall.CollidesWith(pos, wallCheckRadius) {
			return false
		}
	}

	radiusSqr := radius * radius

	if world.envKind == gamedata.EnvInferno {
		for _, g := range world.lavaGeysers {
			if g.pos.DistanceSquaredTo(pos) < (radiusSqr + (40 * 40)) {
				return false
			}
		}
		for _, p := range world.lavaPuddles {
			if p.CollidesWith(pos, radius) {
				return false
			}
		}
	}

	for _, b := range world.neutralBuildings {
		if b.pos.DistanceSquaredTo(pos) < (radiusSqr + (40 * 40)) {
			return false
		}
	}
	for _, source := range world.essenceSources {
		if source.pos.DistanceTo(pos) < (radius + source.stats.size) {
			return false
		}
	}
	for _, construction := range world.constructions {
		if construction.pos.DistanceTo(pos) < (radius + 40) {
			return false
		}
	}
	for _, turret := range world.turrets {
		if turret.pos.DistanceTo(pos) < (radius + 32) {
			return false
		}
	}
	for _, colony := range world.allColonies {
		// TODO: flying colonies are not a problem?
		if colony == skipColony {
			continue
		}
		if colony.pos.DistanceTo(pos) < (radius + 40) {
			return false
		}
	}

	skipSmall := flags&collisionSkipSmallCrawlers != 0
	for _, creep := range world.creeps {
		if skipSmall && creep.stats.Kind == gamedata.CreepCrawler {
			continue
		}
		if creep.stats.ShadowImage == assets.ImageNone && creep.pos.DistanceTo(pos) < (radius+creep.stats.Size) {
			return false
		}
	}

	if flags&collisionSkipTeleporters == 0 {
		for _, tp := range world.teleporters {
			if tp.pos.DistanceTo(pos) < (radius + 54) {
				return false
			}
		}
	}

	if flags&collisionSkipForest == 0 {
		for _, f := range world.forests {
			if f.CollidesWith(pos, radius+22) {
				return false
			}
		}
	}

	return true
}

type effectLayer int

const (
	normalEffectLayer effectLayer = iota
	slightlyAboveEffectLayer
	aboveEffectLayer
	customEffectLayer
)

func effectLayerFromBool(above bool) effectLayer {
	if above {
		return aboveEffectLayer
	}
	return normalEffectLayer
}

type effectConfig struct {
	Pos            gmath.Vec
	Rotation       gmath.Rad
	Image          resource.ImageID
	AnimationSpeed animationSpeed
	Layer          effectLayer
	Reverse        bool
	Rotates        bool
}

type animationSpeed int

const (
	animationSpeedNormal animationSpeed = iota
	animationSpeedFast
	animationSpeedSlow
	animationSpeedVerySlow
	animationSpeedSlowest
)

func (s animationSpeed) SecondsPerFrame() float64 {
	switch s {
	case animationSpeedFast:
		return 0.035
	case animationSpeedSlow:
		return 0.05
	case animationSpeedVerySlow:
		return 0.07
	case animationSpeedSlowest:
		return 0.08
	default:
		return 0.04
	}
}

func createEffect(world *worldState, config effectConfig) {
	if world.simulation {
		return
	}

	effect := newEffectNode(world, config.Pos, config.Layer, config.Image)
	effect.rotation = config.Rotation
	effect.rotates = config.Rotates
	world.nodeRunner.AddObject(effect)
	if config.AnimationSpeed != animationSpeedNormal {
		effect.anim.SetSecondsPerFrame(config.AnimationSpeed.SecondsPerFrame())
	}
	if config.Reverse {
		effect.anim.Mode = ge.AnimationBackward
	}
}

func createAreaExplosion(world *worldState, rect gmath.Rect, layer effectLayer) {
	if world.simulation {
		return
	}

	// FIXME: Rect.Center() does not work properly in gmath.
	center := gmath.Vec{
		X: rect.Max.X - rect.Width()*0.5,
		Y: rect.Max.Y - rect.Height()*0.5,
	}

	if world.cameraShakingEnabled {
		// TODO: use max() here.
		rectWidth := rect.Width()
		rectHeight := rect.Height()
		maxSideSize := rectWidth
		if rectHeight > rectWidth {
			maxSideSize = rectHeight
		}
		shakePower := 0
		if maxSideSize >= 32 {
			shakePower = 40 + (int(maxSideSize) - 32)
		}
		if shakePower != 0 {
			world.ShakeCamera(shakePower, center)
		}
	}

	size := rect.Width() * rect.Height()
	minExplosions := gmath.ClampMin(size/120.0, 1)
	numExplosions := world.localRand.IntRange(int(minExplosions), int(minExplosions*1.3))
	allowVertical := layer == normalEffectLayer
	for numExplosions > 0 {
		offset := gmath.Vec{
			X: world.localRand.FloatRange(-rect.Width()*0.4, rect.Width()*0.4),
			Y: world.localRand.FloatRange(-rect.Height()*0.4, rect.Height()*0.4),
		}
		if numExplosions >= 4 && world.localRand.Chance(0.4) {
			numExplosions -= 4
			world.nodeRunner.AddObject(newEffectNode(world, center.Add(offset), layer, assets.ImageBigExplosion))
		} else {
			numExplosions--
			if allowVertical && world.localRand.Chance(0.4) {
				img := assets.ImageVerticalExplosion1
				if world.localRand.Bool() {
					img = assets.ImageVerticalExplosion2
				}
				effect := newEffectNode(world, center.Add(offset), layer, img)
				world.nodeRunner.AddObject(effect)
				effect.anim.SetSecondsPerFrame(0.035)
			} else {
				createMuteExplosion(world, layer, center.Add(offset))
			}
		}
	}
	playSound(world, assets.AudioExplosion1, center)
}

func createMuteExplosion(world *worldState, layer effectLayer, pos gmath.Vec) {
	imageRoll := world.localRand.Float()
	img := assets.ImageSmallExplosion1 // 35%
	switch {
	case imageRoll <= 0.15: // 15%
		img = assets.ImageSmallExplosion4
	case imageRoll <= 0.35: // 20%
		img = assets.ImageSmallExplosion3
	case imageRoll <= 0.6: // 30%
		img = assets.ImageSmallExplosion2
	}
	explosion := newEffectNode(world, pos, layer, img)
	world.nodeRunner.AddObject(explosion)
}

func playIonExplosionSound(world *worldState, pos gmath.Vec) {
	explosionSoundIndex := world.localRand.IntRange(0, 1)
	explosionSound := resource.AudioID(int(assets.AudioIonZap1) + explosionSoundIndex)
	playSound(world, explosionSound, pos)
}

func createBigVerticalExplosion(world *worldState, pos gmath.Vec, layer effectLayer) {
	if world.simulation {
		return
	}

	img := assets.ImageBigVerticalExplosion1
	if world.localRand.Bool() {
		img = assets.ImageBigVerticalExplosion2
	}
	world.nodeRunner.AddObject(newEffectNode(world, pos, layer, img))
	playSound(world, assets.AudioExplosion1, pos)
}

func createExplosion(world *worldState, layer effectLayer, pos gmath.Vec) {
	if world.simulation {
		return
	}

	createMuteExplosion(world, layer, pos)
	playSound(world, assets.AudioExplosion1, pos)
}

func spriteRect(pos gmath.Vec, sprite *ge.Sprite) gmath.Rect {
	offset := gmath.Vec{X: sprite.FrameWidth * 0.5, Y: sprite.FrameHeight * 0.5}
	return gmath.Rect{
		Min: pos.Sub(offset),
		Max: pos.Add(offset),
	}
}

func roundedPos(pos gmath.Vec) gmath.Vec {
	return gmath.Vec{
		X: math.Round(pos.X),
		Y: math.Round(pos.Y),
	}
}

func correctedPos(sector gmath.Rect, pos gmath.Vec, pad float64) gmath.Vec {
	if pos.X < (pad + sector.Min.X) {
		pos.X = pad + sector.Min.X
	} else if pos.X > (sector.Max.X - pad) {
		pos.X = sector.Max.X - pad
	}
	if pos.Y < (pad + sector.Min.Y) {
		pos.Y = pad + sector.Min.Y
	} else if pos.Y > (sector.Max.Y - pad) {
		pos.Y = sector.Max.Y - pad
	}
	return pos
}

func snipePos(projectileSpeed float64, fireFrom, targetPos, targetVelocity gmath.Vec) gmath.Vec {
	if targetVelocity.IsZero() || projectileSpeed == 0 {
		return targetPos
	}
	dist := targetPos.DistanceTo(fireFrom)
	predictedPos := targetPos.Add(targetVelocity.Mulf(dist / projectileSpeed))
	return predictedPos
}

func retreatPos(rand *gmath.Rand, dist float64, objectPos, threatPos gmath.Vec) gmath.Vec {
	direction := threatPos.AngleToPoint(objectPos) + gmath.Rad(rand.FloatRange(-0.2, 0.2))
	return objectPos.MoveInDirection(dist, direction)
}

func playSound(world *worldState, id resource.AudioID, pos gmath.Vec) {
	if world.simulation {
		return
	}
	for _, cam := range world.cameras {
		if cam.ContainsPos(pos) {
			numSamples := assets.NumAudioSamples(id)
			if numSamples != 1 {
				id = id + resource.AudioID(world.localRand.IntRange(0, numSamples-1))
			}
			world.rootScene.Audio().PlaySound(id)
			return
		}
	}
}

type pendingImage struct {
	data      *ebiten.Image
	options   ebiten.DrawImageOptions
	drawOrder float64
}
