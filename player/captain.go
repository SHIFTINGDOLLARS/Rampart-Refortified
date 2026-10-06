package main

import (
	"io/fs"
	"strings"
)

// Yappy Captain: the arcade's voice lines, which the DOS version ships in
// SOUND.RSC but never plays (it only loads ready/aim/fire/cease/final and the
// effects). The captain addresses each player by colour: "<colour>beg" is
// "Blue commander," said up front, "<colour>end" the same tagged on the end.
//
//   build phase, no enclosed castle      alive (once per build phase, also in multiplayer)
//   build phase, castle enclosed (1P)     capture + end   ("now capture another castle")
//   wall piece doesn't fit               beg + rotate
//   cannon phase                         placecan
//   region conquered (1P)                welldone + end
//   game over (1P), the plank picture    agony2
//
// Lines are mixed on top of the game's sound (SoundBlaster.Voice), so they don't
// cut off the game's own samples.

const (
	hookCannonPhase  = 0x2ce0 // cannon_phase entry
	hookRebuildEnd   = 0x0c0e // play_round: rebuild_phase returned
	hookPieceBlocked = 0x20c6 // try_place_piece: a cell is not free
	hookConquered    = 0x0d36 // 1P "<colour> conquers" message
	hookWorldWon     = 0x0dbc // 1P world conquered
	hookPlankScreen  = 0x0da5 // 1P game over: the walk-the-plank picture was just loaded
	voiceRate        = 8000
	voiceGain        = 0.6
	rotateCooldown   = 10 * 70 // frames, per player
	buildCheckDelay  = 70      // frames after the build timer starts
)

var captainLog func(names []string, busy bool) // tests

var colourClip = [3]string{"blu", "red", "org"}

type Captain struct {
	clips map[string][]float32
	queue [][]float32 // utterances waiting
	cur   []float32
	pos   float64

	building    bool
	buildAt     int64
	checked     bool
	captureSaid bool
	hadCastle   [3]bool
	lastRotate  [3]int64
	enabled     func() bool
}

func loadVoices() map[string][]float32 {
	out := map[string][]float32{}
	d, err := fs.ReadFile(gameFiles, "game/SOUND.RSC")
	if err != nil {
		return out
	}
	for _, name := range []string{"alive", "capture", "rotate", "placecan", "welldone",
		"blubeg", "redbeg", "orgbeg", "bluend", "redend", "orgend", "agony2"} {
		b := rscEntry(d, name)
		if len(b) < 26 || !strings.HasPrefix(string(b), "Creative Voice File") {
			continue
		}
		q := int(b[20]) | int(b[21])<<8
		var pcm []float32
		for q+4 <= len(b) && b[q] != 0 {
			t := b[q]
			ln := int(b[q+1]) | int(b[q+2])<<8 | int(b[q+3])<<16
			body := b[q+4 : min(len(b), q+4+ln)]
			switch {
			case t == 1 && len(body) > 2:
				body = body[2:]
				fallthrough
			case t == 2:
				for _, v := range body {
					pcm = append(pcm, (float32(v)-128)/128)
				}
			}
			q += 4 + ln
		}
		out[name] = pcm
	}
	return out
}

func NewCaptain(enabled func() bool) *Captain {
	return &Captain{clips: loadVoices(), enabled: enabled}
}

// say queues an utterance made of clips (with a short pause between them). If
// the captain is already busy with two lines, it is dropped.
func (c *Captain) say(names ...string) {
	if c == nil || !c.enabled() {
		return
	}
	var u []float32
	for i, n := range names {
		if i > 0 {
			u = append(u, make([]float32, voiceRate/12)...)
		}
		u = append(u, c.clips[n]...)
	}
	if len(u) == 0 {
		return
	}
	if captainLog != nil {
		captainLog(names, c.cur != nil)
	}
	if c.cur == nil {
		c.cur, c.pos = u, 0
	} else if len(c.queue) < 2 {
		c.queue = append(c.queue, u)
	}
}

