package main

import (
	"encoding/binary"
	"io/fs"
	"strings"
)

// Host-side game patches. Each hook runs when the game's code reaches a given
// IP in segment 0 (CS = 0x0810 at our load address). Addresses refer to the
// unpacked program (see NOTES.md).

const (
	gameCS       = 0x0810
	dsLin        = 0x165e0 // DS:0000 linear
	hookJoinSel  = 0x1199  // join screen: controller in BL pressed fire, choose its slot
	hookSetup1P  = 0x125b  // build the player struct for a one-player game
	hookSetup2P  = 0x12dc  // ... for a two-player game
	hookGameLoop = 0x0a20  // game_loop entry: back at the menus, between games
	hookRotate   = 0x2f8b  // cannon phase, rotate pressed: "Enhanced only" check
	rotateDone   = 0x2fca  // after the item cycle: redraw the cursor
	rotateBusy   = 0x2fcf  // can't switch now: error beep
	rotateNone   = 0x2fd5  // ret
	join1PTail   = 0x12bd
	join2PTail   = 0x138e
	joinTake     = 0x11dd
	joinAgain    = 0x11ca
	joinReject   = 0x11c4
)

type Features struct {
	S       *Settings
	landMap [4]uint8 // PFM land code -> owner code, set when players are assigned
	soloCol int      // colour shown for the solo player (-1: not in a solo game, or Blue)
	solo    bool     // a single-player game is running
	m       *Machine

	CastleColor [3]int // colour preset per side (colorPresets index), session only
	captain     *Captain
	bots        *Bots
	pads        *Pads
	joinFrame   int64 // last frame the join screen polled its controllers

	gbPlacing   bool // grunt buster: the cannon being created is one
	gbQueue     [][2]int
	gbCannon    uint16
	gbAX        uint16
	gbSprites   map[[3]int]*gbSprite
	gbTint      []int // battle-view buster pixels to darken this frame
	EndlessGame bool  // endless chosen on the island screen (this game only)

	endlessRegion   int     // island region of the ENDLESS box (0: none)
	endlessBoxState int     // 1: drawing it, 2: drawn
	savedBoxPtr     uint16  // DS:6921 while our box is drawn
	endlessRounds   int     // Endless: rounds survived this game
	endlessStart    int     // Endless: level at the first round (-1: not yet)
	endlessLevel    int     // Endless: ship level of the latest round
	endlessDefeats  int     // Endless: times defeated (the last one ends the game)
	endlessScore    uint32  // Endless: score at the latest check
	gameInitials    string  // typed on the game's high-score screen this game
	pendingRun      *Record // Endless game just ended: show the records
	OnConfig        func()  // called after the game saves RAMPART.CFG

	// Player colour blocks of the multiplayer palette, [block][colour][entry]:
	// block 0 = walls/territory/cursors (DAC 96, 112, 128), block 1 = castle
	// keeps (DAC 208, 224, 240). Matches the per-colour bases at DS:477B/477E.
	playerPal [2][3][16][3]float64
	firePal   [2][16][3]float64 // single-player palette at Red's blocks (fire colours)
}

func NewFeatures(s *Settings) *Features {
	f := &Features{S: s, soloCol: -1, pads: &Pads{}}
	f.landMap = [4]uint8{0, 1, 2, 3}
	if rsc, err := fs.ReadFile(gameFiles, "game/M1PPF.RSC"); err == nil {
		if p := rscEntry(rsc, "PALETTE"); len(p) >= 768 {
			for b, base := range palBlocks {
				for k := 0; k < 16; k++ {
					for ch := 0; ch < 3; ch++ {
						f.firePal[b][k][ch] = float64(p[3*(base+16+k)+ch])
					}
				}
			}
		}
	}
	if rsc, err := fs.ReadFile(gameFiles, "game/M2PPF.RSC"); err == nil {
		if p := rscEntry(rsc, "PALETTE"); len(p) >= 768 {
			for b, base := range palBlocks {
				for c := 0; c < 3; c++ {
					for k := 0; k < 16; k++ {
						for ch := 0; ch < 3; ch++ {
							f.playerPal[b][c][k][ch] = float64(p[3*(base+16*c+k)+ch])
						}
					}
				}
			}
		}
	}
	return f
}

