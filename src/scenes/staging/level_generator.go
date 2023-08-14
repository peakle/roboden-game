package staging

import (
	"fmt"
	"math"
	"sort"

	"github.com/quasilyte/ge"
	"github.com/quasilyte/ge/xslices"
	"github.com/quasilyte/gmath"
	"github.com/quasilyte/roboden-game/gamedata"
	"github.com/quasilyte/roboden-game/pathing"
)

const (
	maxWallSegments int     = 16
	wallTileSize    float64 = 32
)

type levelGenerator struct {
	scene        *ge.Scene
	rng          gmath.Rand
	world        *worldState
	playerSpawn  gmath.Vec
	sectors      []gmath.Rect
	sectorSlider gmath.Slider
	bg           *ge.TiledBackground

	pendingResources []*essenceSourceNode
}

type creepPlacingConfig struct {
	CreepInit func(creep *creepNode)
	Pad       float64
	NoScraps  bool
}

func newLevelGenerator(scene *ge.Scene, bg *ge.TiledBackground, world *worldState) *levelGenerator {
	g := &levelGenerator{
		scene: scene,
		world: world,
		bg:    bg,
	}
	g.rng.SetSeed(world.config.Seed)
	g.sectors = []gmath.Rect{
		{Min: gmath.Vec{X: 0, Y: 0}, Max: gmath.Vec{X: g.world.width / 2, Y: g.world.height / 2}},
		{Min: gmath.Vec{X: g.world.width / 2, Y: 0}, Max: gmath.Vec{X: g.world.width, Y: g.world.height / 2}},
		{Min: gmath.Vec{X: 0, Y: g.world.height / 2}, Max: gmath.Vec{X: g.world.width / 2, Y: g.world.height}},
		{Min: gmath.Vec{X: g.world.width / 2, Y: g.world.height / 2}, Max: gmath.Vec{X: g.world.width, Y: g.world.height}},
	}
	g.sectorSlider.SetBounds(0, len(g.sectors)-1)
	return g
}

func (g *levelGenerator) Generate() {
	g.playerSpawn = g.world.rect.Center()
	resourceMultipliers := []float64{
		0.35,
		0.7,
		1, // Default
		1.25,
		1.6,
	}
	g.placeForests()
	g.placeTeleporters()
	g.placeRelicts()
	g.placePlayers()
	g.placeWalls()
	g.placeCreepBases()
	g.placeCreeps()
	g.placeResources(resourceMultipliers[g.world.config.Resources])
	g.placeBoss()

	g.fillPathgrid()
}

func (g *levelGenerator) randomFreePosWithFallback(sector, fallback gmath.Rect, radius, pad float64) (gmath.Vec, gmath.Rect) {
	pos := g.randomFreePos(sector, radius, pad)
	selectedSector := sector
	if pos.IsZero() {
		pos = g.randomFreePos(fallback, radius, pad)
		selectedSector = fallback
	}
	return pos, selectedSector
}

func (g *levelGenerator) randomFreePos(sector gmath.Rect, radius, pad float64) gmath.Vec {
	tries := 0
	for {
		pos := g.randomPos(sector)
		if pad != 0 {
			pos = correctedPos(sector, pos, pad)
		}
		if posIsFree(g.world, nil, pos, radius) {
			return pos
		}
		tries++
		if tries > 32 {
			break
		}
	}

	// Try a linear search starting from a random offset.
	probePos := g.randomPos(sector)
	pos := probePos
	for pos.Y+pad < sector.Max.Y {
		for pos.X+pad < sector.Max.X {
			if posIsFree(g.world, nil, pos, radius) {
				return pos
			}
			pos.X += radius
		}
		pos.Y += radius
	}
	// Now try moving backwards.
	pos = probePos.Sub(gmath.Vec{X: radius, Y: radius})
	for pos.Y-pad > sector.Min.Y {
		for pos.X-pad > sector.Min.X {
			if posIsFree(g.world, nil, pos, radius) {
				return pos
			}
			pos.X -= radius
		}
		pos.Y -= radius
	}

	return gmath.Vec{}
}

func (g *levelGenerator) randomPos(sector gmath.Rect) gmath.Vec {
	return randomSectorPos(&g.rng, sector)
}

func (g *levelGenerator) fillPathgridRect(rect gmath.Rect, tag uint8) {
	for y := rect.Min.Y; y < rect.Max.Y; y += wallTileSize {
		for x := rect.Min.X; x < rect.Max.X; x += wallTileSize {
			pos := gmath.Vec{X: x, Y: y}
			g.world.MarkPos(pos, tag)
		}
	}
}

func (g *levelGenerator) fillPathgrid() {
	w := g.world

	if pathing.CellSize != wallTileSize {
		panic("update the pathgrid build algorithm")
	}

	// Traverse all relevant world objects and mark the occupied cells.

	// We're using a few assumptions here:
	// 1. Wall tiles are always grid-aligned.
	// 2. Wall tiles have the same grid size as path grid cells.
	for _, wall := range g.world.walls {
		if wall.rectShape {
			g.fillPathgridRect(wall.rect, ptagBlocked)
			continue
		}
		for _, pos := range wall.points {
			w.MarkPos(pos, ptagBlocked)
		}
	}
}

