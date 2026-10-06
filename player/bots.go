package main

import (
	"math"
	"math/rand"
)

// Computer players. On the join screen, keys 1/2/3 put a bot in the Blue / Red /
// Orange seat (press again to take it out; a human joining that colour takes
// the seat over). A bot is a controller id of its own (botCtl0 + seat), so the
// game treats it like any player; we answer the game's controller reads for it:
//
//	0x7870 read_controller(al = id) -> ax buttons (1 fire, 2 rotate), cx/dx moves
//	0x7AD6 read_aim(al = id)        -> the same, in pixels, for the battle cursor
//
// The caller's return address tells us which phase is asking.

const (
	botCtl0       = 8
	hookReadCtl   = 0x7870
	hookReadAim   = 0x7ad6
	hookJoinPoll  = 0x1153 // join screen loop, controllers just polled
	hookNextPiece = 0x560e
	hookPlayRound = 0x0b55 // play_round entry (a game is on: pad Start = pause) // deal a wall piece: al = piece, si = player
	joinLoop      = 0x1130
	hookJoinDone  = 0x11fc // join countdown finished, [24CA] = players
	joinTakeSlot  = 0x11dd // si = slot, bl = controller
	joinDrawSlot  = 0x11ee // si = slot: draw it as joined, back to the loop
	drawJoinBox   = 0xc75b // ax = slot (|0x100 joined), bx = box table

	retRebuild = 0x1729
	retCannon  = 0x2e6c
	retCastle  = 0x4ae8
	retAim     = 0x4643
	retInitial = 0x8ef3
	retAnyKey  = 0x786d // 7864 read_player_controller: MP "continue" wait (70FB), 1P continue (6F3B)
)

var botLog func(format string, a ...any) // tests

func isBotCtl(c uint8) bool { return c >= botCtl0 && c < botCtl0+3 }

type botSkill struct {
	move, rotate, settle int // frames between cursor steps / rotations / before placing
	aimSpeed             int // battle cursor pixels per frame
	aimErr               int // pixels
	fireGap              int // frames between shots
	think                int // frames before acting in a new phase
	reach                int // most new wall cells it'll plan to take another castle
}

var botSkills = []botSkill{
	{move: 6, rotate: 10, settle: 12, aimSpeed: 2, aimErr: 7, fireGap: 45, think: 90, reach: 30}, // easy
	{move: 3, rotate: 6, settle: 5, aimSpeed: 3, aimErr: 3, fireGap: 22, think: 45, reach: 40},   // normal
	{move: 2, rotate: 3, settle: 2, aimSpeed: 5, aimErr: 0, fireGap: 10, think: 20, reach: 52},   // hard
}

var botSkillNames = []string{"EASY", "NORMAL", "HARD"}

type Bot struct {
	seat  int
	phase int // return address of the last read
	next  int64

	// rebuild / cannon placement
	piece      int
	have       bool
	tx, ty, tr int
	toTarget   []int // BFS distance to the target cursor cell
	want       int   // cannon phase: item to place (0 cannon, 1 super cannon, 2 balloon)
	rotTries   int
	superDone  bool // placed a super cannon this cannon phase

	// battle
	ax, ay    int // aim point in pixels
	aimSet    bool
	shots     int
	kind      int // 0 wall, 1 cannon
	cell      int
	cannonOff uint32
	recent    map[int]int64
}

type Bots struct {
	f    *Features
	bots [3]*Bot
}

func (f *Features) skill() botSkill { return botSkills[min(2, max(0, f.S.BotSkill))] }