func (f *Features) Install(m *Machine) {
	m.hooks[hookJoinSel] = f.joinSelect
	m.hooks[hookSetup1P] = f.setup1P
	m.hooks[hookSetup2P] = f.setup2P
	m.hooks[hookGameLoop] = f.gameLoop
	m.hooks[hookRotate] = f.rotate
	f.installEndlessBox(m)
	m.fs.Transform = f.transform
	m.fs.OnWrite = f.onWrite
	m.fs.MemOnly["RAMPART.CFG"] = true
	m.PalFix = f.palFix
	f.m = m
	f.installRules(m)
	if f.captain == nil {
		f.captain = NewCaptain(func() bool { return f.S.YappyCaptain })
	}
	f.captain.building = false
	f.captain.queue, f.captain.cur = nil, nil
	f.installCaptain(m)
	f.installBots(m)
	f.installGruntBuster(m)
	f.gbQueue = nil
	m.IdxFix = func(idx []uint8) { f.gbOverlay(m, idx) }
	m.RGBFix = f.gbTintFrame
	m.RGBFix = f.gbTintFrame
}

func (m *Machine) dsb(o uint32) uint8      { return m.mem[dsLin+o] }
func (m *Machine) dssb(o uint32, v uint8)  { m.mem[dsLin+o] = v }
func (m *Machine) dssw(o uint32, v uint16) { m.mem[dsLin+o], m.mem[dsLin+o+1] = byte(v), byte(v>>8) }
func (m *Machine) dsw(o uint32) uint16     { return uint16(m.mem[dsLin+o]) | uint16(m.mem[dsLin+o+1])<<8 }

// Join screen: each controller takes the slot of its own color (instead of the
// next free one), so colors follow controls. Unmapped controllers keep join order.
func (f *Features) joinSelect(m *Machine) {
	ctl := uint8(m.cpu.r[rBX])
	k := f.S.ColorFor(ctl)
	if k < 0 {
		return
	}
	slot := m.dsb(0x25fe + uint32(k))
	m.cpu.r[rSI] = uint16(k)
	switch {
	case isBotCtl(slot): // a human takes over the bot's seat
		m.dssb(0x25fe+uint32(k), ctl)
		m.cpu.ip = joinDrawSlot
	case slot == 0xff:
		m.cpu.ip = joinTake
	case slot == ctl:
		m.cpu.ip = joinAgain
	default:
		m.cpu.ip = joinReject // that color is taken by another controller
	}
}

func (m *Machine) initPlayer(color int, ctl uint8) uint32 {
	si := uint32(0x69 * color)
	m.dssw(si+0x231e, 0x9000|uint16(color))
	m.dssb(si+0x2320, uint8(color))
	m.dssb(si+0x2321, ctl)
	m.dssw(si+0x232b, 0)
	m.dssw(si+0x232d, 0)
	m.dssb(si+0x2342, 0)
	m.dssb(si+0x2343, 0)
	m.dssb(si+0x2344, 0)
	return si
}

func (m *Machine) resetRoundGlobals() {
	m.dssw(0x22dd, 0)
	m.dssb(0x22dc, 0)
	m.dssw(0x22df, 0)
	m.dssw(0x22d8, 0)
}

// One player. The game's single-player code is built around Blue: the ships
// take another colour's slot and the Red/Orange palette ranges hold fire and
// ship colours. So the game always runs as Blue, and the chosen colour is
// applied on screen only: the Blue palette range is shown in that colour and
// the colour-named boxes and shields ("BLUE ARMY CONQUERS") use its name.
func (f *Features) setup1P(m *Machine) {
	slot := 0
	for k := 0; k < 3; k++ {
		if m.dsb(0x25fe+uint32(k)) != 0xff {
			slot = k
			break
		}
	}
	c := f.S.SoloColor
	if c == ColAuto {
		c = slot
	}
	f.landMap = [4]uint8{0, 1, 2, 3}
	f.soloCol = -1
	f.solo = true
	if c != ColBlue {
		f.soloCol = c
	}
	f.setNames(m, c)
}

// Name prefixes of the colour boxes and shields: two tables of three
// 3-byte strings "BL", "RD", "OR".
var colorNameTables = []uint32{0xec11, 0x68cf}
var colorPrefixes = []string{"BL", "RD", "OR"}

// setNames makes colour 0 (Blue) use colour c's boxes and vice versa.
func (f *Features) setNames(m *Machine, c int) {
	for _, t := range colorNameTables {
		for k := 0; k < 3; k++ {
			n := k
			switch k {
			case 0:
				n = c
			case c:
				n = 0
			}
			m.dssb(t+uint32(3*k), colorPrefixes[n][0])
			m.dssb(t+uint32(3*k)+1, colorPrefixes[n][1])
		}
	}
}