func (g *levelGenerator) nextSector(sectorIndex int, sectors []gmath.Rect) gmath.Rect {
	if sectorIndex+1 >= len(sectors) {
		return sectors[0]
	}
	return sectors[sectorIndex+1]
}

func (g *levelGenerator) placeTeleporters() {
	for i := 0; i < g.world.config.Teleporters; i++ {
		tp1sectorIndex := gmath.RandIndex(g.world.rand, g.sectors)
		tp1pos, tp1sector := g.randomFreePosWithFallback(g.sectors[tp1sectorIndex], g.nextSector(tp1sectorIndex, g.sectors), 96, 196)
		tp1 := &teleporterNode{id: i, pos: g.world.Adjust2x2CellPos(tp1pos, 0).Sub(teleportOffset), world: g.world}

		var tp2 *teleporterNode
		for {
			tp2sectorIndex := gmath.RandIndex(g.world.rand, g.sectors)
			tp2sector := g.sectors[tp2sectorIndex]
			if tp2sector == tp1sector {
				continue
			}
			tp2pos, _ := g.randomFreePosWithFallback(tp2sector, g.nextSector(tp2sectorIndex, g.sectors), 96, 196)
			tp2 = &teleporterNode{id: i, pos: g.world.Adjust2x2CellPos(tp2pos, 0).Sub(teleportOffset), world: g.world}
			break
		}

		tp1.other = tp2
		tp2.other = tp1

		g.world.teleporters = append(g.world.teleporters, tp1)
		g.world.nodeRunner.AddObject(tp1)
		g.world.teleporters = append(g.world.teleporters, tp2)
		g.world.nodeRunner.AddObject(tp2)
	}
}

func (g *levelGenerator) placeRelicts() {
	if !g.world.config.Relicts {
		return
	}

	artifactsPool := make([]*gamedata.AgentStats, len(gamedata.ArtifactsList))
	copy(artifactsPool, gamedata.ArtifactsList)
	gmath.Shuffle(g.world.rand, artifactsPool)

	numArtifacts := 2
	if g.world.config.WorldSize == 3 {
		numArtifacts++
	}
	artifacts := artifactsPool[:numArtifacts]

	for _, a := range artifacts {
		g.sectorSlider.TrySetValue(g.world.rand.IntRange(0, len(g.sectors)-1))
		for attempt := 0; attempt < 10; attempt++ {
			sector := g.sectors[g.sectorSlider.Value()]
			g.sectorSlider.Dec()
			pos := g.randomFreePos(sector, 48, 168)
			if pos.IsZero() {
				continue
			}
			pos = g.world.pathgrid.AlignPos(pos)
			b := newNeutralBuildingNode(g.world, a, pos)
			b.Init(g.scene)
			g.world.neutralBuildings = append(g.world.neutralBuildings, b)
			break
		}
	}
}

func (g *levelGenerator) placePlayers() {
	if g.world.config.GameMode == gamedata.ModeReverse {
		// In reverse mode, there is always only 1 player controlling the colony.
		g.createBase(g.world.players[1], g.playerSpawn, true)
		return
	}

	switch len(g.world.config.Players) {
	case 1:
		g.createBase(g.world.players[0], g.playerSpawn, true)
	case 2:
		playerOffset := gmath.Vec{X: 64, Y: 64}
		g.createBase(g.world.players[0], g.playerSpawn.Sub(playerOffset), true)
		g.createBase(g.world.players[1], g.playerSpawn.Add(playerOffset), true)
	default:
		panic(fmt.Sprintf("invalid number of players: %d", len(g.world.config.Players)))
	}
}

func (g *levelGenerator) createBase(p player, pos gmath.Vec, mainBase bool) {
	core := g.world.NewColonyCoreNode(colonyConfig{
		World:  g.world,
		Radius: 128,
		Pos:    pos,
		Player: p,
	})
	core.priorities.SetWeight(priorityResources, 0.5)
	core.priorities.SetWeight(priorityGrowth, 0.4)
	core.priorities.SetWeight(prioritySecurity, 0.1)
	g.world.nodeRunner.AddObject(core)

	switch g.world.config.StartingResources {
	case 1:
		core.resources = core.maxVisualResources() / 3
	case 2:
		core.resources = core.maxVisualResources()
	}

	for i := 0; i < 5; i++ {
		a := core.NewColonyAgentNode(gamedata.WorkerAgentStats, core.pos.Add(g.rng.Offset(-20, 20)))
		g.world.nodeRunner.AddObject(a)
		a.AssignMode(agentModeStandby, gmath.Vec{}, nil)
	}
	if mainBase {
		for _, stats := range g.world.config.ExtraDrones {
			a := core.NewColonyAgentNode(stats, core.pos.Add(g.scene.Rand().Offset(-20, 20)))
			g.world.nodeRunner.AddObject(a)
			a.AssignMode(agentModeStandby, gmath.Vec{}, nil)
		}
	}
}