// Sample returns the next output sample (at SampleRate).
func (c *Captain) Sample() float64 {
	if c == nil || c.cur == nil {
		return 0
	}
	i := int(c.pos)
	fr := c.pos - float64(i)
	a := c.cur[i]
	b := a
	if i+1 < len(c.cur) {
		b = c.cur[i+1]
	}
	c.pos += voiceRate / float64(SampleRate)
	if int(c.pos) >= len(c.cur) {
		c.cur, c.pos = nil, 0
		if len(c.queue) > 0 {
			c.cur, c.queue = c.queue[0], c.queue[1:]
		}
	}
	return (float64(a) + (float64(b)-float64(a))*fr) * voiceGain
}

func (c *Captain) busy() bool { return c.cur != nil }

// ------------------------------------------------------------ game hooks

func chainHook(m *Machine, ip uint16, fn func(m *Machine)) {
	prev := m.hooks[ip]
	m.hooks[ip] = func(m *Machine) {
		fn(m)
		if prev != nil {
			prev(m)
		}
	}
}

// colourOf returns the colour (0 blue, 1 red, 2 orange) the player in struct
// si is shown as.
func (f *Features) colourOf(m *Machine, si uint32) int {
	if f.solo {
		return max(0, f.soloCol)
	}
	return int(m.dsw(si+0x231e) & 3)
}

// enclosedCastles counts castles enclosed by the player with land code
// `code` (colour+1): castle list DS:50D6 (6 bytes: flags 80 = used, x*8, y*8;
// the grid cell is x/8+1, y/8+1), and in the type grid DS:35BB an enclosed
// cell carries its owner's code in bits 6-7.
func enclosedCastles(m *Machine, code int) int {
	n := 0
	for o := uint32(0); o < 6*64 && m.dsb(0x50d6+o)&0x80 != 0; o += 6 {
		x := int(m.dsw(0x50d8+o))/8 + 1
		y := int(m.dsw(0x50da+o))/8 + 1
		if x < 42 && y < 27 && int(m.dsb(0x35bb+uint32(y*42+x))>>6) == code {
			n++
		}
	}
	return n
}

func (f *Features) players(m *Machine) []uint32 {
	var ps []uint32
	for si := uint32(0); si <= 0xd2; si += 0x69 {
		if m.dsw(si+0x231e)&0x8000 != 0 {
			ps = append(ps, si)
		}
	}
	return ps
}

func (f *Features) installCaptain(m *Machine) {
	c := f.captain
	chainHook(m, hookCannonPhase, func(m *Machine) { c.say("placecan") })
	chainHook(m, hookRebuildTimer, func(m *Machine) {
		c.building, c.buildAt, c.checked, c.captureSaid = true, m.Frame, false, false
	})
	chainHook(m, hookRebuildEnd, func(m *Machine) { c.building = false })
	chainHook(m, hookPieceBlocked, func(m *Machine) {
		si := uint32(m.cpu.r[rSI])
		p := int(si / 0x69)
		if p > 2 || isBotSeat(m, si) || m.Frame-c.lastRotate[p] < rotateCooldown || c.busy() {
			return
		}
		c.lastRotate[p] = m.Frame
		c.say(colourClip[f.colourOf(m, si)]+"beg", "rotate")
	})
	won := func(m *Machine) {
		c.say("welldone", colourClip[f.colourOf(m, uint32(m.dsw(0x24c2)))]+"end")
	}
	chainHook(m, hookConquered, won)
	chainHook(m, hookPlankScreen, func(m *Machine) {
		c.queue, c.cur = nil, nil
		c.say("agony2")
	})
	chainHook(m, hookWorldWon, won)
	m.FrameHooks = append(m.FrameHooks, func(m *Machine) {
		if !c.building || m.Frame < c.buildAt+buildCheckDelay {
			return
		}
		solo := m.dsb(0x24ca) == 1
		mpAlive := false
		for _, si := range f.players(m) {
			p := int(si / 0x69)
			if p > 2 || isBotSeat(m, si) {
				continue
			}
			has := enclosedCastles(m, int(m.dsw(si+0x231e)&3)+1) > 0
			col := colourClip[f.colourOf(m, si)]
			switch {
			case !c.checked && !has:
				if solo {
					c.say("alive")
				} else if !mpAlive { // multiplayer: one line for everyone
					mpAlive = true
					c.say("alive")
				}
			case solo && has && !c.captureSaid:
				// once per build phase: grunts inside a castle make its enclosure
				// flicker, which must not repeat the line
				c.captureSaid = true
				c.say("capture", col+"end")
			}
			c.hadCastle[p] = has
		}
		c.checked = true
	})
}