// Back at the menus: undo the solo recolour.
func (f *Features) gameLoop(m *Machine) {
	if f.endlessRounds > 0 {
		f.pendingRun = &Record{Name: f.gameInitials, Rounds: f.endlessRounds, Score: f.endlessScore,
			Cont: max(0, f.endlessDefeats-1), Level: f.endlessLevel}
	}
	f.endlessDefeats, f.endlessScore, f.endlessLevel, f.gameInitials = 0, 0, 0, ""
	f.pads.inGame = false
	f.soloCol, f.solo = -1, false
	f.landMap = [4]uint8{0, 1, 2, 3}
	f.setNames(m, 0)
	f.EndlessGame = false
	f.endlessRounds, f.endlessStart = 0, -1
}

var palBlocks = []int{96, 208}

func recolorBlock(pal *[256]uint32, dac *[256][3]byte, base int, blue, want *[16][3]float64) {
	var sumB, sumC float64
	for k := 0; k < 16; k++ {
		for ch := 0; ch < 3; ch++ {
			sumB += blue[k][ch]
			sumC += float64(dac[base+k][ch])
		}
	}
	if sumB == 0 || sumC == 0 {
		return
	}
	scale := sumC / sumB
	fits := scale <= 1.02
	for k := 0; k < 16 && fits; k++ {
		for ch := 0; ch < 3; ch++ {
			if d := float64(dac[base+k][ch]) - blue[k][ch]*scale; d > 1.5 || d < -1.5 {
				fits = false
			}
		}
	}
	for k := 0; k < 16; k++ {
		var rgb [3]float64
		switch {
		case fits:
			for ch := 0; ch < 3; ch++ {
				rgb[ch] = want[k][ch] * scale
			}
		case [3]float64{float64(dac[base+k][0]), float64(dac[base+k][1]), float64(dac[base+k][2])} == blue[k]:
			rgb = want[k] // not a uniform fade: swap the entries that are exactly Blue's
		default:
			continue
		}
		v := func(x float64) uint32 { return uint32(min(63, max(0, x+0.5))) * 255 / 63 }
		pal[base+k] = v(rgb[0])<<16 | v(rgb[1])<<8 | v(rgb[2])
	}
}

// Two players: keep each player's own color (the original always uses Blue+Red).
func (f *Features) setup2P(m *Machine) {
	var cols []int
	var ctls []uint8
	for k := 0; k < 3; k++ {
		if v := m.dsb(0x25fe + uint32(k)); v != 0xff {
			cols, ctls = append(cols, k), append(ctls, v)
		}
	}
	f.soloCol, f.solo = -1, false
	f.setNames(m, 0)
	if len(cols) != 2 || (cols[0] == 0 && cols[1] == 1) {
		f.landMap = [4]uint8{0, 1, 2, 3}
		return // original code does exactly this case
	}
	m.dssw(0x67b9, 0x13b)
	for k := 0; k < 3; k++ {
		si := uint32(0x69 * k)
		m.dssw(si+0x231e, 0)
		for _, o := range []uint32{0x2323, 0x2325, 0x232b, 0x232d} {
			m.dssw(si+o, 0)
		}
	}
	first := m.initPlayer(cols[0], ctls[0])
	m.initPlayer(cols[1], ctls[1])
	m.dssw(0x24c2, uint16(first))
	m.dssw(0x24c6, 1)
	m.dssb(0x24cb, 0)
	m.resetRoundGlobals()
	f.landMap = [4]uint8{0, uint8(cols[0] + 1), uint8(cols[1] + 1), 3}
	m.cpu.ip = join2PTail
}

// Rotate during cannon placement cycles cannon -> super cannon -> balloon. The
// original only allows that in Enhanced games, with both extras. We redo the
// cycle (0x2F92..0x2FC8) host-side so each extra follows its own setting.
//
//	item [si+0x234B]: 0 cannon, 1 super cannon (needs 4 cannons left), 2 balloon (3)
//	[si+0x2349]: low byte = cannons left, bit 0x100 = placement locked
func (f *Features) rotate(m *Machine) {
	enh := m.dsw(0x6c71) != 0
	sc := itemAllowed(f.S.SuperCannons, enh)
	bl := itemAllowed(f.S.Balloons, enh)
	gb := gbAllowed(f.S.GruntBusters, m.dsb(0x24cb) == 1)
	if !sc && !bl && !gb {
		m.cpu.ip = rotateNone
		return
	}
	si := uint32(m.cpu.r[rSI])
	st := m.dsw(si + 0x2349)
	if st&0x100 != 0 {
		m.cpu.ip = rotateBusy
		return
	}
	left := st & 0xff
	ok := func(k uint16) bool {
		switch k {
		case 1:
			return sc && left >= 4
		case 2:
			return bl && left >= 3
		case 3:
			return gb && left >= 2
		}
		return true
	}
	k := m.dsw(si + 0x234b)
	for i := 0; i < 4; i++ {
		k = (k + 1) % 4
		if ok(k) {
			break
		}
	}
	m.dssw(si+0x234b, k)
	m.cpu.ip = rotateDone
}

