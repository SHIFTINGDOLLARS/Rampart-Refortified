package main

import (
	"fmt"
	"math"
)

// Grunt buster: a cannon-phase item (rotate cycles cannon -> super cannon ->
// balloon -> grunt buster). It costs 2 cannons, takes a normal 2x2 cannon spot
// and fires five balls in a + around the crosshair. Its balls only kill grunts:
// anywhere else they land with a puff and do nothing.
//
// Placement goes through the plain cannon code (2FEA -> create_cannon 333E); the
// new cannon is marked with bit 7 of its byte +0F (bit 0 is the balloon mark,
// the rest is unused). Firing: the game fires the next ready cannon in turn
// (next_ready_cannon 3580). We pick instead: a ready grunt buster if a grunt is
// under the +, otherwise the next ready ordinary cannon, so a buster never wastes
// a shot on a ship and never steals an ordinary shot.
//
// Looks: the plain cannon with its barrel black and its bands in the player's
// colour. It is drawn by the game as a plain cannon and recoloured in the
// rendered frame (Machine.IdxFix), in the building view (2x2 tiles, palette
// remapped into the player's colour block) and the battle view (2x3 pre-coloured
// tiles per facing).

const (
	gbMark          = 0x80
	hookPlaceCannon = 0x2fea // cannon placement check+place, si = player
	hookPlaceDone   = 0x2eda // back from the placement call (jnc 2EDF)
	hookPlaceCost   = 0x2ee7 // placed: charge by item ([si+234B])
	placeCostDone   = 0x2f44 // call be39 (cursor redraw); jmp 2f49
	hookCannonMade  = 0x336d // create_cannon: entry filled in, si = cannon
	hookCursorShape = 0xbe8d // be39: cursor shape [si+2363] set by item
	hookFireSelect  = 0x46aa // player_aim: call next_ready_cannon (3 bytes)
	fireSelected    = 0x46ad
	hookLaunch      = 0x46bb // call launch_cannonball (si cannon, cx,dx target)
	launchCannon    = 0x3467
	hookBallLands   = 0x364f // update_cannonballs: call ball_impact, bx = cannon
	ballLanded      = 0x3652
	impactGrunt     = 0x3704 // ball_impact's grunt branch (si = cell)
	impactPuff      = 0x3746 // ball_impact's "nothing here" puff
)

func gbAllowed(mode string, solo bool) bool {
	switch mode {
	case "everywhere":
		return true
	case "off":
		return false
	}
	return solo
}

func isGruntBuster(m *Machine, cannon uint32) bool {
	return m.dsb(cannon+0x0f)&gbMark != 0
}

