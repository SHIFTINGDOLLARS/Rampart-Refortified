package main

import (
	"io/fs"
	"strings"
)

// The game's own 8x8 menu font: tiles 0x207..0x23d of MTITPF.GRA, coloured with
// the menu palette (BRICKPAL in MTITLE.RSC). The font has no O or S; the game
// uses 0 and 5 for them, and so do we.

type Font struct {
	glyph map[rune][64]uint8
	pal   [256]uint32
}

func rscEntry(data []byte, name string) []byte {
	n := int(data[0]) | int(data[1])<<8
	for i := 0; i < n; i++ {
		p := 0x14 + 20*i
		nm := strings.TrimRight(string(data[p:p+12]), "\x00")
		if nm == name {
			off := int(data[p+12]) | int(data[p+13])<<8 | int(data[p+14])<<16 | int(data[p+15])<<24
			size := int(data[p+16]) | int(data[p+17])<<8 | int(data[p+18])<<16 | int(data[p+19])<<24
			return data[off : off+size]
		}
	}
	return nil
}

func LoadFont() *Font {
	f := &Font{glyph: map[rune][64]uint8{}}
	gra, err1 := fs.ReadFile(gameFiles, "game/MTITPF.GRA")
	rsc, err2 := fs.ReadFile(gameFiles, "game/MTITLE.RSC")
	if err1 != nil || err2 != nil {
		return f
	}
	if p := rscEntry(rsc, "BRICKPAL"); len(p) >= 768 {
		for i := 0; i < 256; i++ {
			r, g, b := uint32(p[i*3])*255/63, uint32(p[i*3+1])*255/63, uint32(p[i*3+2])*255/63
			f.pal[i] = r<<16 | g<<8 | b
		}
	}
	tile := func(n int) (t [64]uint8) {
		copy(t[:], gra[n*64:n*64+64])
		return
	}
	for c := rune('!'); c <= ':'; c++ { // the font has no ';'
		f.glyph[c] = tile(int(c) + 0x1e6)
	}
	for c := rune('<'); c <= '@'; c++ {
		f.glyph[c] = tile(int(c) + 0x1e5)
	}
	f.glyph[';'] = f.glyph[':']
	for c := rune('A'); c <= 'Z'; c++ {
		switch {
		case c <= 'N':
			f.glyph[c] = tile(int(c) + 0x1e5)
		case c == 'O':
			f.glyph[c] = tile('0' + 0x1e6)
		case c <= 'R':
			f.glyph[c] = tile(int(c) + 0x1e4)
		case c == 'S':
			f.glyph[c] = tile('5' + 0x1e6)
		default:
			f.glyph[c] = tile(int(c) + 0x1e3)
		}
	}
	return f
}

// Draw renders upper-case text at (x, y) into a 320x200 RGB buffer.
// tint != 0 recolours the glyph's brightness with that color.
func (f *Font) Draw(pix []uint32, x, y int, s string, tint uint32) int {
	for _, c := range strings.ToUpper(s) {
		g, ok := f.glyph[c]
		if ok {
			for gy := 0; gy < 8; gy++ {
				for gx := 0; gx < 8; gx++ {
					v := g[gy*8+gx]
					px, py := x+gx, y+gy
					if v == 0 || px < 0 || py < 0 || px >= 320 || py >= 200 {
						continue
					}
					col := f.pal[v]
					if tint != 0 {
						col = tintColor(col, tint)
					}
					pix[py*320+px] = col
				}
			}
		}
		x += 8
	}
	return x
}

func tintColor(c, t uint32) uint32 {
	l := ((c>>16)&255*3 + (c>>8)&255*6 + c&255) / 10 // brightness
	r := (t >> 16 & 255) * l / 255
	g := (t >> 8 & 255) * l / 255
	b := (t & 255) * l / 255
	return r<<16 | g<<8 | b
}

func TextWidth(s string) int { return 8 * len([]rune(s)) }
