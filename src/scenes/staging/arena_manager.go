package staging

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/quasilyte/ge"
	"github.com/quasilyte/ge/xslices"
	"github.com/quasilyte/gmath"
	"github.com/quasilyte/gsignal"
	"github.com/quasilyte/roboden-game/assets"
	"github.com/quasilyte/roboden-game/gamedata"
	"github.com/quasilyte/roboden-game/timeutil"
)

const maxArenaGroupBudget = 120

type arenaCreepInfo struct {
	stats    *gamedata.CreepStats
	cost     int
	minLevel int
}

type arenaManager struct {
	level      int
	waveBudget int
	lastLevel  int

	info                 *messageNode
	overviewText         string
	infoUpdateDelay      float64
	levelStartDelay      float64
	budgetStepMultiplier float64

	victory  bool
	infArena bool

	attackSides []int

	waveInfo arenaWaveInfo

	scene *ge.Scene
	world *worldState

	creepSelectionSlice      []*arenaCreepInfo
	groupCreepSelectionSlice []*arenaCreepInfo
	basicFlyingCreeps        []*arenaCreepInfo
	basicGroundCreeps        []*arenaCreepInfo

	crawlerCreepInfo        *arenaCreepInfo
	eliteCrawlerCreepInfo   *arenaCreepInfo
	stealthCrawlerCreepInfo *arenaCreepInfo
	heavyCrawlerCreepInfo   *arenaCreepInfo
	wandererCreepInfo       *arenaCreepInfo
	stunnerCreepInfo        *arenaCreepInfo
	assaultCreepInfo        *arenaCreepInfo
	builderCreepInfo        *arenaCreepInfo
	templarCreepInfo        *arenaCreepInfo

	EventVictory gsignal.Event[gsignal.Void]
}

type arenaWaveUnit struct {
	stats     *gamedata.CreepStats
	super     bool
	fragScore int
}

type arenaWaveGroup struct {
	units     []arenaWaveUnit
	totalCost int
	side      int
}

type arenaWaveInfo struct {
	groups []arenaWaveGroup

	isLast          bool
	dominator       bool
	howitzer        bool
	taskForce       bool
	builders        bool
	flyingAttackers bool
	groundAttackers bool

	attackSides [4]bool
}

func newArenaManager(world *worldState) *arenaManager {
	return &arenaManager{
		world: world,
		waveInfo: arenaWaveInfo{
			groups: make([]arenaWaveGroup, 0, 8),
		},
		attackSides:              []int{0, 1, 2, 3},
		creepSelectionSlice:      make([]*arenaCreepInfo, 0, 16),
		groupCreepSelectionSlice: make([]*arenaCreepInfo, 0, 16),
		infArena:                 world.config.GameMode == gamedata.ModeInfArena,
	}
}

func (m *arenaManager) IsDisposed() bool {
	return false
}

func (m *arenaManager) Init(scene *ge.Scene) {
	m.scene = scene

	if !m.infArena {
		m.lastLevel = 20
	}

	m.level = 1

	m.crawlerCreepInfo = &arenaCreepInfo{
		stats: gamedata.CrawlerCreepStats,
		cost:  creepFragScore(gamedata.CrawlerCreepStats),
	}
	m.eliteCrawlerCreepInfo = &arenaCreepInfo{
		stats:    gamedata.EliteCrawlerCreepStats,
		cost:     creepFragScore(gamedata.EliteCrawlerCreepStats),
		minLevel: 2,
	}
	m.stealthCrawlerCreepInfo = &arenaCreepInfo{
		stats:    gamedata.StealthCrawlerCreepStats,
		cost:     creepFragScore(gamedata.StealthCrawlerCreepStats),
		minLevel: 3,
	}
	m.heavyCrawlerCreepInfo = &arenaCreepInfo{
		stats:    gamedata.HeavyCrawlerCreepStats,
		minLevel: 9,
		cost:     creepFragScore(gamedata.HeavyCrawlerCreepStats),
	}

	m.wandererCreepInfo = &arenaCreepInfo{
		stats: gamedata.WandererCreepStats,
		cost:  creepFragScore(gamedata.WandererCreepStats),
	}
	m.stunnerCreepInfo = &arenaCreepInfo{
		stats:    gamedata.StunnerCreepStats,
		cost:     creepFragScore(gamedata.StunnerCreepStats),
		minLevel: 2,
	}
	m.assaultCreepInfo = &arenaCreepInfo{
		stats:    gamedata.AssaultCreepStats,
		cost:     creepFragScore(gamedata.AssaultCreepStats),
		minLevel: 6,
	}
	m.builderCreepInfo = &arenaCreepInfo{
		stats:    gamedata.BuilderCreepStats,
		cost:     creepFragScore(gamedata.BuilderCreepStats),
		minLevel: 7,
	}
	m.templarCreepInfo = &arenaCreepInfo{
		stats:    gamedata.TemplarCreepStats,
		cost:     creepFragScore(gamedata.TemplarCreepStats),
		minLevel: 11,
	}

	m.basicFlyingCreeps = []*arenaCreepInfo{
		m.wandererCreepInfo,
		m.stunnerCreepInfo,
		m.assaultCreepInfo,
		m.templarCreepInfo,
	}
	m.basicGroundCreeps = []*arenaCreepInfo{
		m.crawlerCreepInfo,
		m.eliteCrawlerCreepInfo,
		m.stealthCrawlerCreepInfo,
		m.heavyCrawlerCreepInfo,
	}

	m.budgetStepMultiplier = 0.75 + (float64(m.world.config.ArenaProgression) * 0.25)
	m.infoUpdateDelay = 5
	m.prepareWave()
	m.overviewText = m.createWaveOverviewText()
	if len(m.world.cameras) != 0 {
		m.info = m.createWaveInfoMessageNode()
		m.world.nodeRunner.AddObject(m.info)
	}
}

