package main

import (
	"fmt"
	"strings"
)

// Gamepads (XInput on Windows). Pad 1 and pad 2 play as the game's two joystick
// controllers (ids 5 and 6): the same controller-read hooks the bots use answer
// for them from the pad state, so the game's own joystick code (port 201h,
// calibration) is never involved.
//
//	A, right trigger, RB      fire
//	B, X, Y, left trigger, LB rotate (held: faster battle cursor, like the keyboard)
//	D-pad / left stick        move (cells: first step at once, then repeat; battle: analog)
//	Back (View)               F12 options menu
//	Start                     during a game: pause (the game's Pause key); otherwise F1
//
// In the F12 menu and the records screen the pad drives the menu keys.

const (
	padUp    = 0x0001
	padDown  = 0x0002
	padLeft  = 0x0004
	padRight = 0x0008
	padStart = 0x0010
	padBack  = 0x0020
	padLB    = 0x0100
	padRB    = 0x0200
	padA     = 0x1000
	padB     = 0x2000
	padX     = 0x4000
	padY     = 0x8000

	padDead      = 7849 // XInput's recommended left-stick dead zone
	padDigital   = 16000
	padTrigger   = 100
	padFirstRep  = 14 // frames before a held direction repeats
	padRepeat    = 4  // frames between repeats (half that at full tilt)
	padAimPixels = 4.0
)

// PadState is one controller's state as read from the system.
type PadState struct {
	Connected bool
	Buttons   uint16
	LX, LY    int16 // left stick, up = positive (XInput convention)
	LT, RT    uint8
}

func (s PadState) fire() bool {
	return s.Buttons&(padA|padRB) != 0 || s.RT > padTrigger
}

func (s PadState) rotate() bool {
	return s.Buttons&(padB|padX|padY|padLB) != 0 || s.LT > padTrigger
}

// dir returns the digital direction (-1/0/1 per axis; y down = positive).
func (s PadState) dir() (int, int, bool) {
	dx, dy, full := 0, 0, false
	if s.Buttons&padLeft != 0 || s.LX < -padDigital {
		dx = -1
	}
	if s.Buttons&padRight != 0 || s.LX > padDigital {
		dx = 1
	}
	if s.Buttons&padUp != 0 || s.LY > padDigital {
		dy = -1
	}
	if s.Buttons&padDown != 0 || s.LY < -padDigital {
		dy = 1
	}
	if abs(int(s.LX)) > 30000 || abs(int(s.LY)) > 30000 {
		full = true
	}
	return dx, dy, full
}

type pad struct {
	st, prev       PadState
	fireL, rotL    bool // latched presses, cleared when the game reads them
	cdx, cdy       int  // direction last seen by a cell read
	nextRep        int64
	accX, accY     float64
	menuDX, menuDY int
	menuRep        int64
}

type Pads struct {
	p      [2]pad
	tick   int64 // host frames (the menu pauses the game's frame counter)
	inGame bool  // between play_round and the menus (Start = pause)
}

func isPadCtl(c uint8) bool { return c == CtlJoy1 || c == CtlJoy2 }