// gruntNear reports whether a grunt stands on the cell under pixel (x,y) or
// one of its four neighbours.
func gruntNear(m *Machine, x, y int) bool {
	cx, cy := (x+8)>>3, (y+8)>>3
	for _, d := range [5][2]int{{0, 0}, {-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
		X, Y := cx+d[0], cy+d[1]
		if X < 0 || Y < 0 || X >= gw || Y >= gh {
			continue
		}
		if m.dsb(0x35bb+uint32(Y*gw+X))&0x1f == 0x0b {
			return true
		}
	}
	return false
}

// gbCacheTiles copies the cannon sprites while the game is drawing cannons:
// building view at the draw-object call (959C, ah = 2), battle view at A8F2.
func (f *Features) gbCacheTiles(m *Machine, battleView bool) {
	if f.gbSprites == nil {
		f.gbSprites = map[[3]int]*gbSprite{}
	}
	if !battleView {
		if f.gbSprites[[3]int{0, 0, 0}] != nil {
			return
		}
		seg := uint32(m.dsw(0x1061))
		for lvl := uint32(0); lvl < 3; lvl++ {
			t := func(o uint32) int { return int(m.dsw(0x8378 + 4*lvl + o)) }
			f.gbSprites[[3]int{0, int(lvl), 0}] = tilesSprite(m, seg, []int{t(0), t(2), t(0x10), t(0x12)}, 2, 2)
		}
		return
	}
	// A8F2 entry: al = facing, si = the cannon's cell. The battle tile bank holds
	// the look of the current firepower level and is reloaded when it changes
	// (a continue), so re-copy this sprite whenever its tiles differ.
	face := int(m.cpu.r[rAX] & 0xff)
	cell := uint32(m.cpu.r[rSI])
	if face > 7 || cell >= gn {
		return
	}
	col := int(m.dsb(0x3a29+cell)>>6) - 1
	if col < 0 || col > 2 {
		return
	}
	seg := uint32(m.dsw(0x89cc))
	b := 0x28 + col*0x36 + face*6
	key := [3]int{1, col, face}
	if sp := f.gbSprites[key]; sp != nil && sp.same(m, seg, b) {
		return
	}
	f.gbSprites[key] = tilesSprite(m, seg, []int{b, b + 1, b + 2, b + 3, b + 4, b + 5}, 2, 3)
}

// same reports whether a cached 2x3 battle sprite still matches tiles b..b+5.
func (s *gbSprite) same(m *Machine, seg uint32, b int) bool {
	for k := 0; k < 6; k++ {
		base := seg<<4 + uint32((b+k)&0x3ff)*64
		tx, ty := k%2, k/2
		for py := 0; py < 8; py++ {
			for px := 0; px < 8; px++ {
				if s.pix[(ty*8+py)*s.w+tx*8+px] != m.mem[base+uint32(py*8+px)] {
					return false
				}
			}
		}
	}
	return true
}

func (f *Features) installGruntBuster(m *Machine) {
	f.gbSprites = nil
	chainHook(m, 0x959c, func(m *Machine) {
		if m.cpu.r[rAX]>>8&0x7f == 2 && m.cpu.r[rAX]&1 != 0 {
			f.gbCacheTiles(m, false)
		}
	})
	chainHook(m, 0xa8f2, func(m *Machine) { f.gbCacheTiles(m, true) })
	m.hooks[hookPlaceCannon] = func(m *Machine) {
		si := uint32(m.cpu.r[rSI])
		f.gbPlacing = si <= 0xd2 && m.dsw(si+0x234b) == 3
	}
	m.hooks[hookPlaceDone] = func(m *Machine) { f.gbPlacing = false }
	m.hooks[hookCannonMade] = func(m *Machine) {
		c := uint32(m.cpu.r[rSI])
		v := m.dsb(c+0x0f) &^ gbMark
		if f.gbPlacing {
			if botLog != nil {
				botLog("[gb f%d] placed cannon %x", m.Frame, c)
			}
			v |= gbMark
			f.gbPlacing = false
		}
		m.dssb(c+0x0f, v)
	}
	m.hooks[hookPlaceCost] = func(m *Machine) {
		si := uint32(m.cpu.r[rSI])
		if m.dsw(si+0x234b) != 3 {
			return
		}
		m.dssw(si+0x234b, 0)
		m.dssw(si+0x2349, (m.dsw(si+0x2349)-2)|0x100)
		m.dssw(si+0x2333, 0x0f)
		m.cpu.ip = placeCostDone
	}
	m.hooks[hookCursorShape] = func(m *Machine) {
		si := uint32(m.cpu.r[rSI])
		if m.dsw(si+0x234b) == 3 {
			m.dssw(si+0x2363, 0x0201) // the cannon cursor, recoloured by the overlay
		}
	}

	// Fire: choose the cannon (replaces call next_ready_cannon).
	m.hooks[hookFireSelect] = func(m *Machine) {
		col := int(m.cpu.r[rAX] & 3)
		x, y := int(int16(m.cpu.r[rCX])), int(int16(m.cpu.r[rDX]))
		ptr := uint32(0x5f9b + 2*col)
		start := uint32(m.dsw(ptr))
		if start < 0x56ab || start >= 0x5c4b {
			start = 0x56ab
		}
		wantGB := gruntNear(m, x, y)
		pick := func(gb bool) uint32 {
			c := start
			for n := 0; n < 90; n++ {
				fl := m.dsb(c)
				if fl&0x80 != 0 && int(fl&3) == col && fl&0x0c == 0 && fl&0x10 != 0 && isGruntBuster(m, c) == gb {
					return c
				}
				if c += 16; c >= 0x5c4b {
					c = 0x56ab
				}
			}
			return 0
		}
		c := uint32(0)
		if wantGB {
			c = pick(true)
		}
		if c == 0 {
			c = pick(false)
		}
		if c == 0 {
			m.cpu.setf(fCF, true)
		} else {
			n := c + 16
			if n >= 0x5c4b {
				n = 0x56ab
			}
			m.dssw(ptr, uint16(n))
			m.cpu.r[rSI] = uint16(c)
			m.cpu.setf(fCF, false)
		}
		m.cpu.ip = fireSelected
	}

	// Launch: a buster fires five balls; re-enter launch_cannonball for each.
	m.hooks[hookLaunch] = func(m *Machine) {
		if len(f.gbQueue) == 0 {
			c := uint32(m.cpu.r[rSI])
			if !isGruntBuster(m, c) {
				return
			}
			x, y := int(int16(m.cpu.r[rCX])), int(int16(m.cpu.r[rDX]))
			f.gbQueue = [][2]int{{x, y}, {x - 8, y}, {x + 8, y}, {x, y - 8}, {x, y + 8}}
			f.gbCannon, f.gbAX = uint16(c), m.cpu.r[rAX]
			if botLog != nil {
				botLog("[gb f%d] fire at %d,%d", m.Frame, x, y)
			}
		}
		t := f.gbQueue[0]
		f.gbQueue = f.gbQueue[1:]
		m.cpu.r[rSI], m.cpu.r[rAX] = f.gbCannon, f.gbAX
		m.cpu.r[rCX] = uint16(int16(min(319, max(0, t[0]))))
		m.cpu.r[rDX] = uint16(int16(min(199, max(0, t[1]))))
		if len(f.gbQueue) > 0 {
			m.cpu.push(hookLaunch) // come back here for the next ball
			m.cpu.ip = launchCannon
		}
		// last ball: the original call runs and returns past it
	}

	// Landing: a buster's ball only does something to a grunt.
	m.hooks[hookBallLands] = func(m *Machine) {
		c := uint32(m.cpu.r[rBX])
		if c < 0x56ab || c >= 0x5c4b || !isGruntBuster(m, c) {
			return
		}
		x, y := int(int16(m.cpu.r[rCX])), int(int16(m.cpu.r[rDX]))
		cell := uint32(((y+8)>>3)*gw + (x+8)>>3)
		entry := uint16(impactPuff)
		if cell < gn && m.dsb(0x35bb+cell)&0x1f == 0x0b {
			entry = impactGrunt
		}
		// Enter ball_impact past its prologue: same pushes, same counter.
		m.cpu.push(ballLanded)
		for _, r := range []int{rBX, rCX, rDX, rSI, rDI} {
			m.cpu.push(m.cpu.r[r])
		}
		m.dssw(0x5fb1, m.dsw(0x5fb1)-1)
		if botLog != nil {
			botLog("[gb f%d] lands %d,%d grunt=%v", m.Frame, x, y, entry == impactGrunt)
		}
		m.cpu.r[rSI] = uint16(cell)
		m.cpu.ip = entry
	}
}

// ------------------------------------------------------------------ looks

// gbRecolor maps a cannon sprite's colour nibbles (low 4 bits within the
// player's 16-colour block) to the buster's: barrel black (its highlight dark
// grey), and black between barrel pixels (the bands, the muzzle) in the
// player's own colour.
func gbRecolor(tile []uint8, w, h int) []int8 {
	out := make([]int8, len(tile))
	barrel := func(v uint8) bool {
		switch v & 15 {
		case 4, 6, 8, 0xa, 0xb:
			return v != 0
		}
		return false
	}
	at := func(x, y int) uint8 {
		if x < 0 || y < 0 || x >= w || y >= h {
			return 0
		}
		return tile[y*w+x]
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := tile[y*w+x]
			out[y*w+x] = -1
			switch {
			case v == 0:
			case barrel(v) && v&15 == 4:
				out[y*w+x] = 0xd
			case barrel(v):
				out[y*w+x] = 2
			case v&15 == 2:
				between := func(dx, dy int) bool {
					a, b := false, false
					for k := 1; k <= 3; k++ {
						a = a || barrel(at(x-k*dx, y-k*dy))
						b = b || barrel(at(x+k*dx, y+k*dy))
					}
					return a && b
				}
				if between(1, 0) || between(0, 1) {
					out[y*w+x] = 5
				}
			}
		}
	}
	return out
}