func (m *arenaManager) Update(delta float64) {
	if m.victory {
		return
	}

	m.levelStartDelay -= delta
	if m.levelStartDelay <= 0 {
		if !m.infArena && m.level > m.lastLevel {
			m.victory = true
			m.info.Dispose()
			m.EventVictory.Emit(gsignal.Void{})
			return
		}
		m.spawnCreeps()
		m.level++
		m.prepareWave()
		m.overviewText = m.createWaveOverviewText()
		if m.info != nil {
			m.info.Dispose()
		}
		if len(m.world.cameras) != 0 {
			m.info = m.createWaveInfoMessageNode()
			m.world.nodeRunner.AddObject(m.info)
		}
	}

	m.infoUpdateDelay -= delta
	if m.infoUpdateDelay <= 0 {
		m.infoUpdateDelay = 5 + m.infoUpdateDelay
		m.info.UpdateText(m.createWaveInfoText())
	}
}

func (m *arenaManager) createWaveInfoMessageNode() *messageNode {
	s := m.createWaveInfoText()
	message := newScreenTutorialHintNode(m.world.cameras[0], gmath.Vec{X: 16, Y: 70}, gmath.Vec{}, s)
	message.xpadding = 20
	return message
}

func (m *arenaManager) createWaveOverviewText() string {
	if !m.infArena && m.level > m.lastLevel {
		return ""
	}

	d := m.scene.Dict()

	var buf strings.Builder
	buf.Grow(128)

	buf.WriteString(d.Get("game.wave_direction"))
	buf.WriteString(": ")
	if m.waveInfo.attackSides == [4]bool{true, true, true, true} {
		buf.WriteString(d.Get("game.side.all"))
	} else {
		sideParts := make([]string, 0, 2)
		for side, hasAttackers := range m.waveInfo.attackSides {
			if !hasAttackers {
				continue
			}
			sideParts = append(sideParts, d.Get(sideName(side)))
		}
		buf.WriteString(strings.Join(sideParts, ", "))
	}

	unitKindParts := make([]string, 0, 4)
	if m.waveInfo.groundAttackers {
		unitKindParts = append(unitKindParts, d.Get("drone.target.ground"))
	}
	if m.waveInfo.flyingAttackers {
		unitKindParts = append(unitKindParts, d.Get("drone.target.flying"))
	}
	buf.WriteByte('\n')
	buf.WriteString(d.Get("game.wave_units"))
	buf.WriteString(": ")
	buf.WriteString(strings.Join(unitKindParts, ", "))

	specialParts := make([]string, 0, 2)
	if m.waveInfo.isLast {
		specialParts = append(specialParts, "???")
	} else {
		if m.waveInfo.howitzer {
			specialParts = append(specialParts, d.Get("game.wave_howitzer"))
		}
		if m.waveInfo.dominator {
			specialParts = append(specialParts, d.Get("game.wave_dominator"))
		}
		if m.waveInfo.taskForce {
			specialParts = append(specialParts, d.Get("game.wave_task_force"))
		}
		if m.waveInfo.builders {
			specialParts = append(specialParts, d.Get("game.wave_builders"))
		}
	}
	if len(specialParts) != 0 {
		buf.WriteByte('\n')
		buf.WriteString(d.Get("game.wave_special_units"))
		buf.WriteString(": ")
		buf.WriteString(strings.Join(specialParts, ", "))
	}

	return buf.String()
}