// --------------------------------------------------------------- file transforms

var cfgKey = []byte("FE_FI_FO_FUM")

func cfgCrypt(b []byte) {
	for i := range b {
		b[i] ^= cfgKey[i%len(cfgKey)]
	}
}

func cfgChecksum(plain []byte) uint32 {
	var s uint32
	for i := 0; i+4 <= 0x60; i += 4 {
		s += binary.LittleEndian.Uint32(plain[i:])
	}
	return s
}

func (f *Features) transform(name string, data []byte) []byte {
	switch {
	case name == "RAMPART.CFG" && len(data) >= 0x66:
		p := append([]byte(nil), data[:0x62]...)
		cfgCrypt(p)
		f.applySettingsToCfg(p)
		out := append([]byte(nil), p...)
		cfgCrypt(out)
		out = binary.LittleEndian.AppendUint32(out, cfgChecksum(p))
		return out
	case strings.HasSuffix(name, "PPF.RSC") && f.landMap != [4]uint8{0, 1, 2, 3}:
		return f.remapRSC(data)
	}
	return data
}

const cfgWords = 0x62 / 2

// applySettingsToCfg builds the game's plaintext config from settings.json.
func (f *Features) applySettingsToCfg(p []byte) {
	put := func(o int, v uint16) { binary.LittleEndian.PutUint16(p[o:], v) }
	if len(f.S.GameCfg) == cfgWords {
		for i, w := range f.S.GameCfg {
			put(2*i, uint16(w))
		}
	}
	switch f.S.Sound {
	case "soundblaster":
		put(0, 3)
		put(2, 7)
		put(4, 1)
		put(6, 1)
	case "adlib":
		put(0, 2)
	case "speaker":
		put(0, 1)
	default:
		put(0, 0)
	}
	for i := 0; i < 7; i++ {
		put(0x32+2*i, uint16(f.S.Keys1[i]))
		put(0x4c+2*i, uint16(f.S.Keys2[i]))
	}
	o := f.S.GameOptions
	put(0x5a, uint16(o.Difficulty))
	put(0x5c, uint16(o.GameType))
	put(0x5e, uint16(o.CannonKill))
	put(0x60, uint16(o.BattleLength))
}

// The game saved its config: keep everything in settings.json (unencrypted).
func (f *Features) onWrite(name string, data []byte) {
	if name != "RAMPART.CFG" || len(data) < 0x62 {
		return
	}
	f.importCfg(data)
	f.S.Save()
	if f.OnConfig != nil {
		f.OnConfig()
	}
}

// importCfg reads an encrypted RAMPART.CFG into the settings.
func (f *Features) importCfg(data []byte) {
	p := append([]byte(nil), data[:0x62]...)
	cfgCrypt(p)
	w := func(o int) int16 { return int16(binary.LittleEndian.Uint16(p[o:])) }
	f.S.GameCfg = make([]int16, cfgWords)
	for i := range f.S.GameCfg {
		f.S.GameCfg[i] = w(2 * i)
	}
	for i := 0; i < 7; i++ {
		f.S.Keys1[i] = p[0x32+2*i]
		f.S.Keys2[i] = p[0x4c+2*i]
	}
	switch w(0) {
	case 0:
		f.S.Sound = "off"
	case 1:
		f.S.Sound = "speaker"
	case 2:
		f.S.Sound = "adlib"
	default:
		f.S.Sound = "soundblaster"
	}
	f.S.GameOptions = GameOptions{w(0x5a), w(0x5c), w(0x5e), w(0x60)}
}

// remapRSC rewrites PFM_* land codes 1..3 according to landMap.
func (f *Features) remapRSC(data []byte) []byte {
	out := append([]byte(nil), data...)
	n := int(binary.LittleEndian.Uint16(out))
	for i := 0; i < n; i++ {
		p := 0x14 + 20*i
		name := strings.TrimRight(string(out[p:p+12]), "\x00")
		if !strings.HasPrefix(name, "PFM_") {
			continue
		}
		off := int(binary.LittleEndian.Uint32(out[p+12:]))
		size := int(binary.LittleEndian.Uint32(out[p+16:]))
		for j := off; j < off+size && j < len(out); j++ {
			if v := out[j]; v >= 1 && v <= 3 {
				out[j] = f.landMap[v]
			}
		}
	}
	return out
}