type gbSprite struct {
	pix   []uint8 // w*h tile pixels as stored
	remap []int8  // new low nibble per pixel (-1: keep)
	w, h  int
}

// tilesSprite assembles w x h tiles (ids, row-major) from a tile segment.
func tilesSprite(m *Machine, seg uint32, ids []int, w, h int) *gbSprite {
	s := &gbSprite{pix: make([]uint8, w*8*h*8), w: w * 8, h: h * 8}
	for k, id := range ids {
		tx, ty := k%w, k/w
		base := seg<<4 + uint32(id&0x3ff)*64
		for py := 0; py < 8; py++ {
			for px := 0; px < 8; px++ {
				s.pix[(ty*8+py)*s.w+tx*8+px] = m.mem[base+uint32(py*8+px)]
			}
		}
	}
	s.remap = gbRecolor(s.pix, s.w, s.h)
	return s
}

// apply recolours the sprite at screen (x,y) where the frame still shows it.
// match: the share of opaque pixels that must agree (it's this sprite, here).
func (s *gbSprite) apply(idx []uint8, x, y int, block uint8, raw bool) bool {
	n, ok := 0, 0
	for i, v := range s.pix {
		if v == 0 {
			continue
		}
		X, Y := x+i%s.w, y+i/s.w
		if X < 0 || Y < 0 || X >= 320 || Y >= 200 {
			continue
		}
		if v&15 == 1 || v&15 == 2 {
			continue // floor pattern: it matches wherever there's floor
		}
		n++
		want := v
		if !raw {
			want = block | v&15
		}
		if idx[Y*320+X] == want {
			ok++
		}
	}
	if n == 0 || ok*10 < n*6 {
		return false
	}
	for i, v := range s.pix {
		r := s.remap[i]
		if v == 0 || r < 0 {
			continue
		}
		X, Y := x+i%s.w, y+i/s.w
		if X < 0 || Y < 0 || X >= 320 || Y >= 200 {
			continue
		}
		want := v
		if !raw {
			want = block | v&15
		}
		if idx[Y*320+X] == want {
			idx[Y*320+X] = block | uint8(r)
		}
	}
	return true
}