func (f *Features) installBots(m *Machine) {
	if f.bots == nil {
		f.bots = &Bots{f: f}
	}
	bs := f.bots
	for i := range bs.bots {
		bs.bots[i] = &Bot{seat: i, recent: map[int]int64{}}
	}
	read := func(aim bool) func(m *Machine) {
		return func(m *Machine) {
			id := uint8(m.cpu.r[rAX])
			var ax uint16
			var cx, dx int
			switch {
			case isBotCtl(id):
				ret := m.rw(m.cpu.lin(sSS, m.cpu.r[rSP]))
				ax, cx, dx = bs.bots[id-botCtl0].read(f, m, ret)
			case isPadCtl(id) && f.pads != nil:
				ax, cx, dx = f.pads.read(m, id, aim, f.S.PadSpeed)
			default:
				return
			}
			m.cpu.r[rAX], m.cpu.r[rCX], m.cpu.r[rDX] = ax, uint16(int16(cx)), uint16(int16(dx))
			m.cpu.ip = m.cpu.pop()
		}
	}
	m.hooks[hookReadCtl] = read(false)
	m.hooks[hookReadAim] = read(true)
	chainHook(m, hookPlayRound, func(m *Machine) {
		if f.pads != nil {
			f.pads.inGame = true
		}
	})
	m.hooks[hookJoinPoll] = f.joinBotKeys
	m.hooks[hookNextPiece] = f.nextPiece
	// Join countdown over: a bot alone would play the single-player campaign,
	// which has menus it can't use. Drop it (no one joined: back to the menu).
	chainHook(m, hookRebuildEnd, func(m *Machine) {
		if botLog == nil {
			return
		}
		b := readBoard(m)
		for _, si := range f.players(m) {
			code := int(m.dsw(si+0x231e)&3) + 1
			n := 0
			for _, c := range b.castles {
				if b.enclosedBy(c, code) {
					n++
				}
			}
			botLog("[rebuilt f%d] player %d bot=%v enclosed=%d stale=%d lives=%d score=%d cannons=%d", m.Frame, code, isBotSeat(m, si), n, staleMarks, m.dsb(si+0x2343),
				uint32(m.dsw(si+0x232b))|uint32(m.dsw(si+0x232d))<<16, countCannons(m, code-1))
		}
	})
	m.hooks[hookJoinDone] = func(m *Machine) {
		if m.dsb(0x24ca) != 1 {
			return
		}
		for k := uint32(0); k < 3; k++ {
			if isBotCtl(m.dsb(0x25fe + k)) {
				m.dssb(0x25fe+k, 0xff)
				m.dssb(0x24ca, 0)
			}
		}
	}
}

// isBotSeat: is the player struct at si played by a bot?
func isBotSeat(m *Machine, si uint32) bool { return isBotCtl(m.dsb(si + 0x2321)) }

// Join screen: 1/2/3 toggle a bot in seat 0/1/2.
func (f *Features) joinBotKeys(m *Machine) {
	f.joinFrame = m.Frame
	for k := 0; k < 3; k++ {
		a := uint32(0x2151 + 2 + k) // scancodes 02..04 = 1..3
		if m.dsb(a)&0x40 == 0 {
			continue
		}
		m.dssb(a, m.dsb(a)&^0x40)
		slot := m.dsb(0x25fe + uint32(k))
		switch {
		case slot == 0xff:
			m.cpu.r[rSI] = uint16(k)
			m.cpu.r[rBX] = botCtl0 + uint16(k)
			m.cpu.ip = joinTakeSlot
		case isBotCtl(slot):
			m.dssb(0x25fe+uint32(k), 0xff)
			m.dssb(0x24ca, m.dsb(0x24ca)-1)
			m.cpu.push(joinLoop)
			m.cpu.r[rAX] = uint16(k)
			m.cpu.r[rBX] = 0x25cf
			m.cpu.ip = drawJoinBox
		default:
			continue // a human has it
		}
		return
	}
}

// Smart Pieces: replace the dealt piece with one that repairs the walls.
func (f *Features) nextPiece(m *Machine) {
	mode := f.S.Rules.SmartPieces
	if mode == 0 || (m.dsb(0x24cb) == 1 && !f.S.Rules.SmartSolo) {
		return
	}
	if mode == 1 && rand.Intn(2) == 0 {
		return
	}
	loadShapes(m)
	si := uint32(m.cpu.r[rSI])
	code := int(m.dsw(si+0x231e)&3) + 1
	if p := readBoard(m).smartPiece(code); p >= 0 {
		if botLog != nil {
			botLog("[smart f%d] player %d: %d -> %d", m.Frame, code, m.cpu.r[rAX]&0xff, p)
		}
		m.cpu.r[rAX] = m.cpu.r[rAX]&0xff00 | uint16(p)
	}
}

// ------------------------------------------------------------------ bot brain

