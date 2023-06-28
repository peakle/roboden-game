// Code generated by "stringer -type=ColonyAgentKind -trimprefix=Agent"; DO NOT EDIT.

package gamedata

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[agentFirst-0]
	_ = x[AgentWorker-1]
	_ = x[AgentScout-2]
	_ = x[AgentFreighter-3]
	_ = x[AgentRedminer-4]
	_ = x[AgentCrippler-5]
	_ = x[AgentFighter-6]
	_ = x[AgentScavenger-7]
	_ = x[AgentCourier-8]
	_ = x[AgentPrism-9]
	_ = x[AgentServo-10]
	_ = x[AgentRepeller-11]
	_ = x[AgentDisintegrator-12]
	_ = x[AgentRepair-13]
	_ = x[AgentCloner-14]
	_ = x[AgentRecharger-15]
	_ = x[AgentGenerator-16]
	_ = x[AgentMortar-17]
	_ = x[AgentAntiAir-18]
	_ = x[AgentDefender-19]
	_ = x[AgentKamikaze-20]
	_ = x[AgentSkirmisher-21]
	_ = x[AgentScarab-22]
	_ = x[AgentRoomba-23]
	_ = x[AgentCommander-24]
	_ = x[AgentGuardian-25]
	_ = x[AgentStormbringer-26]
	_ = x[AgentDestroyer-27]
	_ = x[AgentMarauder-28]
	_ = x[AgentTrucker-29]
	_ = x[AgentDevourer-30]
	_ = x[AgentKindNum-31]
	_ = x[AgentGunpoint-32]
	_ = x[AgentTetherBeacon-33]
	_ = x[AgentBeamTower-34]
	_ = x[agentLast-35]
}

const _ColonyAgentKind_name = "agentFirstWorkerScoutFreighterRedminerCripplerFighterScavengerCourierPrismServoRepellerDisintegratorRepairClonerRechargerGeneratorMortarAntiAirDefenderKamikazeSkirmisherScarabRoombaCommanderGuardianStormbringerDestroyerMarauderTruckerDevourerKindNumGunpointTetherBeaconBeamToweragentLast"

var _ColonyAgentKind_index = [...]uint16{0, 10, 16, 21, 30, 38, 46, 53, 62, 69, 74, 79, 87, 100, 106, 112, 121, 130, 136, 143, 151, 159, 169, 175, 181, 190, 198, 210, 219, 227, 234, 242, 249, 257, 269, 278, 287}

func (i ColonyAgentKind) String() string {
	if i >= ColonyAgentKind(len(_ColonyAgentKind_index)-1) {
		return "ColonyAgentKind(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _ColonyAgentKind_name[_ColonyAgentKind_index[i]:_ColonyAgentKind_index[i+1]]
}