// gbOverlay recolours every grunt buster on screen (Machine.IdxFix).
func (f *Features) gbOverlay(m *Machine, idx []uint8) {
	f.gbTint = f.gbTint[:0]
	any := false
	for c := uint32(0x56ab); c < 0x5c4b; c += 16 {
		if fl := m.dsb(c); fl&0x80 != 0 && fl&0x08 == 0 && isGruntBuster(m, c) {
			any = true
			break
		}
	}
	cursorGB := -1
	for si := uint32(0); si <= 0xd2; si += 0x69 {
		if m.dsw(si+0x231e)&0x8000 != 0 && m.dsw(si+0x234b) == 3 {
			cursorGB = int(si)
		}
	}
	if !any && cursorGB < 0 {
		return
	}
	blockOf := func(col int) uint8 { return m.dsb(0x477b+uint32(col)) << 4 }
	// Sprites come from the cache only: it's filled while the game is drawing
	// cannons (gbCacheTiles), when the tile banks surely hold them. Between
	// phases the banks are reused for other graphics while the screen still
	// shows the old picture.
	buildApply := func(idx []uint8, x, y int, blk uint8) bool {
		for lvl := 0; lvl < 3; lvl++ {
			if sp := f.gbSprites[[3]int{0, lvl, 0}]; sp != nil && sp.apply(idx, x, y, blk, false) {
				return true
			}
		}
		return false
	}
	battle := func(col, face int) *gbSprite { return f.gbSprites[[3]int{1, col, face}] }
	for c := uint32(0x56ab); c < 0x5c4b; c += 16 {
		fl := m.dsb(c)
		if fl&0x80 == 0 || fl&0x08 != 0 || !isGruntBuster(m, c) {
			continue
		}
		col := int(fl & 3)
		x, y := int(m.dsw(c+1)), int(m.dsw(c+3))
		blk := blockOf(col)
		if gbDebug != nil {
			for lvl := 0; lvl < 3; lvl++ {
				if sp := f.gbSprites[[3]int{0, lvl, 0}]; sp != nil {
					gbDebug(fmt.Sprintf("buster %x at %d,%d level %d score %.2f", c, x, y, lvl, sp.scoreBlock(idx, x, y, blk)))
				}
			}
		}
		if buildApply(idx, x, y, blk) {
			continue
		}
		if face := int(m.dsb(c + 0x0d)); face < 8 {
			// A8F2 puts the 2x3 battle sprite on the cannon's cells and the row
			// above (tiles 0-1 one row up, 2-3 on the cannon's row, 4-5 below),
			// each drawn at its own map position: so exactly 8 px above the
			// cannon. (Searching for it instead matched identical neighbours.)
			if sp := battle(col, face); sp != nil && sp.score(idx, x, y-8) >= 0.6 {
				f.gbTint = sp.mask(idx, x, y-8, f.gbTint)
			}
		}
	}
	if cursorGB >= 0 {
		si := uint32(cursorGB)
		x := (int(m.dsb(si+0x2365)) - 1) * 8
		y := (int(m.dsb(si+0x2366)) - 1) * 8
		col := int(m.dsw(si+0x231e) & 3)
		// The placement cursor is its own sprite in the cursor colours (D0..DF;
		// D1..D4 cycle): show it inverted the same way, barrel black and bands
		// (D7, D8) in the player's colour.
		blk := blockOf(col)
		for py := 0; py < 16; py++ {
			for px := 0; px < 16; px++ {
				X, Y := x+px, y+py
				if X < 0 || Y < 0 || X >= 320 || Y >= 200 {
					continue
				}
				switch v := idx[Y*320+X]; v {
				case 0xd7, 0xd8:
					idx[Y*320+X] = blk | 5
				case 0xd5, 0xd6, 0xd9, 0xda, 0xdb:
					idx[Y*320+X] = blk | 2
				}
			}
		}
	}
}