func (bt *Bot) read(f *Features, m *Machine, ret uint16) (ax uint16, cx, dx int) {
	si := uint32(0x69 * bt.seat)
	if ret != uint16(bt.phase) {
		bt.phase = int(ret)
		bt.have, bt.aimSet, bt.superDone = false, false, false
		bt.next = m.Frame + int64(f.skill().think/2+rand.Intn(f.skill().think+1))
	}
	if m.Frame < bt.next {
		return 0, 0, 0
	}
	switch ret {
	case retRebuild:
		return bt.rebuild(f, m, si)
	case retCannon:
		return bt.cannons(f, m, si)
	case retCastle:
		if m.dsb(si+0x2337) != 0 {
			return 0, 0, 0
		}
		if rand.Intn(3) == 0 { // look around a little
			bt.next = m.Frame + 20 + int64(rand.Intn(30))
			return 0, rand.Intn(3) - 1, rand.Intn(3) - 1
		}
		bt.next = m.Frame + 30
		return 1, 0, 0
	case retAim:
		return bt.battle(f, m, si)
	case retAnyKey: // a defeated bot takes its continue right away instead of letting the 15 s run out
		bt.next = m.Frame + 40
		return 1, 0, 0
	case retInitial:
		bt.next = m.Frame + 6
		pos := int(m.dsw(0x7ace))
		want := "BOT"[min(pos, 2)]
		cur := m.dsb(0x7acc)
		switch { // 8EF3: a negative move steps the letter up, positive down
		case cur < want:
			return 0, -1, 0
		case cur > want:
			return 0, 1, 0
		}
		return 1, 0, 0
	}
	return 0, 0, 0
}

// step moves the cursor (x,y) one cell toward the target along toTarget.
func (bt *Bot) step(b *board, x, y int) (cx, dx int) {
	here := bt.toTarget[y*gw+x]
	for _, d := range [4][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
		nx, ny := x+d[0], y+d[1]
		if b.cursorOK(nx, ny) {
			if v := bt.toTarget[ny*gw+nx]; v >= 0 && (here < 0 || v < here) {
				return d[0], d[1]
			}
		}
	}
	return 0, 0
}

func (bt *Bot) rebuild(f *Features, m *Machine, si uint32) (uint16, int, int) {
	sk := f.skill()
	p := int(m.dsb(si + 0x2349))
	rot := int(m.dsb(si+0x234a) & 3)
	x, y := int(int8(m.dsb(si+0x2365))), int(int8(m.dsb(si+0x2366)))
	if p >= 13 {
		bt.have = false
		return 0, 0, 0
	}
	loadShapes(m)
	b := readBoard(m)
	code := int(m.dsw(si+0x231e)&3) + 1
	if bt.have && bt.piece == p {
		if _, _, ok := b.placeScore(&plan{need: make([]bool, gn), avoid: make([]bool, gn)}, p, bt.tr, bt.tx, bt.ty); !ok {
			bt.have = false
		}
	}
	if !bt.have || bt.piece != p {
		pl := b.planReach(code, sk.reach)
		pc := b.bestPlacement(pl, []int{p}, b.cursorBFS(x, y))
		if pc.piece < 0 || (pc.cov == 0 && pl.nNeed == 0) {
			// Nothing to build (or nowhere to put it): hold the piece rather than
			// litter the land with walls. A piece that doesn't fit a needed gap is
			// still placed out of the way, so the next one comes.
			bt.next = m.Frame + 20
			return 0, 0, 0
		}
		if botLog != nil {
			botLog("[bot%d f%d] piece %d -> (%d,%d) r%d cov=%d need=%d secure=%v score=%.1f", bt.seat, m.Frame, p, pc.x, pc.y, pc.rot, pc.cov, pl.nNeed, pl.secure, pc.score)
		}
		bt.piece, bt.have = p, true
		bt.tx, bt.ty, bt.tr = pc.x, pc.y, pc.rot
		bt.toTarget = b.cursorBFS(pc.x, pc.y)
	}
	if rot != bt.tr {
		bt.next = m.Frame + int64(sk.rotate)
		return 2, 0, 0
	}
	if x != bt.tx || y != bt.ty {
		bt.next = m.Frame + int64(sk.move)
		cx, dx := bt.step(b, x, y)
		if cx == 0 && dx == 0 {
			bt.have = false
		}
		if x+cx == bt.tx && y+dx == bt.ty {
			bt.next += int64(sk.settle)
		}
		return 0, cx, dx
	}
	bt.have = false
	bt.next = m.Frame + int64(sk.move)
	return 1, 0, 0
}

