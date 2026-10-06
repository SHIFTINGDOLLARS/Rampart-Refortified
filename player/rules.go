package main

import "math"

// Difficulty modifiers (Settings.Rules), applied by hooks at the places where
// the game sets its timers and counts. Addresses refer to the unpacked program.

const (
	hookRebuildTimer = 0x154d // rebuild phase: call timer_set, AH = seconds (by difficulty)
	hookCannonTimer  = 0x2d3a // cannon phase: AH = 15
	hookBattleTimer  = 0x4445 // battle, FIRE: AH = 11
	hookCannonAllow  = 0x1052 // compute_cannon_allowance: [si+2338] final for player si
	hookCannonHP     = 0x0aed // new game: cannon hit points [24CC] chosen
	hookGruntQuota   = 0x5a77 // 1P round planner end: grunt quota [67D8] set
	hookGruntLand    = 0x5ab5 // 1P: grunt landing countdown [6844] just reset
	hookDefeated     = 0x6ee4 // 1P defeat: [si+2343] continues used, 4 = game over
	hookWinCheck     = 0x6654 // 1P after battle: returns ax=1 if the region is conquered
	hookRoundCap     = 0x0c46 // 1P after rebuild: more than 12 rounds also ends the region
)

// Endless: the region is never conquered (6654, 0C46). Stock ships come from a
// per-region pool (578A: [67D4..67D6] by type, 10-35 ships) that runs dry after
// a few rounds, and a continue refills it. In Endless the planner (59AA) gets a
// fresh pool every round instead: its size grows with the rounds survived, the
// level (ship mix and toughness) every 4 rounds, and each continue used steps
// both back by 4 rounds' worth.
const (
	hookShipPlan     = 0x59aa // 1P round planner: releases ships from the pool
	endlessLevelStep = 4      // rounds survived per level
	endlessShipsMin  = 3      // ships per round at the start
	endlessShipsMax  = 12     // ... and at most
)

func (f *Features) installRules(m *Machine) {
	m.hooks[hookRebuildTimer] = func(m *Machine) { f.scaleTimer(m, f.S.Rules.BuildTime) }
	m.hooks[hookCannonTimer] = func(m *Machine) { f.scaleTimer(m, f.S.Rules.CannonTime) }
	m.hooks[hookBattleTimer] = func(m *Machine) { f.scaleTimer(m, f.S.Rules.BattleTime) }
	m.hooks[hookCannonAllow] = f.cannonAllow
	m.hooks[hookCannonHP] = f.cannonHP
	m.hooks[hookGruntQuota] = f.gruntQuota
	m.hooks[hookGruntLand] = f.gruntLand
	m.hooks[hookDefeated] = f.defeated
	m.hooks[hookWinCheck] = f.endlessWin
	m.hooks[hookShipPlan] = f.endlessPlan
	// Initials typed on the game's high-score screen (8FED: all three entered).
	m.hooks[0x8fed] = func(m *Machine) {
		p := uint32(m.dsw(0x7ac8))
		f.gameInitials = string([]byte{m.dsb(p), m.dsb(p + 1), m.dsb(p + 2)})
	}
	m.hooks[hookRoundCap] = func(m *Machine) {
		if f.endless() {
			m.cpu.ip = 0x0c4f
		}
	}
}

func (f *Features) endless() bool { return f.S.Rules.Endless || f.EndlessGame }

func (f *Features) endlessWin(m *Machine) {
	if !f.endless() {
		return
	}
	f.endlessRounds++
	f.endlessScore = f.score(m)
	m.cpu.r[rAX] = 0
	m.cpu.ip = m.cpu.pop()
}