func (m *arenaManager) createWaveInfoText() string {
	d := m.scene.Dict()

	var buf strings.Builder
	buf.Grow(256)

	if !m.infArena && m.level > m.lastLevel {
		buf.WriteString(d.Get("game.wave_last"))
		buf.WriteString(": ")
		buf.WriteString(timeutil.FormatDuration(d, time.Duration(m.levelStartDelay*float64(time.Second))))
		return buf.String()
	}

	buf.WriteString(d.Get("game.wave"))
	buf.WriteByte(' ')
	buf.WriteString(strconv.Itoa(m.level))
	buf.WriteString(" ")
	buf.WriteString(d.Get("game.wave_starts_in"))
	buf.WriteByte(' ')
	buf.WriteString(timeutil.FormatDuration(d, time.Duration(m.levelStartDelay*float64(time.Second))))
	if m.overviewText != "" {
		buf.WriteByte('\n')
		buf.WriteString(m.overviewText)
	}

	return buf.String()
}

func (m *arenaManager) spawnCreeps() {
	m.scene.Audio().PlaySound(assets.AudioWaveStart)

	isLastLevel := !m.infArena && m.level == m.lastLevel

	for _, g := range m.waveInfo.groups {
		if isLastLevel {
			for i := range g.units {
				u := &g.units[i]
				u.fragScore = creepCost(u.stats, u.super)
				m.world.result.CreepTotalValue += u.fragScore
			}
		}
		sendCreeps(m.world, g)
	}
}