func (g *levelGenerator) placeCreep(sector gmath.Rect, stats *gamedata.CreepStats, config creepPlacingConfig) *creepNode {
	_, creep := g.placeCreepsClusterImpl(sector, 1, stats, config)
	return creep
}

func (g *levelGenerator) placeCreepsCluster(sector gmath.Rect, maxSize int, stats *gamedata.CreepStats, config creepPlacingConfig) int {
	numPlaced, _ := g.placeCreepsClusterImpl(sector, maxSize, stats, config)
	return numPlaced
}

func (g *levelGenerator) placeCreepsClusterImpl(sector gmath.Rect, maxSize int, stats *gamedata.CreepStats, config creepPlacingConfig) (int, *creepNode) {
	rand := &g.rng
	placed := 0
	var placedCreep *creepNode
	pos := correctedPos(sector, g.randomPos(sector), config.Pad)
	initialPos := pos
	unitPos := pos
	for i := 0; i < maxSize; i++ {
		if stats.Building {
			pos = g.world.AdjustCellPos(pos, 6)
		}
		if !posIsFree(g.world, nil, pos, 24) || pos.DistanceTo(g.playerSpawn) < 520 {
			break
		}
		creep := g.world.NewCreepNode(pos, stats)
		if config.CreepInit != nil {
			config.CreepInit(creep)
		}
		if stats.Kind == gamedata.CreepCrawler {
			creep.specialModifier = crawlerGuard
		}
		g.world.nodeRunner.AddObject(creep)
		unitPos = pos
		direction := gmath.RadToVec(rand.Rad()).Mulf(32)
		if rand.Bool() {
			pos = initialPos.Add(direction)
		} else {
			pos = pos.Add(direction)
		}
		placedCreep = creep
		placed++
	}
	// Creep groups may have some scraps near them.
	if !config.NoScraps && placed != 0 && rand.Chance(0.7) {
		numScraps := rand.IntRange(1, 2)
		for i := 0; i < numScraps; i++ {
			scrapPos := g.adjustResourcePos(gmath.RadToVec(rand.Rad()).Mulf(rand.FloatRange(64, 128)).Add(unitPos))
			if posIsFree(g.world, nil, scrapPos, 8) {
				g.world.CreateScrapsAt(scrapSource, scrapPos)
			}
		}
	}
	return placed, placedCreep
}

func (g *levelGenerator) adjustResourcePos(pos gmath.Vec) gmath.Vec {
	return g.world.AdjustCellPos(pos, 10)
}

func (g *levelGenerator) placeResourceCluster(sector gmath.Rect, maxSize int, kind *essenceSourceStats) int {
	rand := &g.rng
	placed := 0
	pos := correctedPos(sector, g.randomPos(sector), 96)
	initialPos := pos
	for i := 0; i < maxSize; i++ {
		pos = g.adjustResourcePos(pos)
		if !posIsFree(g.world, nil, pos, 8) {
			break
		}
		source := g.world.NewEssenceSourceNode(kind, pos)
		g.pendingResources = append(g.pendingResources, source)
		direction := gmath.RadToVec(rand.Rad()).Mulf(32)
		if rand.Bool() {
			pos = initialPos.Add(direction)
		} else {
			pos = pos.Add(direction)
		}
		placed++
	}
	return placed
}