// endlessPlan runs before the round planner: it sets the level and refills the
// ship pool for this round.
func (f *Features) endlessPlan(m *Machine) {
	if !f.endless() || m.dsb(0x24ca) != 1 {
		return
	}
	si := uint32(m.dsw(0x24c2))
	if f.endlessStart < 0 {
		f.endlessStart = int(m.dsb(si + 0x2342))
	}
	e := max(0, f.endlessRounds-endlessLevelStep*int(m.dsb(si+0x2343)))
	level := min(4, f.endlessStart+e/endlessLevelStep)
	m.dssb(si+0x2342, uint8(level))
	f.endlessLevel = level
	ships := min(endlessShipsMax, endlessShipsMin+(e+1)/2)

	// Ship mix of this level's stock pool (the 578A tables), scaled to `ships`.
	tab := uint32(0x644b)
	if m.dsw(0x6c71) != 0 {
		tab = 0x6496
	}
	list := uint32(m.dsw(tab + uint32(m.dsw(0x6c6f))*15 + uint32(level)*2))
	var c [3]int
	sum := 0
	for t := range c {
		c[t] = int(m.dsb(list + uint32(t)))
		sum += c[t]
	}
	if sum == 0 {
		c, sum = [3]int{1, 0, 0}, 1
	}
	got, big := 0, 0
	for t := range c {
		if c[t] > c[big] {
			big = t
		}
		c[t] = c[t] * ships / sum
		got += c[t]
	}
	c[big] += ships - got
	for t := range c {
		m.dssb(0x67d4+uint32(t), uint8(c[t]))
		m.dssb(0x67d1+uint32(t), 0) // drop ships still queued from last round
	}
	// Release exactly `ships`: the planner adds its own count to [67D7] and caps it
	// at the pool size.
	m.dssb(0x67d9, uint8(ships))
	m.dssb(0x67d7, uint8(ships))
}

// scaleTimer scales the seconds in AH before timer_set (1..99 so it fits the display).
func (f *Features) scaleTimer(m *Machine, pct int) {
	if pct == 100 {
		return
	}
	ax := m.cpu.r[rAX]
	sec := int(math.Round(float64(ax>>8) * float64(pct) / 100))
	sec = min(99, max(1, sec))
	m.cpu.r[rAX] = uint16(sec)<<8 | ax&0xff
}

func (f *Features) cannonAllow(m *Machine) {
	if n := f.S.Rules.ExtraCannons; n != 0 {
		si := uint32(m.cpu.r[rSI])
		v := int(m.dsb(si+0x2338)) + n
		m.dssb(si+0x2338, uint8(min(30, max(1, v))))
	}
}

// Cannon HP: a cannon dies after HP+1 hits.
func (f *Features) cannonHP(m *Machine) {
	if p := f.S.Rules.CannonHP; p != 100 {
		shots := float64(m.dsb(0x24cc)) + 1
		n := int(math.Round(shots * float64(p) / 100))
		m.dssb(0x24cc, uint8(min(60, max(1, n))-1))
	}
}

func (f *Features) gruntQuota(m *Machine) {
	q := int(m.dsb(0x67d8))
	switch f.S.Rules.Grunts {
	case "none":
		q = 0
	case "more":
		q *= 2
	case "invasion":
		q *= 4
	}
	m.dssb(0x67d8, uint8(min(90, q)))
}

func (f *Features) gruntLand(m *Machine) {
	d := 1
	switch f.S.Rules.Grunts {
	case "more":
		d = 2
	case "invasion":
		d = 4
	}
	if d > 1 {
		m.dssb(0x6844, uint8(max(1, int(m.dsb(0x6844))/d)))
	}
}

// score: the solo player's 32-bit score.
func (f *Features) score(m *Machine) uint32 {
	si := uint32(m.dsw(0x24c2))
	return uint32(m.dsw(si+0x232b)) | uint32(m.dsw(si+0x232d))<<16
}

func (f *Features) defeated(m *Machine) {
	if f.endless() && f.endlessRounds > 0 {
		f.endlessDefeats++
		f.endlessScore = f.score(m)
	}
	if f.S.Rules.Ironman {
		m.dssb(uint32(m.cpu.r[rSI])+0x2343, 4)
	}
}