func (m *arenaManager) prepareWave() {
	if !m.infArena && m.level > m.lastLevel {
		m.levelStartDelay = 5.0 * 60
		m.waveInfo = arenaWaveInfo{}
		return
	}

	isLastLevel := !m.infArena && m.level == m.lastLevel

	budgetStep := 0
	switch {
	case isLastLevel:
		m.levelStartDelay = 4.0 * 60
		budgetStep = 140
	case m.level%5 == 0:
		m.levelStartDelay = 4.0 * 60
		budgetStep = 20
		if m.infArena {
			budgetStep += m.level
		} else {
			budgetStep += 2 * m.level
		}
	case m.level == 1:
		m.levelStartDelay = 90
		m.waveBudget = 25
	default:
		m.levelStartDelay = 2.5 * 60
		budgetStep = 10
	}
	m.waveBudget += int(math.Round(float64(budgetStep) * m.budgetStepMultiplier))

	budget := m.waveBudget
	if m.world.config.ExecMode != gamedata.ExecuteSimulation {
		m.world.sessionState.Logf("wave %d budget is %d", m.level, budget)
	}

	// First decide which kind of attack we're doing.
	attackDirectionRoll := m.world.rand.Float()
	numAttackSides := 0
	budgetMultiplier := 1.0
	switch {
	case attackDirectionRoll < 0.5:
		numAttackSides = 1
	case attackDirectionRoll < 0.8:
		numAttackSides = 2
		budgetMultiplier = 0.75
	default:
		numAttackSides = 4
		budgetMultiplier = 0.4
	}

	groups := m.waveInfo.groups[:0]
	m.waveInfo = arenaWaveInfo{}

	gmath.Shuffle(m.world.rand, m.attackSides)
	sides := m.attackSides[:numAttackSides]
	for _, side := range sides {
		m.waveInfo.attackSides[side] = true
		sideBudget := int(math.Round(float64(budget) * budgetMultiplier))
		creepSelection := m.creepSelectionSlice[:0]
		selectionRoll := m.world.rand.Float()
		allowFlying := true
		switch {
		case selectionRoll <= 0.5:
			// Flying-only creeps.
			creepSelection = append(creepSelection, m.basicFlyingCreeps...)
			m.waveInfo.flyingAttackers = true
		case selectionRoll <= 0.8:
			// Ground-only creeps.
			creepSelection = append(creepSelection, m.basicGroundCreeps...)
			m.waveInfo.groundAttackers = true
			allowFlying = false
		default:
			creepSelection = append(creepSelection, m.basicFlyingCreeps...)
			creepSelection = append(creepSelection, m.basicGroundCreeps...)
			m.waveInfo.flyingAttackers = true
			m.waveInfo.groundAttackers = true
		}
		if allowFlying && m.world.rand.Chance(0.6) {
			creepSelection = append(creepSelection, m.builderCreepInfo)
		}

		for sideBudget > 0 {
			groupCreepSelection := m.groupCreepSelectionSlice[:0]
			groupCreepSelection = append(groupCreepSelection, creepSelection...)
			g := arenaWaveGroup{side: side}
			localBudget := sideBudget
			if localBudget > maxArenaGroupBudget {
				localBudget = maxArenaGroupBudget
			}
			sideBudget -= localBudget
			for {
				creep, budgetRemaining, ok := m.pickUnit(localBudget, groupCreepSelection)
				if !ok {
					break
				}
				if creep.stats.Kind == gamedata.CreepBuilder {
					m.waveInfo.builders = true
					groupCreepSelection = xslices.Remove(groupCreepSelection, m.builderCreepInfo)
				}
				localBudget = budgetRemaining
				g.units = append(g.units, creep)
			}
			groups = append(groups, g)
		}
	}

	if m.level > 6 && (m.level%6 == 0) {
		// wave 12 => 3
		// wave 18 => 4
		// wave 24 => 5
		// wave 30 => 6
		// wave 36 => 7
		m.waveInfo.taskForce = true
		numAttackers := 1 + (m.level / 6)
		g := arenaWaveGroup{side: m.attackSides[0]}
		g.units = make([]arenaWaveUnit, numAttackers)
		for i := range g.units {
			super := i == 0
			g.units[i] = arenaWaveUnit{super: super, stats: gamedata.ServantCreepStats}
		}
		groups = append(groups, g)
	}

	if isLastLevel {
		// The last wave.
		m.waveInfo.isLast = true
		for i := 0; i < 3; i++ {
			super := i == 0
			groups[0].units = append(groups[0].units, arenaWaveUnit{super: super, stats: gamedata.DominatorCreepStats})
		}
		for i := 0; i < 2; i++ {
			index := gmath.RandIndex(m.world.rand, groups)
			groups[index].units = append(groups[index].units, arenaWaveUnit{stats: gamedata.HowitzerCreepStats})
		}
		var groupSlider gmath.Slider
		groupSlider.SetBounds(0, len(groups)-1)
		for i := 0; i < 7; i++ {
			super := i <= 1
			index := groupSlider.Value()
			groups[index].units = append(groups[index].units, arenaWaveUnit{super: super, stats: gamedata.ServantCreepStats})
			groupSlider.Inc()
		}
	} else if m.level%5 == 0 {
		// A mini boss wave.
		// 5  => 1 boss
		// 10 => 2 bosses
		// 15 => 3 bosses
		// 20 => 4 bosses
		// ...
		// At the 10th wave, there is always exactly 1 super unit.
		numBosses := m.level / 5
		for i := 0; i < numBosses; i++ {
			super := i == 1
			groupIndex := gmath.RandIndex(m.world.rand, groups)
			if m.world.rand.Bool() {
				groups[groupIndex].units = append(groups[groupIndex].units, arenaWaveUnit{super: super, stats: gamedata.HowitzerCreepStats})
				m.waveInfo.howitzer = true
			} else {
				groups[groupIndex].units = append(groups[groupIndex].units, arenaWaveUnit{super: super, stats: gamedata.DominatorCreepStats})
				m.waveInfo.dominator = true
			}
		}
	}

	m.waveInfo.groups = groups
}

func (m *arenaManager) pickUnit(budget int, selection []*arenaCreepInfo) (arenaWaveUnit, int, bool) {
	var u arenaWaveUnit
	if budget < selection[0].cost {
		return u, budget, false
	}
	creepInfo := randIterate(m.world.rand, selection, func(x *arenaCreepInfo) bool {
		return x.cost <= budget && x.minLevel <= m.level
	})
	if creepInfo.cost != 0 {
		u.stats = creepInfo.stats
		cost := creepInfo.cost
		superCostMultiplier := superCreepCostMultiplier(u.stats)
		if (creepInfo.minLevel+4) <= m.level && creepInfo.cost*superCostMultiplier <= budget {
			// 1  => 0%
			// 4  => 0%
			// 5  => 3%
			// 10 => 18%
			// 15 => 33%
			// 20 => 48%
			// 30 => 78%
			eliteChance := gmath.Clamp(float64(m.level-4)*0.03, 0, 0.9)
			if m.world.rand.Chance(eliteChance) {
				u.super = true
				return u, budget - creepInfo.cost*superCostMultiplier, true
			}
		}
		if creepInfo.cost <= budget {
			return u, budget - cost, true
		}
	}
	return u, budget, false
}