// Cannon phase. Items (rotate cycles them, see Features.rotate): 0 cannon (2x2,
// costs 1), 1 super cannon (3x3, costs 4), 2 balloon (2x2, costs 3). Every cell
// must be enclosed empty land of the player (2FEA / 30C5 / 3263); the item goes
// at the cursor's top-left. The game sets the item back to 0 after a super cannon
// or balloon is placed.
func (bt *Bot) cannons(f *Features, m *Machine, si uint32) (uint16, int, int) {
	sk := f.skill()
	st := m.dsw(si + 0x2349)
	left := int(st & 0xff)
	if left == 0 || st&0x100 != 0 {
		return 0, 0, 0
	}
	item := int(m.dsw(si + 0x234b))
	x, y := int(int8(m.dsb(si+0x2365))), int(int8(m.dsb(si+0x2366)))
	b := readBoard(m)
	code := int(m.dsw(si+0x231e)&3) + 1
	yard := func(i int) bool { return b.free(i) && b.o[i]&0x20 != 0 && b.land(i) == code }
	fits := func(x, y, n int) bool {
		if x < 0 || y < 0 || x+n > gw || y+n > gh {
			return false
		}
		for dy := 0; dy < n; dy++ {
			for dx := 0; dx < n; dx++ {
				if !yard((y+dy)*gw + x + dx) {
					return false
				}
			}
		}
		return true
	}
	size := map[int]int{0: 2, 1: 3, 2: 2}
	if !bt.have || !fits(bt.tx, bt.ty, size[bt.want]) {
		bt.have = false
		reach := b.cursorBFS(x, y)
		var spots []int // reachable free 2x2 top-left cells
		var spots3 []int
		for i := 0; i < gn; i++ {
			if reach[i] < 0 {
				continue
			}
			if fits(i%gw, i/gw, 2) {
				spots = append(spots, i)
			}
			if fits(i%gw, i/gw, 3) {
				spots3 = append(spots3, i)
			}
		}
		if len(spots) == 0 {
			bt.next = m.Frame + 35
			return 0, 0, 0
		}
		room := packCount(spots)
		enh := m.dsw(0x6c71) != 0
		bt.want = 0
		switch {
		case f.S.BotSkill >= 2 && itemAllowed(f.S.SuperCannons, enh) && left >= 4 && len(spots3) > 0 &&
			(room < left || (left >= 5 && !bt.superDone)):
			bt.want = 1 // hard: one super cannon a round, or when cannons won't all fit
		case f.S.BotSkill >= 1 && itemAllowed(f.S.Balloons, enh) && left >= 3 && room < left:
			bt.want = 2 // no room for all the cannons: spend three on a balloon
		}
		// Enemy land's centre: cannons nearer the front get their shots there sooner.
		var ex, ey, en float64
		for i := 0; i < gn; i++ {
			if l := b.land(i); l != 0 && l != code {
				ex, ey, en = ex+float64(i%gw), ey+float64(i/gw), en+1
			}
		}
		cand, n := spots, 2
		if bt.want == 1 {
			cand, n = spots3, 3
		}
		bs := -1e9
		for _, i := range cand {
			// Pack tightly: keep as many 2x2 spots free as possible for the rest.
			s := 10 * float64(spotsLeft(spots, i, n))
			if en > 0 {
				dx, dy := float64(i%gw)-ex/en, float64(i/gw)-ey/en
				s -= math.Sqrt(dx*dx+dy*dy) * 0.3
			}
			s -= 0.02 * float64(reach[i])
			if s > bs {
				bs, bt.tx, bt.ty, bt.have = s, i%gw, i/gw, true
			}
		}
		bt.toTarget = b.cursorBFS(bt.tx, bt.ty)
		bt.rotTries = 0
	}
	if item != bt.want {
		if bt.rotTries++; bt.rotTries > 4 { // that item isn't available: plain cannons
			bt.want, bt.have = 0, false
			return 0, 0, 0
		}
		bt.next = m.Frame + int64(sk.rotate)
		return 2, 0, 0
	}
	if x != bt.tx || y != bt.ty {
		bt.next = m.Frame + int64(sk.move)
		cx, dx := bt.step(b, x, y)
		if cx == 0 && dx == 0 {
			bt.have = false
		}
		return 0, cx, dx
	}
	if bt.want == 1 {
		bt.superDone = true
	}
	if botLog != nil {
		botLog("[cannon%d f%d] item=%d left=%d at (%d,%d)", bt.seat, m.Frame, bt.want, left, x, y)
	}
	bt.have = false
	bt.next = m.Frame + int64(sk.settle+sk.move)
	return 1, 0, 0
}