// SetPad records pad i's state for this frame. Call once per frame, before Frame.
func (a *App) SetPad(i int, st PadState) {
	if i < 0 || i > 1 {
		return
	}
	ps := a.F.pads
	if i == 0 {
		ps.tick++
	}
	p := &ps.p[i]
	p.prev, p.st = p.st, st
	pressed := func(b uint16) bool { return st.Buttons&b != 0 && p.prev.Buttons&b == 0 }
	if !st.Connected {
		p.fireL, p.rotL = false, false
		return
	}
	if st.fire() && !p.prev.fire() {
		p.fireL = true
	}
	if st.rotate() && !p.prev.rotate() {
		p.rotL = true
	}
	tap := func(sc uint8) { a.Key(sc, true); a.Key(sc, false) }
	if a.Menu.open || a.Board.open {
		p.fireL, p.rotL = false, false
		switch {
		case pressed(padA):
			tap(0x1c)
		case pressed(padB), pressed(padStart):
			tap(0x01)
		case pressed(padBack):
			tap(0x58)
		}
		dx, dy, _ := st.dir()
		if dx != p.menuDX || dy != p.menuDY || ps.tick >= p.menuRep {
			if dx != p.menuDX || dy != p.menuDY {
				p.menuRep = ps.tick + 18
			} else {
				p.menuRep = ps.tick + 6
			}
			p.menuDX, p.menuDY = dx, dy
			switch {
			case dy < 0:
				tap(0x48)
			case dy > 0:
				tap(0x50)
			case dx < 0:
				tap(0x4b)
			case dx > 0:
				tap(0x4d)
			}
		}
		return
	}
	p.menuDX, p.menuDY = 0, 0
	switch {
	case pressed(padBack):
		tap(0x58)
	case pressed(padStart):
		if ps.inGame {
			tap(0x45) // the game's Pause key; a second press resumes
		} else {
			tap(0x3b) // F1: Declare War from the main menu
		}
	}
}

// padRead answers a controller read for pad id 5/6. aim: the battle crosshair
// read (pixels, and held-button bits 4/8); otherwise cells.
func (ps *Pads) read(m *Machine, id uint8, aim bool, speed int) (ax uint16, cx, dx int) {
	p := &ps.p[id-CtlJoy1]
	if !p.st.Connected {
		return 0, 0, 0
	}
	if p.fireL {
		ax |= 1
		p.fireL = false
	}
	if p.rotL {
		ax |= 2
		p.rotL = false
	}
	sp := float64(speed) / 100
	if aim {
		if p.st.fire() {
			ax |= 4
		}
		if p.st.rotate() {
			ax |= 8
		}
		vx, vy := 0.0, 0.0
		if v := int(p.st.LX); abs(v) > padDead {
			vx = float64(v-sign(v)*padDead) / float64(32767-padDead)
		}
		if v := -int(p.st.LY); abs(v) > padDead {
			vy = float64(v-sign(v)*padDead) / float64(32767-padDead)
		}
		if b := p.st.Buttons; b&(padLeft|padRight|padUp|padDown) != 0 {
			vx, vy = 0, 0
			if b&padLeft != 0 {
				vx = -0.5
			}
			if b&padRight != 0 {
				vx = 0.5
			}
			if b&padUp != 0 {
				vy = -0.5
			}
			if b&padDown != 0 {
				vy = 0.5
			}
		}
		// Squared response: fine control near the centre, full speed at the edge.
		p.accX += vx * abs64(vx) * padAimPixels * sp
		p.accY += vy * abs64(vy) * padAimPixels * sp
		cx, dx = int(p.accX), int(p.accY)
		p.accX -= float64(cx)
		p.accY -= float64(dx)
		return ax, cx, dx
	}
	ddx, ddy, full := p.st.dir()
	switch {
	case ddx == 0 && ddy == 0:
		p.cdx, p.cdy = 0, 0
		return ax, 0, 0
	case ddx != p.cdx || ddy != p.cdy:
		p.cdx, p.cdy = ddx, ddy
		p.nextRep = m.Frame + int64(float64(padFirstRep)/sp)
		return ax, ddx, ddy
	case m.Frame >= p.nextRep:
		rep := float64(padRepeat) / sp
		if full {
			rep /= 2
		}
		p.nextRep = m.Frame + int64(max(1, rep))
		return ax, ddx, ddy
	}
	return ax, 0, 0
}

func sign(v int) int {
	if v < 0 {
		return -1
	}
	return 1
}

func abs64(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// padSummary says which pads are connected, for the Controls page.
func (a *App) padSummary() string {
	var on []string
	for i, p := range a.F.pads.p {
		if p.st.Connected {
			on = append(on, fmt.Sprintf("%d", i+1))
		}
	}
	if len(on) == 0 {
		return "NONE CONNECTED"
	}
	return strings.Join(on, " AND ") + " CONNECTED"
}
