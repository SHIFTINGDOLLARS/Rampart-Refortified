package main

import "math/rand"

// ENDLESS box on the single-player island screen (6701).
//
// The stock screen puts RECRUITBOX on region 0 and VETERANBOX on region 1 or 2
// (random). Each box is a selectable "castle" for home_castle_select; the
// chosen region becomes the map. We add a third box on whichever of regions 1/2
// the Veteran box didn't take. Its tiles are drawn here (title in the boxes'
// gothic style, subtitle in their small font) and written into free slots of
// the MTRNPF tile bank, which holds 782 tiles of a 1024-tile segment.
// Picking it starts a Recruit-level game on that region with Endless on.

const (
	hookVetRegion   = 0x681c // first island screen: bx = 4*Veteran region (1 or 2)
	hookIslandBoxes = 0x6899 // boxes placed, home_castle_select is next
	hookIslandPick  = 0x68e7 // bx = chosen region (6: none)
	hookFirstCastle = 0x4d58 // castle select: bh=1 on the initial pick, [6266] = castle entry
	islandBoxCode   = 0x6824 // castle_add at cx,dx + draw the box at [6921], then jmp 6899
	boxPtrVar       = 0x6921 // DS word: Veteran box data pointer
	endlessBoxData  = 0x2975 // DS scratch after the four loaded boxes (260d + 4*0xda)
	endlessTile0    = 0x310  // first free tile id in the MTRNPF bank
	boxTextColor    = 100    // the boxes' white
	boxShadowColor  = 107    // the boxes' dark shadow
)

// Title letters, traced from VETERAN (E, N) and drawn to match (D, L, S).
// '#' = white, '+' = shadow.
var titleGlyphs = map[rune][]string{
	'E': {"..######.", ".######+.", ".++#+++..", "..##.....", "..##.###.", "..#####+.", "..##+++..", "..#+.....", ".#######.", "#######+.", "+++++++.."},
	'N': {"...##....###.", "..####..###+.", "..+###..++#..", "...+###...#..", "...#+##..#+..", "...#.###.#...", "...#.+##.#...", "...#..####...", "..###.+####..", ".###+..+##+..", ".+++....++..."},
	'D': {".#####...", "#######..", "++#++##..", ".##..##..", ".##..###.", ".##..+##.", ".##...##.", ".#+..##+.", "#######..", "######+..", "++++++..."},
	'L': {".####....", "#####+...", "++##+....", "..##.....", "..##.....", "..##.....", "..##.....", "..#+.....", ".#######.", "#######+.", "+++++++.."},
	'S': {"..#####..", ".#######.", ".##+++#+.", ".##...+..", ".+####...", "..+####..", "....+##..", ".#...##..", "#######+.", ".#####+..", ".+++++..."},
}

// Small letters in the style of "ADVANCED LEVEL" (6 rows, 2-pixel strokes).
var smallGlyphs = map[rune][]string{
	'S': {"######", "##....", "######", "....##", "....##", "######"},
	'U': {"##..##", "##..##", "##..##", "##..##", "##..##", "######"},
	'R': {"######", "##..##", "##..##", "#####.", "##..##", "##..##"},
	'V': {"##..##", "##..##", "##..##", "##..##", ".####.", "..##.."},
	'I': {"##", "##", "##", "##", "##", "##"},
	'A': {"######", "##..##", "##..##", "######", "##..##", "##..##"},
	'L': {"##...", "##...", "##...", "##...", "##...", "#####"},
	'M': {"##...##", "###.###", "##.#.##", "##...##", "##...##", "##...##"},
	'O': {"######", "##..##", "##..##", "##..##", "##..##", "######"},
	'D': {"#####.", "##..##", "##..##", "##..##", "##..##", "#####."},
	'E': {"#####", "##...", "#####", "##...", "##...", "#####"},
	' ': {"...", "...", "...", "...", "...", "..."},
}