// spotsLeft counts the 2x2 spots that stay free after an n x n item goes at cell i.
func spotsLeft(spots []int, i, n int) int {
	x0, y0 := i%gw, i/gw
	k := 0
	for _, j := range spots {
		x, y := j%gw, j/gw
		if x+1 < x0 || x > x0+n-1 || y+1 < y0 || y > y0+n-1 {
			k++
		}
	}
	return k
}

// packCount: how many cannons fit, placing each where it leaves the most room.
func packCount(spots []int) int {
	left := append([]int(nil), spots...)
	n := 0
	for len(left) > 0 {
		best, bi := -1, 0
		for _, i := range left {
			if k := spotsLeft(left, i, 2); k > best {
				best, bi = k, i
			}
		}
		var next []int
		x0, y0 := bi%gw, bi/gw
		for _, j := range left {
			x, y := j%gw, j/gw
			if x+1 < x0 || x > x0+1 || y+1 < y0 || y > y0+1 {
				next = append(next, j)
			}
		}
		left = next
		n++
	}
	return n
}

// ------------------------------------------------------------------ battle

func cellPixel(i int) (int, int) { return (i%gw-1)*8 + 4, (i/gw-1)*8 + 4 }

func (bt *Bot) readyCannon(m *Machine, col int) bool {
	for o := uint32(0); o < 90*16; o += 16 {
		fl := m.dsb(0x56ab + o)
		if fl&0x80 != 0 && int(fl&3) == col && fl&0x0c == 0 && fl&0x10 != 0 {
			return true
		}
	}
	return false
}

func (bt *Bot) pickTarget(f *Features, m *Machine, me uint32) bool {
	b := readBoard(m)
	var enemies []int
	for si := uint32(0); si <= 0xd2; si += 0x69 {
		if si != me && m.dsw(si+0x231e)&0x8000 != 0 {
			enemies = append(enemies, int(m.dsw(si+0x231e)&3))
		}
	}
	if len(enemies) == 0 {
		return false
	}
	e := enemies[rand.Intn(len(enemies))]
	code := e + 1
	skill := f.S.BotSkill
	// enemy cannons
	var cans []uint32
	for o := uint32(0); o < 90*16; o += 16 {
		fl := m.dsb(0x56ab + o)
		if fl&0x80 != 0 && fl&0x08 == 0 && int(fl&3) == e {
			cans = append(cans, o)
		}
	}
	// enemy walls: the rings around their enclosed castles, or any wall
	var ring, corners, walls []int
	if skill > 0 {
		for _, c := range b.castles {
			if b.land(c.y*gw+c.x) != code || !b.enclosedBy(c, code) {
				continue
			}
			l := b.loopAround(c, code, true)
			n := len(l.cells)
			for k, i := range l.cells {
				ring = append(ring, i)
				if n >= 3 {
					a, z := l.cells[(k+n-1)%n], l.cells[(k+1)%n]
					if (i-a == 1 || a-i == 1) != (z-i == 1 || i-z == 1) {
						corners = append(corners, i)
					}
				}
			}
		}
	}
	for i := 0; i < gn; i++ {
		if b.wall(i) && b.land(i) == code {
			walls = append(walls, i)
		}
	}
	fresh := func(l []int) []int {
		var out []int
		for _, i := range l {
			if t, ok := bt.recent[i]; !ok || m.Frame-t > 140 {
				out = append(out, i)
			}
		}
		return out
	}
	ring, corners, walls = fresh(ring), fresh(corners), fresh(walls)
	pickWall := func() bool {
		var l []int
		switch {
		case skill == 2 && len(corners) > 0 && rand.Intn(10) < 7:
			l = corners
		case skill >= 1 && len(ring) > 0:
			l = ring
		default:
			l = walls
		}
		if len(l) == 0 {
			return false
		}
		bt.kind, bt.cell, bt.shots = 0, l[rand.Intn(len(l))], 1
		bt.ax, bt.ay = cellPixel(bt.cell)
		return true
	}
	// Cannons: go for the most damaged one, and only commit a few shots at a
	// time. How often a bot shoots cannons at all depends on how many hits that
	// takes: with tough cannons (Cannon Kill 12, or the toughness rule) walls are
	// the better use of a shot.
	weakest := 1 << 30
	for _, o := range cans {
		weakest = min(weakest, int(int8(m.dsb(0x56ab+o+0x0e)))+1)
	}
	pickCannon := func() bool {
		var best []uint32
		for _, o := range cans {
			if int(int8(m.dsb(0x56ab+o+0x0e)))+1 == weakest {
				best = append(best, o)
			}
		}
		if len(best) == 0 {
			return false
		}
		o := best[rand.Intn(len(best))]
		bt.kind, bt.cannonOff = 1, o
		bt.shots = min(2, weakest)
		bt.ax, bt.ay = int(m.dsw(0x56ab+o+1))+8, int(m.dsw(0x56ab+o+3))+8
		return true
	}
	ok := false
	cannonOdds := 0.0
	if len(cans) > 0 {
		cannonOdds = 0.35 * min(1, 3/float64(weakest))
	}
	if rand.Float64() < cannonOdds {
		ok = pickCannon() || pickWall()
	} else {
		ok = pickWall() || pickCannon()
	}
	if ok {
		if er := f.skill().aimErr; er > 0 {
			bt.ax += rand.Intn(2*er+1) - er
			bt.ay += rand.Intn(2*er+1) - er
		}
		bt.ax, bt.ay = min(319, max(0, bt.ax)), min(199, max(0, bt.ay))
	}
	return ok
}