func (g *levelGenerator) placeResources(resMultiplier float64) {
	rand := &g.rng

	worldSizeMultipliers := []float64{
		0.8,
		0.9,
		1.0,
		1.1,
	}
	multiplier := resMultiplier * worldSizeMultipliers[g.world.config.WorldSize]
	numIron := int(float64(rand.IntRange(28, 42)) * multiplier)
	numScrap := int(float64(rand.IntRange(6, 8)) * multiplier)
	numGold := int(float64(rand.IntRange(20, 28)) * multiplier)
	numCrystals := int(float64(rand.IntRange(14, 20)) * multiplier)
	numOil := int(float64(rand.IntRange(4, 6)) * multiplier)
	numOrganic := int(float64(rand.IntRange(16, 24)) * multiplier)

	switch gamedata.EnvironmentKind(g.world.config.Environment) {
	case gamedata.EnvMoon:
		numOrganic = 0
	case gamedata.EnvForest:
		numIron = 0
		numCrystals /= 2
		numGold = int(float64(numGold) * 0.7)
		numOil = int(float64(numOil) * 1.5)
	}

	if !g.world.config.GoldEnabled {
		numGold = 0
	}

	numRedOil := 0
	numRedCrystals := 0
	if g.world.config.EliteResources {
		numRedOil = gmath.ClampMin(int(float64(rand.IntRange(2, 3))*multiplier), 2)
		numRedCrystals = int(float64(rand.IntRange(10, 15)) * multiplier)
	}
	if g.world.seedKind == gamedata.SeedLeet {
		numRedCrystals *= 2
	}

	g.world.numRedCrystals = numRedCrystals

	g.sectorSlider.TrySetValue(rand.IntRange(0, len(g.sectors)-1))

	for numOrganic > 0 {
		clusterSize := rand.IntRange(1, 2)
		sector := g.sectors[g.sectorSlider.Value()]
		g.sectorSlider.Inc()
		numOrganic -= g.placeResourceCluster(sector, clusterSize, organicSource)
	}

	for numIron > 0 {
		clusterSize := rand.IntRange(2, 6)
		sector := g.sectors[g.sectorSlider.Value()]
		g.sectorSlider.Inc()
		numIron -= g.placeResourceCluster(sector, gmath.ClampMax(clusterSize, numIron), ironSource)
	}
	for numScrap > 0 {
		sector := g.sectors[g.sectorSlider.Value()]
		g.sectorSlider.Inc()
		clusterSize := 1
		if rand.Chance(0.3) {
			clusterSize = 2
		}
		kind := smallScrapSource
		roll := rand.Float()
		if roll > 0.8 {
			kind = bigScrapCreepSource
		} else if roll > 0.4 {
			kind = scrapSource
		}
		numScrap -= g.placeResourceCluster(sector, gmath.ClampMax(clusterSize, numScrap), kind)
	}
	for numGold > 0 {
		clusterSize := rand.IntRange(1, 3)
		sector := g.sectors[g.sectorSlider.Value()]
		g.sectorSlider.Inc()
		numGold -= g.placeResourceCluster(sector, gmath.ClampMax(clusterSize, numGold), goldSource)
	}
	for numOil > 0 {
		sector := g.sectors[g.sectorSlider.Value()]
		g.sectorSlider.Inc()
		numOil -= g.placeResourceCluster(sector, 1, oilSource)
	}
	for numRedOil > 0 {
		sector := g.sectors[g.sectorSlider.Value()]
		g.sectorSlider.Inc()
		numRedOil -= g.placeResourceCluster(sector, 1, redOilSource)
	}
	for numRedCrystals > 0 {
		sector := g.sectors[g.sectorSlider.Value()]
		g.sectorSlider.Inc()
		numRedCrystals -= g.placeResourceCluster(sector, 1, redCrystalSource)
	}
	for numCrystals > 0 {
		clusterSize := 1
		if rand.Chance(0.4) {
			clusterSize = 2
		}
		sector := g.sectors[g.sectorSlider.Value()]
		g.sectorSlider.Inc()
		numCrystals -= g.placeResourceCluster(sector, gmath.ClampMax(clusterSize, numCrystals), crystalSource)
	}

	if g.world.config.GameMode != gamedata.ModeTutorial {
		g.deployStartingResources()
	}

	// Now sort all resources by their Y coordinate and only
	// then add them to the scene.
	sort.Slice(g.pendingResources, func(i, j int) bool {
		return g.pendingResources[i].pos.Y < g.pendingResources[j].pos.Y
	})
	for _, source := range g.pendingResources {
		g.world.nodeRunner.AddObject(source)
	}
}

func (g *levelGenerator) deployStartingResources() {
	rand := &g.rng

	// If there are no resources near the colony spawn pos,
	// place something in there.
	for _, core := range g.world.allColonies {
		hasResources := xslices.ContainsWhere(g.world.essenceSources, func(source *essenceSourceNode) bool {
			// We don't count scraps as some viable starting resource.
			return source.pos.DistanceTo(core.pos) <= core.realRadius &&
				source.stats != scrapSource &&
				source.stats != smallScrapSource
		})
		if !hasResources {
			resNum := 0
			var res *essenceSourceStats
			switch gamedata.EnvironmentKind(g.world.config.Environment) {
			case gamedata.EnvMoon:
				res = ironSource
				resNum = 2
			case gamedata.EnvForest:
				res = oilSource
				resNum = 1
			}
			for i := 0; i < resNum; i++ {
				for j := 0; j < 5; j++ {
					pos := g.adjustResourcePos(gmath.RadToVec(rand.Rad()).Mulf(80).Add(core.pos))
					if !posIsFree(g.world, nil, pos, 14) {
						continue
					}
					essence := g.world.NewEssenceSourceNode(res, pos)
					g.pendingResources = append(g.pendingResources, essence)
					break
				}
			}
		}
	}
}

func (g *levelGenerator) placeBoss() {
	if !g.world.config.EnemyBoss {
		return
	}

	spawnLocations := []gmath.Vec{
		{X: 196, Y: 196},
		{X: g.world.width - 196, Y: 196},
		{X: 196, Y: g.world.height - 196},
		{X: g.world.width - 196, Y: g.world.height - 196},
	}
	pos := gmath.RandElem(&g.rng, spawnLocations)
	boss := g.world.NewCreepNode(pos, gamedata.UberBossCreepStats)
	if g.world.config.GameMode == gamedata.ModeReverse {
		boss.specialDelay = 60 * 60 * 60 // ~never
	} else {
		boss.specialDelay = g.rng.FloatRange(3*60, 4*60)
	}
	boss.super = g.world.config.SuperCreeps
	g.world.nodeRunner.AddObject(boss)

	g.world.boss = boss
}

