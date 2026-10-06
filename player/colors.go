package main

import "math"

// Castle colour presets (session only, never saved). A preset recolours a
// side's palette blocks on screen: entries that differ between Blue, Red and
// Orange (the side's own colours) get the preset's hue with their original
// lightness and saturation; shared entries (grass, black) are left alone.

type colorPreset struct {
	name string
	hue  float64 // degrees; <0: special
	rgb  uint32  // menu swatch
}

const (
	hueWhite   = -1
	hueBlack   = -2
	hueRainbow = -3
	hueDisco   = -4
	hueFire    = -5 // the single-player fire palette ("disco castle"), cycling
)

var colorPresets = []colorPreset{
	{"DEFAULT", 0, 0},
	{"PURPLE", 275, 0xb050ff},
	{"GREEN", 120, 0x40d040},
	{"PINK", 325, 0xff70c0},
	{"TEAL", 178, 0x30d0c8},
	{"GOLD", 50, 0xf0d030},
	{"CYAN", 195, 0x40c8ff},
	{"WHITE", hueWhite, 0xffffff},
	{"BLACK", hueBlack, 0x707070},
	{"RAINBOW", hueRainbow, 0xff8040},
	{"DISCO", hueDisco, 0xff40ff},
	{"FIRE", hueFire, 0xff6000},
}

// defaultSide[c] is the preset look of each side when CastleColor is 0.
var sideNames = []string{"BLUE", "RED", "ORANGE"}

func rgbToHSL(r, g, b float64) (h, s, l float64) {
	mx, mn := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
	l = (mx + mn) / 2
	if mx == mn {
		return 0, 0, l
	}
	d := mx - mn
	if l > 0.5 {
		s = d / (2 - mx - mn)
	} else {
		s = d / (mx + mn)
	}
	switch mx {
	case r:
		h = (g - b) / d
		if g < b {
			h += 6
		}
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	return h * 60, s, l
}

func hslToRGB(h, s, l float64) (r, g, b float64) {
	if s == 0 {
		return l, l, l
	}
	var q float64
	if l < 0.5 {
		q = l * (1 + s)
	} else {
		q = l + s - l*s
	}
	p := 2*l - q
	f := func(t float64) float64 {
		t = math.Mod(t+1, 1)
		switch {
		case t < 1.0/6:
			return p + (q-p)*6*t
		case t < 0.5:
			return q
		case t < 2.0/3:
			return p + (q-p)*(2.0/3-t)*6
		}
		return p
	}
	h /= 360
	return f(h + 1.0/3), f(h), f(h - 1.0/3)
}

// presetBlock builds a side's recoloured palette block (6-bit values).
func (f *Features) presetBlock(b, side, preset int, frame int64) [16][3]float64 {
	ref := f.playerPal[b][side]
	out := ref
	p := colorPresets[preset]
	if p.hue == hueFire {
		// Entries 1..3 of the fire block rotate (red, orange, yellow) every 8 frames,
		// as the game does during battles in single player.
		out = f.firePal[b]
		r := int(frame/8) % 3
		for k := 0; k < 3; k++ {
			out[1+(k+r)%3] = f.firePal[b][1+k]
		}
		return out
	}
	hue := p.hue
	switch hue {
	case hueRainbow:
		hue = math.Mod(float64(frame)*2+float64(side)*120, 360)
	case hueDisco:
		step := frame / 9
		hue = float64((step*137 + int64(side)*97 + int64(b)*61) % 360)
	}
	for k := 0; k < 16; k++ {
		own := false
		for c := 0; c < 3; c++ {
			if f.playerPal[b][c][k] != ref[k] {
				own = true
			}
		}
		if !own {
			continue
		}
		_, s, l := rgbToHSL(ref[k][0]/63, ref[k][1]/63, ref[k][2]/63)
		switch p.hue {
		case hueWhite:
			s, l = 0, 0.45+l*0.55
		case hueBlack:
			s, l = s*0.25, l*0.6
		default:
			s = math.Max(s, 0.35) // greyish entries still pick up some colour
		}
		r, g, bl := hslToRGB(hue, s, l)
		out[k] = [3]float64{r * 63, g * 63, bl * 63}
	}
	return out
}

// palFix adjusts the displayed palette: the solo player's colour (Blue's
// blocks shown as Red or Orange) and any castle colour presets.
func (f *Features) palFix(pal *[256]uint32, dac *[256][3]byte) {
	var frame int64
	if f.m != nil {
		frame = f.m.Frame
	}
	for g := 0; g < 3; g++ {
		side := g
		if f.solo {
			if g != 0 {
				continue // in single player these ranges hold fire and ship colours
			}
			if f.soloCol > 0 {
				side = f.soloCol
			}
		}
		preset := f.CastleColor[side]
		if side == g && preset == 0 {
			continue
		}
		for b, base := range palBlocks {
			want := f.playerPal[b][side]
			if preset != 0 {
				want = f.presetBlock(b, side, preset, frame)
			}
			recolorBlock(pal, dac, base+16*g, &f.playerPal[b][g], &want)
		}
	}
}