// mask returns the screen positions where the frame still shows this sprite
// (raw tile colours) at (x,y), appended to out, if enough of it is there.
func (s *gbSprite) mask(idx []uint8, x, y int, out []int) []int {
	n, ok := 0, 0
	var at []int
	for i, v := range s.pix {
		if v == 0 {
			continue
		}
		X, Y := x+i%s.w, y+i/s.w
		if X < 0 || Y < 0 || X >= 320 || Y >= 200 {
			continue
		}
		// Only the cannon counts and gets darkened: its tiles also carry the
		// floor pattern around it, in the block's dark colour (1) and black (2).
		if v&15 == 1 || v&15 == 2 {
			continue
		}
		n++
		if idx[Y*320+X] == v {
			ok++
			at = append(at, Y*320+X)
		}
	}
	if n == 0 || ok*10 < n*6 {
		return out
	}
	return append(out, at...)
}

// gbTintFrame turns the battle-view busters silver (Machine.RGBFix): no
// saturation, lightness 0.35 + 0.75 x l, so they read the same in every colour.
func (f *Features) gbTintFrame(dst []uint32) {
	for _, p := range f.gbTint {
		c := dst[p]
		h, _, l := rgbToHSL(float64(c>>16&255)/255, float64(c>>8&255)/255, float64(c&255)/255)
		r, g, b := hslToRGB(h, 0, math.Min(1, 0.35+l*0.75)) // silver, whatever the player's colour
		dst[p] = uint32(r*255+0.5)<<16 | uint32(g*255+0.5)<<8 | uint32(b*255+0.5)
	}
}

// score: the share of the sprite's own (non-floor) pixels that the frame shows at (x,y).
func (s *gbSprite) score(idx []uint8, x, y int) float64 {
	n, ok := 0, 0
	for i, v := range s.pix {
		if v == 0 || v&15 == 1 || v&15 == 2 {
			continue
		}
		X, Y := x+i%s.w, y+i/s.w
		if X < 0 || Y < 0 || X >= 320 || Y >= 200 {
			continue
		}
		n++
		if idx[Y*320+X] == v {
			ok++
		}
	}
	if n < 8 {
		return 0
	}
	return float64(ok) / float64(n)
}

var gbDebug func(string) // tests

func (s *gbSprite) scoreBlock(idx []uint8, x, y int, block uint8) float64 {
	n, ok := 0, 0
	for i, v := range s.pix {
		if v == 0 || v&15 == 1 || v&15 == 2 {
			continue
		}
		X, Y := x+i%s.w, y+i/s.w
		if X < 0 || Y < 0 || X >= 320 || Y >= 200 {
			continue
		}
		n++
		if idx[Y*320+X] == block|v&15 {
			ok++
		}
	}
	if n == 0 {
		return 0
	}
	return float64(ok) / float64(n)
}