func (g *levelGenerator) placeCreeps() {
	rand := &g.rng
	worldSizeMultipliers := []float64{
		0.7,
		0.9,
		1.1,
		1.4,
	}
	multiplier := worldSizeMultipliers[g.world.config.WorldSize]
	if g.world.config.InitialCreeps > 1 {
		multiplier *= 2
	}

	numIonMortars := 0
	if g.world.config.IonMortars {
		numIonMortars = int(2 * multiplier)
	}
	if g.world.seedKind == gamedata.SeedLeet {
		numIonMortars += 8
	}
	placedSuperMortar := false
	for numIonMortars > 0 {
		sector := g.sectors[g.sectorSlider.Value()]
		g.sectorSlider.Inc()
		creep := g.placeCreep(sector, gamedata.IonMortarCreepStats, creepPlacingConfig{
			Pad:      200,
			NoScraps: true,
			CreepInit: func(creep *creepNode) {
				if !placedSuperMortar && g.world.config.SuperCreeps {
					creep.super = true
					placedSuperMortar = true
				}
			},
		})
		if creep != nil {
			region := gmath.Rect{
				Min: creep.pos.Sub(gmath.Vec{X: 96, Y: 96}),
				Max: creep.pos.Add(gmath.Vec{X: 96, Y: 96}),
			}
			numGuards := 2
			guardStats := gamedata.HeavyCrawlerCreepStats
			if rand.Bool() {
				numGuards = rand.IntRange(3, 5)
				guardStats = gamedata.CrawlerCreepStats
			}
			for try := 0; try < 5 && numGuards > 0; try++ {
				numGuards -= g.placeCreepsCluster(region, numGuards, guardStats, creepPlacingConfig{
					Pad:      8,
					NoScraps: true,
				})
			}
			numIonMortars--
		}
	}

	if g.world.config.InitialCreeps == 0 {
		return
	}

	g.sectorSlider.TrySetValue(rand.IntRange(0, len(g.sectors)-1))

	numTurrets := int(math.Round(float64(rand.IntRange(4, 5)) * multiplier))
	for numTurrets > 0 {
		sector := g.sectors[g.sectorSlider.Value()]
		g.sectorSlider.Inc()
		numTurrets -= g.placeCreepsCluster(sector, 1, gamedata.TurretCreepStats, creepPlacingConfig{Pad: 140})
	}

	numCrawlers := int(math.Round(float64(rand.IntRange(8, 12)) * multiplier))
	for numCrawlers > 0 {
		sector := g.sectors[g.sectorSlider.Value()]
		g.sectorSlider.Inc()
		stats := gamedata.CrawlerCreepStats
		if g.rng.Chance(0.4) {
			stats = gamedata.HeavyCrawlerCreepStats
		}
		numCrawlers -= g.placeCreepsCluster(sector, 1, stats, creepPlacingConfig{Pad: 128})
	}

	numSpecial := 0
	specialStats := gamedata.HowitzerCreepStats
	if g.world.config.InitialCreeps > 1 {
		numSpecial = 1
		if g.world.rand.Bool() {
			specialStats = gamedata.BuilderCreepStats
		}
	}
	for numSpecial > 0 {
		sector := g.sectors[g.sectorSlider.Value()]
		g.sectorSlider.Inc()
		numSpecial -= g.placeCreepsCluster(sector, 1, specialStats, creepPlacingConfig{Pad: 196, NoScraps: true})
	}
}