func (bt *Bot) targetAlive(m *Machine) bool {
	if bt.kind == 1 {
		fl := m.dsb(0x56ab + bt.cannonOff)
		return fl&0x80 != 0 && fl&0x08 == 0
	}
	return m.dsb(0x35bb+uint32(bt.cell))&0x1f == 1
}

func (bt *Bot) battle(f *Features, m *Machine, si uint32) (uint16, int, int) {
	sk := f.skill()
	if !bt.aimSet || bt.shots <= 0 || !bt.targetAlive(m) {
		bt.aimSet = bt.pickTarget(f, m, si)
		if !bt.aimSet {
			bt.next = m.Frame + 30
			return 0, 0, 0
		}
	}
	x, y := int(m.dsw(si+0x236b)), int(m.dsw(si+0x236d))
	cx := min(sk.aimSpeed, max(-sk.aimSpeed, bt.ax-x))
	dx := min(sk.aimSpeed, max(-sk.aimSpeed, bt.ay-y))
	if cx != 0 || dx != 0 {
		if abs(bt.ax-x) <= sk.aimSpeed && abs(bt.ay-y) <= sk.aimSpeed {
			bt.next = m.Frame + int64(sk.settle)
		}
		return 0, cx, dx
	}
	col := int(m.dsw(si+0x231e) & 3)
	if !bt.readyCannon(m, col) || m.dsb(0x6102) != 0 {
		return 0, 0, 0
	}
	bt.shots--
	if botLog != nil {
		botLog("[shot%d f%d] kind=%d", bt.seat, m.Frame, bt.kind)
	}
	if bt.kind == 0 {
		bt.recent[bt.cell] = m.Frame
	}
	bt.next = m.Frame + int64(sk.fireGap/2+rand.Intn(sk.fireGap+1))
	return 1, 0, 0
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// joinOverlay labels the bot seats on the join screen (the game's box says
// "PLEASE WAIT...", as for any joined player).
func (f *Features) joinOverlay(m *Machine, pix []uint32, font *Font) {
	if font == nil || m.Frame-f.joinFrame > 2 {
		return
	}
	for k := 0; k < 3; k++ {
		if !isBotCtl(m.dsb(0x25fe + uint32(k))) {
			continue
		}
		x0 := 21 + 104*k
		bg := pix[150*320+x0+1]
		fillRect(pix, x0, 135, 70, 36, bg)
		for _, l := range []struct {
			s string
			y int
		}{{"COMPUTER", 140}, {botSkillNames[min(2, max(0, f.S.BotSkill))], 156}} {
			font.Draw(pix, x0+(70-TextWidth(l.s))/2, l.y, l.s, 0xffffff)
		}
	}
}

func countCannons(m *Machine, col int) int {
	n := 0
	for o := uint32(0); o < 90*16; o += 16 {
		fl := m.dsb(0x56ab + o)
		if fl&0x80 != 0 && fl&0x08 == 0 && int(fl&3) == col {
			n++
		}
	}
	return n
}