// endlessBoxPixels renders the box interior (80x56, box cells 1..10 x 1..7).
func endlessBoxPixels() (px [56][80]uint8) {
	put := func(g []string, x, y int) int {
		w := 0
		for gy, row := range g {
			for gx, c := range row {
				xx, yy := x+gx, y+gy
				if xx < 0 || yy < 0 || xx >= 80 || yy >= 56 {
					continue
				}
				switch c {
				case '#':
					px[yy][xx] = boxTextColor
				case '+':
					px[yy][xx] = boxShadowColor
				}
			}
			w = max(w, len(row))
		}
		return w
	}
	width := func(s string, set map[rune][]string, gap int) int {
		w := 0
		for _, c := range s {
			w += len(set[c][0]) + gap
		}
		return w - gap
	}
	text := func(s string, set map[rune][]string, gap, y int) {
		x := (80 - width(s, set, gap)) / 2
		for _, c := range s {
			x += put(set[c], x, y) + gap
		}
	}
	// Box-relative rows match VETERAN: title at y 16, small lines at 37 and 45.
	text("ENDLESS", titleGlyphs, -1, 16-8)
	text("SURVIVAL", smallGlyphs, 1, 37-8)
	text("MODE", smallGlyphs, 1, 45-8)
	return
}

// writeEndlessBox writes the tiles and the 12x9 box resource.
func writeEndlessBox(m *Machine) {
	bank := uint32(m.dsw(0x1067)) << 4
	px := endlessBoxPixels()
	// Frame from RECRUITBOX (same tiles as VETERANBOX).
	cells := [9][12]uint16{}
	for x := 1; x < 11; x++ {
		cells[0][x], cells[8][x] = 0x2a6, 0x2c8
	}
	for y := 1; y < 8; y++ {
		cells[y][0], cells[y][11] = 0x2a8, 0x2a9
	}
	cells[0][0], cells[0][11], cells[8][0], cells[8][11] = 0x2a5, 0x2a7, 0x2c7, 0x2c9
	id := uint16(endlessTile0)
	for cy := 0; cy < 7; cy++ {
		for cx := 0; cx < 10; cx++ {
			var t [64]uint8
			used := false
			for y := 0; y < 8; y++ {
				for x := 0; x < 8; x++ {
					t[y*8+x] = px[cy*8+y][cx*8+x]
					used = used || t[y*8+x] != 0
				}
			}
			if !used || id > 0x3ff {
				continue
			}
			for i, v := range t {
				m.mem[bank+uint32(id)*64+uint32(i)] = v
			}
			cells[cy+1][cx+1] = id
			id++
		}
	}
	m.dssb(endlessBoxData, 12)
	m.dssb(endlessBoxData+1, 9)
	for y := 0; y < 9; y++ {
		for x := 0; x < 12; x++ {
			m.dssw(endlessBoxData+2+uint32(y*12+x)*2, cells[y][x])
		}
	}
}

func (f *Features) installEndlessBox(m *Machine) {
	m.hooks[hookVetRegion] = func(m *Machine) {
		f.endlessRegion = 3 - int(m.cpu.r[rBX])/4 // the one of regions 1, 2 Veteran didn't take
	}
	m.hooks[hookIslandBoxes] = func(m *Machine) {
		switch {
		case f.endlessBoxState == 1: // back from drawing our box
			m.dssw(boxPtrVar, f.savedBoxPtr)
			f.endlessBoxState = 2
		case m.dsw(0x22dd) == 0 && f.endlessRegion > 0:
			writeEndlessBox(m)
			f.savedBoxPtr = m.dsw(boxPtrVar)
			m.dssw(boxPtrVar, endlessBoxData)
			r := uint32(f.endlessRegion) * 4
			m.cpu.r[rCX] = m.dsw(0x68b1 + r)
			m.cpu.r[rDX] = m.dsw(0x68b3 + r)
			f.endlessBoxState = 1
			m.cpu.ip = islandBoxCode
		}
	}
	// The cursor starts on the castle met first scanning down the left edge, which
	// would be our box when it sits on region 1. Start on RECRUIT (entry 0) instead.
	m.hooks[hookFirstCastle] = func(m *Machine) {
		if f.endlessBoxState == 2 && m.dsb(0x626d) != 0 && m.cpu.r[rBX]>>8 == 1 {
			m.dssw(0x6266, 0)
		}
	}
	m.hooks[hookIslandPick] = func(m *Machine) {
		picked := f.endlessBoxState == 2 && int(m.cpu.r[rBX]) == f.endlessRegion
		f.endlessBoxState, f.endlessRegion = 0, 0
		if picked && m.dsw(0x22dd) == 0 {
			f.EndlessGame = true
			mp := f.S.Rules.EndlessMap - 1
			if mp < 0 {
				mp = rand.Intn(7)
			}
			m.dssw(0x24cd, uint16(mp))
			m.cpu.ip = 0x6913 // no Veteran bonus: a Recruit start
		}
	}
}