func (g *levelGenerator) placeCreepBases() {
	numWispLairs := 0
	if gamedata.EnvironmentKind(g.world.config.Environment) == gamedata.EnvForest {
		numWispLairs = 1
	}
	hasWispLair := numWispLairs > 0
	for numWispLairs > 0 {
		sector := g.sectors[g.sectorSlider.Value()]
		g.sectorSlider.Inc()
		numWispLairs -= g.placeCreepsCluster(sector, 1, gamedata.WispLairCreepStats, creepPlacingConfig{Pad: 196})
	}

	numFortresses := 0
	if g.world.config.CreepFortress {
		numFortresses = 1
	}
	hasFortresses := numFortresses > 0
	for numFortresses > 0 {
		sector := g.sectors[g.sectorSlider.Value()]
		g.sectorSlider.Inc()
		numFortresses -= g.placeCreepsCluster(sector, 1, gamedata.FortressCreepStats, creepPlacingConfig{Pad: 256})
	}

	if hasWispLair || hasFortresses {
		for _, creep := range g.world.creeps {
			switch creep.stats.Kind {
			case gamedata.CreepWispLair:
				g.world.wispLair = creep
			case gamedata.CreepFortress:
				g.world.fortress = creep
			}
		}
	}

	if g.world.fortress != nil {
		// Place turrets around the fortress.
		offsets := []gmath.Vec{
			{X: -34, Y: -34},
			{X: 34, Y: -34},
			{X: 34, Y: 34},
			{X: -34, Y: 34},
		}
		for _, offset := range offsets {
			turret := g.world.NewCreepNode(g.world.fortress.pos.Add(offset), gamedata.TurretCreepStats)
			g.world.nodeRunner.AddObject(turret)
		}
	}

	if g.world.config.NumCreepBases == 0 {
		return // Zero bases
	}
	// The bases are always located somewhere on the map boundary.
	// Bases can't be on the same border.
	pad := 128.0
	borderWidth := 440.0
	borders := []gmath.Rect{
		// top border
		{Min: gmath.Vec{X: pad, Y: pad}, Max: gmath.Vec{X: g.world.width - pad, Y: borderWidth + pad}},
		// right border
		{Min: gmath.Vec{X: g.world.width - borderWidth - pad, Y: pad}, Max: gmath.Vec{X: g.world.width - pad, Y: g.world.height - pad}},
		// left border
		{Min: gmath.Vec{X: pad, Y: pad}, Max: gmath.Vec{X: borderWidth + pad, Y: g.world.height - pad}},
		// bottom border
		{Min: gmath.Vec{X: pad, Y: g.world.height - borderWidth - pad}, Max: gmath.Vec{X: g.world.width - pad, Y: g.world.height - pad}},
	}
	gmath.Shuffle(&g.rng, borders)
	var borderSlider gmath.Slider
	borderSlider.SetBounds(0, len(borders)-1)
	numBases := g.world.config.NumCreepBases
	for i := 0; i < numBases; i++ {
		border := borders[borderSlider.Value()]
		borderSlider.Inc()
		basePos := g.randomFreePos(border, 48, 32)
		basePos = g.world.AdjustCellPos(basePos, 6)
		if g.world.debugLogs {
			g.world.sessionState.Logf("deployed a creep base %d at %v distance is %f", i+1, basePos, basePos.DistanceTo(g.playerSpawn))
		}
		baseRegion := gmath.Rect{
			Min: basePos.Sub(gmath.Vec{X: 148, Y: 148}),
			Max: basePos.Add(gmath.Vec{X: 148, Y: 148}),
		}
		super := i == 0 && g.world.config.SuperCreeps

		if g.world.seedKind == gamedata.SeedLeet {
			for attempt := 0; attempt < 3; attempt++ {
				crawlersFactory := g.placeCreep(baseRegion, gamedata.CrawlerBaseCreepStats, creepPlacingConfig{
					CreepInit: func(creep *creepNode) {
						creep.super = super
					},
					NoScraps: true,
					Pad:      8,
				})
				if crawlersFactory != nil {
					break
				}
			}
		}

		g.placeCreepsCluster(baseRegion, 1, gamedata.TurretCreepStats, creepPlacingConfig{Pad: 24, NoScraps: true})
		base := g.world.NewCreepNode(basePos, gamedata.BaseCreepStats)
		base.super = super
		if i == 1 || i == 3 {
			base.specialDelay = (9 * 60.0) * g.rng.FloatRange(0.9, 1.1)
		} else {
			base.specialModifier = 1.0 // Initial base level
			base.specialDelay = g.rng.FloatRange(60, 120)
			base.attackDelay = g.rng.FloatRange(40, 50)
		}
		g.world.nodeRunner.AddObject(base)
	}
}

func (g *levelGenerator) placeForests() {
	if gamedata.EnvironmentKind(g.world.config.Environment) != gamedata.EnvForest {
		return
	}

	rand := &g.rng

	playerTerritory := gmath.Rect{
		Min: g.playerSpawn.Sub(gmath.Vec{X: 96, Y: 96}),
		Max: g.playerSpawn.Add(gmath.Vec{X: 96, Y: 96}),
	}

	maxForests := g.world.config.WorldSize + 2
	if g.world.config.WorldSize == 3 {
		maxForests++
	}
	switch g.world.config.Terrain {
	case 0: // flat
		maxForests--
	case 2: // less flat
		maxForests++
	}

	var trees []pendingImage

	for _, sector := range g.sectors {
		numForests := rand.IntRange(1, maxForests)
		for i := 0; i < numForests; i++ {
			pos := g.world.pathgrid.AlignPos(g.randomFreePos(sector, 32, 96)).Sub(gmath.Vec{
				X: pathing.CellSize * 0.5,
				Y: pathing.CellSize * 0.5,
			})
			width := g.world.rand.IntRange(6, 16)
			height := g.world.rand.IntRange(6, 16)

			xOverflow := (pos.X + float64(width)*32) - (g.world.width - 32.0)
			yOverflow := (pos.Y + float64(height)*32) - (g.world.height - 32.0)
			if xOverflow > 0 {
				pos.X -= xOverflow
			}
			if yOverflow > 0 {
				pos.Y -= yOverflow
			}

			forest := newForestClusterNode(g.world, forestClusterConfig{
				pos:    pos,
				width:  width,
				height: height,
			})
			if forest.outerRect.Overlaps(playerTerritory) {
				continue
			}

			trees = append(trees, forest.init(g.scene)...)

			// TODO: move it to fillPathgrid step or maybe get rid of that stage instead?
			forest.walkRects(func(rect gmath.Rect) {
				g.fillPathgridRect(rect, ptagForest)
			})

			g.world.forests = append(g.world.forests, forest)
		}
	}

	if len(trees) != 0 {
		sort.SliceStable(trees, func(i, j int) bool {
			return trees[i].drawOrder < trees[j].drawOrder
		})
		for _, img := range trees {
			g.bg.DrawImage(img.data, &img.options)
		}
	}
}

func (g *levelGenerator) placeWalls() {
	rand := &g.rng

	worldSizeMultipliers := [...]float64{
		0.5,
		0.75,
		1.0,
		1.4,
	}
	terrainMultiplier := [...]float64{
		0.2,
		1.0,
		2.5,
	}
	multiplier := worldSizeMultipliers[g.world.config.WorldSize] * terrainMultiplier[g.world.config.Terrain]
	numWallClusters := int(float64(rand.IntRange(8, 10)) * multiplier)
	numMountains := int(float64(rand.IntRange(5, 9)) * multiplier)

	switch gamedata.EnvironmentKind(g.world.config.Environment) {
	case gamedata.EnvMoon:
		// Nothing to do.
	case gamedata.EnvForest:
		numWallClusters = 0
	}

	const (
		// A simple 1x1 wall tile (rect shape: true).
		wallPit int = iota
		// A straight line shaped wall.
		wallLine
		// Like a line, but may have branches.
		wallSpikedLine
		// A randomly drawed shape.
		wallSnake
		// A cross-like shape.
		wallCrossway
		// Like a spiked line, but more predictable.
		wallZap
	)

	shapePicker := gmath.NewRandPicker[int](rand)
	shapePicker.AddOption(wallPit, 0.05)
	shapePicker.AddOption(wallLine, 0.1)
	shapePicker.AddOption(wallSpikedLine, 0.15)
	shapePicker.AddOption(wallZap, 0.25)
	shapePicker.AddOption(wallSnake, 0.05)
	shapePicker.AddOption(wallCrossway, 0.1)

	directions := []gmath.Vec{
		{X: wallTileSize},
		{X: -wallTileSize},
		{Y: wallTileSize},
		{Y: -wallTileSize},
	}

	chooseRandDirection := func() gmath.Vec {
		roll := rand.IntRange(0, 4)
		var d gmath.Vec
		switch roll {
		case 0:
			d.X = wallTileSize
		case 1:
			d.X = -wallTileSize
		case 2:
			d.Y = wallTileSize
		default:
			d.Y = -wallTileSize
		}
		return d
	}

	reverseDirection := func(d gmath.Vec) gmath.Vec {
		reversed := d
		reversed.X = -d.X
		reversed.Y = -d.Y
		return d
	}

	rotateDirection := func(d gmath.Vec) gmath.Vec {
		rotated := d
		if rand.Bool() {
			rotated.X = d.Y
			rotated.Y = d.X
		} else {
			rotated.X = -d.Y
			rotated.Y = -d.X
		}
		return rotated
	}

	removeDuplicates := func(points []gmath.Vec) []gmath.Vec {
		set := make(map[gmath.Vec]struct{})
		for _, p := range points {
			set[p] = struct{}{}
		}
		filtered := points[:0]
		for p := range set {
			filtered = append(filtered, p)
		}
		return filtered
	}

	g.sectorSlider.TrySetValue(rand.IntRange(0, len(g.sectors)-1))
	for i := 0; i < numWallClusters; i++ {
		sector := g.sectors[g.sectorSlider.Value()]
		g.sectorSlider.Inc()

		var pos gmath.Vec
		for i := 0; i < 10; i++ {
			pos = correctedPos(sector, g.randomPos(sector), 96)
			if posIsFree(g.world, nil, pos, 48) {
				break
			}
		}
		if pos.IsZero() {
			continue
		}

		// Wall positions should be rounded to a tile size.
		{
			x := math.Floor(pos.X/wallTileSize) * wallTileSize
			y := math.Floor(pos.Y/wallTileSize) * wallTileSize
			pos = gmath.Vec{X: x + wallTileSize/2, Y: y + wallTileSize/2}
		}

		var config wallClusterConfig
		shape := shapePicker.Pick()
		switch shape {
		case wallPit:
			config.points = append(config.points, pos)

		case wallCrossway:
			config.points = append(config.points, pos)
			for _, dir := range directions {
				length := rand.IntRange(0, 3)
				currentPos := pos
				for i := 0; i < length; i++ {
					currentPos = currentPos.Add(dir)
					if !posIsFree(g.world, nil, currentPos, 48) {
						break
					}
					config.points = append(config.points, currentPos)
				}
			}

		case wallZap:
			numJoints := rand.IntRange(1, 3) + 1
			dir := chooseRandDirection()
			config.points = append(config.points, pos)
			currentPos := pos
		OuterLoop:
			for i := 0; i < numJoints; i++ {
				length := rand.IntRange(2, 4)
				for j := 0; j < length; j++ {
					currentPos = currentPos.Add(dir)
					if !posIsFree(g.world, nil, currentPos, 48) {
						break OuterLoop
					}
					config.points = append(config.points, currentPos)
					if len(config.points) == maxWallSegments {
						break OuterLoop
					}
				}
				jointPos := currentPos.Add(rotateDirection(dir))
				if !posIsFree(g.world, nil, jointPos, 48) {
					break
				}
				currentPos = jointPos
				config.points = append(config.points, jointPos)
				if len(config.points) == maxWallSegments {
					break OuterLoop
				}
			}

		case wallSnake:
			steps := rand.IntRange(3, maxWallSegments-1)
			config.points = append(config.points, pos)
			currentPos := pos
			dir := chooseRandDirection()
			for i := 0; i < steps; i++ {
				currentPos = currentPos.Add(dir)
				if !posIsFree(g.world, nil, currentPos, 48) {
					break
				}
				config.points = append(config.points, currentPos)
				roll := rand.Float()
				if roll < 0.2 {
					currentPos = pos
				} else if roll < 0.35 {
					dir = chooseRandDirection()
				} else if roll < 0.5 {
					dir = rotateDirection(dir)
				}
			}
			config.points = removeDuplicates(config.points)

		case wallLine, wallSpikedLine:
			spiked := shape == wallSpikedLine
			config.points = append(config.points, pos)
			currentPos := pos
			maxLength := 3
			if spiked {
				maxLength = 6
			}
			lengthRoll := rand.IntRange(1, maxLength)
			length := lengthRoll
			dir := chooseRandDirection()
			prevSpikeDir := dir
			for length > 0 {
				currentPos = currentPos.Add(dir)
				if !posIsFree(g.world, nil, currentPos, 48) {
					break
				}
				if spiked && rand.Chance(0.5) {
					spikeDir := rotateDirection(dir)
					sidePos := currentPos.Add(spikeDir)
					if spikeDir != prevSpikeDir || g.world.rand.Chance(0.15) {
						if posIsFree(g.world, nil, sidePos, 48) {
							prevSpikeDir = spikeDir
							config.points = append(config.points, sidePos)
						}
					}
				}
				config.points = append(config.points, currentPos)
				length--
			}
			if length > 0 {
				dir = reverseDirection(dir)
				for length > 0 {
					currentPos = currentPos.Add(dir)
					if !posIsFree(g.world, nil, currentPos, 48) {
						break
					}
					config.points = append(config.points, currentPos)
					length--
				}
			}
		}

		if len(config.points) != 0 {
			config.atlas = wallAtras{layers: landcrackAtlas}
			config.world = g.world
			wall := g.world.NewWallClusterNode(config)
			g.scene.AddObject(wall)
			wall.initOriented(g.bg, g.scene)
		}
	}

	chunkSizePicker := gmath.NewRandPicker[mountainKind](rand)
	chunkSizePicker.AddOption(mountainSmall, 0.15)
	chunkSizePicker.AddOption(mountainMedium, 0.25)
	chunkSizePicker.AddOption(mountainBig, 0.45)
	chunkSizePicker.AddOption(mountainWide, 0.1)
	chunkSizePicker.AddOption(mountainTall, 0.05)
	sideChunkSizePicker := gmath.NewRandPicker[mountainKind](rand)
	sideChunkSizePicker.AddOption(mountainSmall, 0.45)
	sideChunkSizePicker.AddOption(mountainMedium, 0.3)
	sideChunkSizePicker.AddOption(mountainBig, 0.1)
	sideChunkSizePicker.AddOption(mountainWide, 0.1)
	sideChunkSizePicker.AddOption(mountainTall, 0.05)
	for i := 0; i < numMountains; i++ {
		sector := g.sectors[g.sectorSlider.Value()]
		g.sectorSlider.Inc()

		var pos gmath.Vec
		for i := 0; i < 10; i++ {
			pos = correctedPos(sector, g.randomPos(sector), 196)
			if posIsFree(g.world, nil, pos, 80) {
				break
			}
		}
		if pos.IsZero() {
			continue
		}

		// Positions should be rounded to a tile size.
		pos = g.world.pathgrid.AlignPos(pos)

		currentPos := pos
		mountainLength := g.rng.IntRange(3, 10)
		widthModifier := g.rng.Float()
		dir := chooseRandDirection()
		chunks := make([]wallChunk, 0, mountainLength+4)
		for j := 0; j < mountainLength; j++ {
			chunkSize := chunkSizePicker.Pick()
			chunks = append(chunks, wallChunk{
				pos:  currentPos,
				kind: chunkSize,
			})
			rotateRoll := g.rng.Float()
			if rotateRoll < 0.1 {
				dir = rotateDirection(dir)
			} else if rotateRoll < 0.45 {
				extraPos := currentPos.Add(rotateDirection(dir))
				if posIsFree(g.world, nil, extraPos, 64) {
					chunks = append(chunks, wallChunk{
						pos:  extraPos,
						kind: sideChunkSizePicker.Pick(),
					})
				}
			} else if rotateRoll < widthModifier {
				parallelDir := dir
				parallelDir.X = dir.Y
				parallelDir.Y = dir.X
				extraPos := currentPos.Add(parallelDir)
				if posIsFree(g.world, nil, extraPos, 64) {
					chunks = append(chunks, wallChunk{
						pos:  extraPos,
						kind: chunkSizePicker.Pick(),
					})
				}
			}
			currentPos = currentPos.Add(dir)
			if !posIsFree(g.world, nil, currentPos, 80) {
				break
			}
		}
		var config wallClusterConfig
		config.chunks = chunks
		config.world = g.world
		wall := g.world.NewWallClusterNode(config)
		g.scene.AddObject(wall)
		wall.initChunks(g.bg, g.scene)
	}
}
