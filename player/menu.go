package main

import "fmt"

// In-game options menu (F12). Drawn over the paused game in the game's own font.

type menuItem struct {
	label  func() string
	value  func() string
	change func(dir int) // left/right
	act    func()        // enter
}

type Menu struct {
	app            *App
	open           bool
	page           int // 0 main, 1 keys1, 2 keys2, 3 castle colours, 4 game rules, 5 about, 6 controls
	sel            int
	retSel         int // main-menu entry to return to
	subSel         int // Controls entry to return to from a key page
	waitKey        int // >=0 while waiting for a key for binding index
	msg            string
	pendingRestart bool
}

var colorNames = []string{"BLUE", "RED", "ORANGE"}
var colorRGB = []uint32{0x5878ff, 0xff4040, 0xffa020}
var soundNames = map[string]string{"soundblaster": "SOUND BLASTER", "adlib": "ADLIB (MUSIC ONLY)", "speaker": "PC SPEAKER", "off": "OFF"}
var soundOrder = []string{"soundblaster", "adlib", "speaker", "off"}
var bindNames = []string{"UP", "DOWN", "ALT. DOWN", "LEFT", "RIGHT", "FIRE", "ROTATE"}

func cycle(v, n, dir int) int { return ((v+dir)%n + n) % n }

func (mn *Menu) items() []menuItem {
	a := mn.app
	s := a.S
	switch mn.page {
	case 1, 2:
		keys := &s.Keys1
		if mn.page == 2 {
			keys = &s.Keys2
		}
		var it []menuItem
		for i := range bindNames {
			i := i
			it = append(it, menuItem{
				label: func() string { return bindNames[i] },
				value: func() string {
					if mn.waitKey == i {
						return "PRESS A KEY..."
					}
					return keyName(keys[i])
				},
				act: func() { mn.waitKey = i; mn.msg = "" },
			})
		}
		it = append(it,
			menuItem{label: func() string { return "RESET TO DEFAULT" }, act: func() {
				if mn.page == 1 {
					s.Keys1 = defaultKeys1
				} else {
					s.Keys2 = defaultKeys2
				}
				a.applyKeys()
			}},
			menuItem{label: func() string { return "BACK" }, act: mn.back})
		return it
	case 3:
		var it []menuItem
		for side := 0; side < 3; side++ {
			side := side
			it = append(it, menuItem{
				label: func() string { return sideNames[side] + " CASTLE" },
				value: func() string { return colorPresets[a.F.CastleColor[side]].name },
				change: func(d int) {
					a.F.CastleColor[side] = cycle(a.F.CastleColor[side], len(colorPresets), d)
				},
			})
		}
		return append(it,
			menuItem{label: func() string { return "RESET COLORS" }, act: func() { a.F.CastleColor = [3]int{} }},
			menuItem{label: func() string { return "BACK" }, act: mn.back})
	case 4:
		r := &s.Rules
		pct := func(name string, v *int) menuItem {
			return menuItem{label: func() string { return name },
				value: func() string {
					if *v == 100 {
						return "NORMAL"
					}
					return fmt.Sprintf("%d%%", *v)
				},
				change: func(d int) {
					i := 3
					for k, st := range percentSteps {
						if st == *v {
							i = k
						}
					}
					*v = percentSteps[min(len(percentSteps)-1, max(0, i+d))]
				}}
		}
		return []menuItem{
			pct("BUILD TIME", &r.BuildTime),
			pct("CANNON PLACING TIME", &r.CannonTime),
			pct("BATTLE TIME", &r.BattleTime),
			{label: func() string { return "EXTRA CANNONS" }, value: func() string {
				if r.ExtraCannons == 0 {
					return "NONE"
				}
				return fmt.Sprintf("%+d", r.ExtraCannons)
			}, change: func(d int) { r.ExtraCannons = min(10, max(-3, r.ExtraCannons+d)) }},
			pct("CANNON TOUGHNESS", &r.CannonHP),
			{label: func() string { return "GRUNTS (1 PLAYER)" }, value: func() string { return gruntNames[r.Grunts] },
				change: func(d int) {
					i := 1
					for k, g := range gruntModes {
						if g == r.Grunts {
							i = k
						}
					}
					r.Grunts = gruntModes[cycle(i, len(gruntModes), d)]
				}},
			{label: func() string { return "IRONMAN (1 PLAYER)" }, value: func() string { return onOff(r.Ironman) },
				change: func(d int) { r.Ironman = !r.Ironman }},
			{label: func() string { return "ENDLESS BOX MAP" }, value: func() string {
				switch r.EndlessMap {
				case 0:
					return "RANDOM"
				case 7:
					return "FINAL"
				}
				return fmt.Sprintf("MAP %d", r.EndlessMap)
			}, change: func(d int) { r.EndlessMap = cycle(r.EndlessMap, 8, d) }},
			{label: func() string { return "ALWAYS ENDLESS" }, value: func() string { return onOff(r.Endless) },
				change: func(d int) { r.Endless = !r.Endless }},
			{label: func() string { return "SMART PIECES" }, value: func() string { return smartNames[r.SmartPieces] },
				change: func(d int) { r.SmartPieces = cycle(r.SmartPieces, 3, d) }},
			{label: func() string { return "SMART PIECES (1 PLAYER)" }, value: func() string { return onOff(r.SmartSolo) },
				change: func(d int) { r.SmartSolo = !r.SmartSolo }},
			{label: func() string { return "COMPUTER PLAYERS" }, value: func() string { return botSkillNames[s.BotSkill] },
				change: func(d int) { s.BotSkill = cycle(s.BotSkill, 3, d) }},
			{label: func() string { return "SPECIAL CANNONS..." }, act: func() { mn.enterSub(7) }},
			{label: func() string { return "RESET RULES" }, act: func() {
				s.Rules = DefaultRules()
				s.SuperCannons, s.Balloons, s.GruntBusters = "original", "original", "solo"
			}},
			{label: func() string { return "BACK" }, act: mn.back},
		}
	}
	if mn.page == 7 {
		return []menuItem{
			extraItem("SUPER CANNONS", &s.SuperCannons),
			extraItem("BALLOONS", &s.Balloons),
			{label: func() string { return "GRUNT BUSTERS" }, value: func() string { return gbNames[s.GruntBusters] },
				change: func(d int) {
					i := 0
					for k, v := range gbModes {
						if v == s.GruntBusters {
							i = k
						}
					}
					s.GruntBusters = gbModes[cycle(i, len(gbModes), d)]
				}},
			{label: func() string { return "BACK" }, act: mn.back},
		}
	}
	if mn.page == 6 {
		padColor := func(name string, v *int) menuItem {
			return menuItem{label: func() string { return name }, value: func() string {
				if *v == ColAuto {
					return "AS JOINED"
				}
				return colorNames[*v]
			}, change: func(d int) { *v = cycle(*v, 4, d) }}
		}
		return []menuItem{
			{label: func() string { return "ARROW KEYS PLAY" }, value: func() string { return colorNames[s.ColorKeys1] },
				change: func(d int) { s.ColorKeys1 = cycle(s.ColorKeys1, 3, d) }},
			{label: func() string { return "MOUSE PLAYS" }, value: func() string { return colorNames[s.ColorMouse] },
				change: func(d int) { s.ColorMouse = cycle(s.ColorMouse, 3, d) }},
			{label: func() string { return "WASD KEYS PLAY" }, value: func() string { return colorNames[s.ColorKeys2] },
				change: func(d int) { s.ColorKeys2 = cycle(s.ColorKeys2, 3, d) }},
			padColor("PAD 1 PLAYS", &s.ColorPad1),
			padColor("PAD 2 PLAYS", &s.ColorPad2),
			{label: func() string { return "ARROW KEYS..." }, act: func() { mn.enterSub(1) }},
			{label: func() string { return "WASD KEYS..." }, act: func() { mn.enterSub(2) }},
			{label: func() string { return "MOUSE SPEED" }, value: func() string { return fmt.Sprintf("%d%%", s.MouseSpeed) },
				change: func(d int) { s.MouseSpeed = min(300, max(20, s.MouseSpeed+10*d)); a.applyMouse() }},
			{label: func() string { return "PAD CURSOR SPEED" }, value: func() string { return fmt.Sprintf("%d%%", s.PadSpeed) },
				change: func(d int) { s.PadSpeed = min(300, max(25, s.PadSpeed+25*d)) }},
			{label: func() string { return "PADS: " + a.padSummary() }},
			{label: func() string { return "BACK" }, act: mn.back},
		}
	}
	if mn.page == 5 {
		row := func(label, value string) menuItem {
			return menuItem{label: func() string { return label }, value: func() string { return value }}
		}
		h := gameHashes
		return []menuItem{
			row("A MOD BY "+ModAuthor, ""),
			row(ModURL, ""),
			row("VERSION", Version),
			row("RELEASE", h.release()),
			row("GAME FILES", fmt.Sprintf("%d", h.Files)),
			row("RAMPART.EXE SHA-256", ""),
			row(h.Exe[:32], ""),
			row(h.Exe[32:], ""),
			row("GAME FILE SET SHA-256", ""),
			row(h.Set[:32], ""),
			row(h.Set[32:], ""),
			row("UNOFFICIAL FAN MOD", ""),
			row("RAMPART (C) 1990 ATARI GAMES", ""),
			row("ALSO SAVED AS ABOUT.TXT", ""),
			{label: func() string { return "BACK" }, act: mn.back},
		}
	}
	return []menuItem{
		{label: func() string { return "SOUND" }, value: func() string { return soundNames[s.Sound] },
			change: func(d int) {
				i := 0
				for k, v := range soundOrder {
					if v == s.Sound {
						i = k
					}
				}
				s.Sound = soundOrder[cycle(i, len(soundOrder), d)]
				mn.pendingRestart = true
			}},
		{label: func() string { return "VOLUME" }, value: func() string { return fmt.Sprintf("%d%%", s.Volume) },
			change: func(d int) { s.Volume = min(100, max(0, s.Volume+10*d)); a.applyVolume() }},
		{label: func() string { return "YAPPY CAPTAIN" }, value: func() string { return onOff(s.YappyCaptain) },
			change: func(d int) { s.YappyCaptain = !s.YappyCaptain }},
		{label: func() string { return "SOLO COLOR" }, value: func() string {
			if s.SoloColor == ColAuto {
				return "AS CONTROLS"
			}
			return colorNames[s.SoloColor]
		}, change: func(d int) { s.SoloColor = cycle(s.SoloColor, 4, d) }},
		{label: func() string { return "CONTROLS..." }, act: func() { mn.enter(6) }},
		{label: func() string { return "CASTLE COLORS..." }, act: func() { mn.enter(3) }},
		{label: func() string { return "GAME RULES..." }, act: func() { mn.enter(4) }},
		{label: func() string { return "ABOUT / VERSION..." }, act: func() { writeAbout(a.saveDir); mn.enter(5) }},
		{label: func() string { return "ENDLESS RECORDS..." }, act: func() {
			mn.close()
			a.releaseAll()
			a.Board.Show(nil)
		}},
		{label: func() string { return "FULLSCREEN" }, value: func() string { return onOff(s.Fullscreen) },
			change: func(d int) { a.SetFullscreen(!s.Fullscreen) }, act: func() { a.SetFullscreen(!s.Fullscreen) }},
		{label: func() string { return "RESTART GAME" }, act: func() { mn.close(); a.Restart() }},
		{label: func() string { return "QUIT" }, act: func() { a.Quit() }},
	}
}

var extraModes = []string{"original", "everywhere", "off"}
var extraNames = map[string]string{"original": "ENHANCED ONLY", "everywhere": "ALL GAME TYPES", "off": "NEVER"}

func extraItem(name string, v *string) menuItem {
	return menuItem{label: func() string { return name }, value: func() string { return extraNames[*v] },
		change: func(d int) {
			i := 0
			for k, m := range extraModes {
				if m == *v {
					i = k
				}
			}
			*v = extraModes[cycle(i, len(extraModes), d)]
		}}
}

var gbModes = []string{"solo", "everywhere", "off"}
var gbNames = map[string]string{"solo": "1 PLAYER ONLY", "everywhere": "ALL GAMES", "off": "NEVER"}

var smartNames = []string{"OFF", "ASSIST", "EXACT"}

var gruntNames = map[string]string{"none": "NONE", "normal": "NORMAL", "more": "MORE", "invasion": "INVASION"}

func (mn *Menu) enter(page int) { mn.retSel, mn.page, mn.sel, mn.msg = mn.sel, page, 0, "" }
func (mn *Menu) back() {
	if mn.page == 1 || mn.page == 2 { // key pages live under Controls
		mn.page, mn.sel, mn.msg = 6, mn.subSel, ""
		return
	}
	if mn.page == 7 { // special cannons live under Game Rules
		mn.page, mn.sel, mn.msg = 4, mn.subSel, ""
		return
	}
	mn.page, mn.sel, mn.msg = 0, mn.retSel, ""
}

// enterSub opens a page under the current sub-page (Controls -> key bindings).
func (mn *Menu) enterSub(page int) { mn.subSel, mn.page, mn.sel, mn.msg = mn.sel, page, 0, "" }

func onOff(b bool) string {
	if b {
		return "ON"
	}
	return "OFF"
}

func (mn *Menu) Open() {
	mn.open, mn.page, mn.sel, mn.waitKey, mn.msg = true, 0, 0, -1, ""
	mn.pendingRestart = false
}

func (mn *Menu) close() {
	mn.open = false
	mn.app.S.Save()
	if mn.pendingRestart {
		mn.pendingRestart = false
		mn.app.Restart()
	}
}

// Key handles a key press (scancode) while the menu is open.
func (mn *Menu) Key(sc uint8) {
	it := mn.items()
	if mn.waitKey >= 0 {
		i := mn.waitKey
		mn.waitKey = -1
		switch sc {
		case 0x01, 0x58: // Esc / F12 cancel
			return
		case 0x2a:
			mn.msg = "LEFT SHIFT CAN'T BE USED (THE GAME IGNORES IT)"
			return
		}
		keys := &mn.app.S.Keys1
		if mn.page == 2 {
			keys = &mn.app.S.Keys2
		}
		other := &mn.app.S.Keys2
		if mn.page == 2 {
			other = &mn.app.S.Keys1
		}
		for j := range keys {
			if j != i && keys[j] == sc && !(isDown(i) && isDown(j)) {
				keys[j] = keys[i] // swap with the binding that had it
				mn.msg = keyName(sc) + " SWAPPED WITH " + bindNames[j]
			}
		}
		for j := range other {
			if other[j] == sc {
				mn.msg = keyName(sc) + " IS ALSO USED BY THE OTHER KEYS"
			}
		}
		keys[i] = sc
		mn.app.applyKeys()
		return
	}
	switch sc {
	case 0x01, 0x58: // Esc, F12
		if mn.page != 0 {
			mn.back()
			return
		}
		mn.close()
	case 0x48, 0x11: // up / W
		mn.sel = cycle(mn.sel, len(it), -1)
	case 0x50, 0x1f: // down / S
		mn.sel = cycle(mn.sel, len(it), 1)
	case 0x4b, 0x1e: // left / A
		if f := it[mn.sel].change; f != nil {
			f(-1)
		}
	case 0x4d, 0x20: // right / D
		if f := it[mn.sel].change; f != nil {
			f(1)
		}
	case 0x1c, 0x39: // Enter / Space
		if f := it[mn.sel].act; f != nil {
			f()
		} else if f := it[mn.sel].change; f != nil {
			f(1)
		}
	}
}

func (mn *Menu) Render(pix []uint32) {
	// dim the game, then a dark panel behind the text
	for i, c := range pix {
		pix[i] = c >> 2 & 0x3f3f3f
	}
	for y := 8; y < 197; y++ {
		for x := 8; x < 312; x++ {
			c := pix[y*320+x]
			pix[y*320+x] = (c>>1&0x7f7f7f + 0x0a0c14)
		}
	}
	f := mn.app.font
	it := mn.items()
	title := "OPTIONS  V" + Version
	switch mn.page {
	case 1:
		title = "ARROW KEYS"
	case 2:
		title = "WASD KEYS"
	case 3:
		title = "CASTLE COLORS"
	case 4:
		title = "GAME RULES"
	case 5:
		title = ModName
	case 6:
		title = "CONTROLS"
	case 7:
		title = "SPECIAL CANNONS"
	}
	top := 14
	f.Draw(pix, (320-TextWidth(title))/2, top, title, 0xffe080)
	y := top + 18
	step := min(11, (182-y)/len(it))
	for i, m := range it {
		lab := m.label()
		x := 24
		if i == mn.sel {
			fillRect(pix, 14, y-(step-8)/2, 292, step, 0x283860)
			f.Draw(pix, 14, y, ">", 0xffe080)
		}
		f.Draw(pix, x, y, lab, 0)
		if m.value != nil {
			v := m.value()
			tint := uint32(0)
			for k, n := range colorNames {
				if v == n {
					tint = colorRGB[k]
				}
			}
			if mn.page == 3 {
				for _, p := range colorPresets {
					if v == p.name {
						tint = p.rgb
					}
				}
			}
			f.Draw(pix, 306-TextWidth(v), y, v, tint)
		}
		y += step
	}
	foot := "ARROWS: MOVE  ENTER: SET  F12: CLOSE"
	if mn.page == 0 && mn.app.S.Sound != mn.app.runningSound {
		foot = "SOUND CHANGE RESTARTS THE GAME ON CLOSE"
	}
	if mn.msg != "" {
		foot = mn.msg
	}
	switch mn.page {
	case 3:
		foot = "COLORS RESET WHEN THE GAME CLOSES"
	case 4:
		foot = "JOIN SCREEN: 1 2 3 ADD COMPUTER PLAYERS"
	case 6:
		foot = "A FIRE B ROTATE BACK MENU START PAUSE"
	case 7:
		foot = "ROTATE IN THE CANNON PHASE TO PICK ONE"
	}
	if mn.msg != "" {
		foot = mn.msg
	}
	if !mn.app.S.colorsDistinct() && (mn.page == 0 || mn.page == 6) {
		foot = "TWO CONTROLS SHARE A COLOR: ONLY ONE CAN JOIN"
	}
	f.Draw(pix, (320-TextWidth(foot))/2, 186, foot, 0xc0c0c0)
}

func fillRect(pix []uint32, x, y, w, h int, c uint32) {
	for yy := max(0, y); yy < min(200, y+h); yy++ {
		for xx := max(0, x); xx < min(320, x+w); xx++ {
			pix[yy*320+xx] = c
		}
	}
}

func keyName(sc uint8) string {
	names := map[uint8]string{
		0x01: "ESC", 0x0e: "BACKSPACE", 0x0f: "TAB", 0x1c: "ENTER", 0x1d: "CTRL", 0x2a: "LEFT SHIFT",
		0x36: "RIGHT SHIFT", 0x38: "ALT", 0x39: "SPACE", 0x3a: "CAPS LOCK", 0x45: "NUM LOCK", 0x46: "SCROLL LOCK",
		0x47: "HOME", 0x48: "UP", 0x49: "PAGE UP", 0x4a: "PAD -", 0x4b: "LEFT", 0x4c: "PAD 5", 0x4d: "RIGHT",
		0x4e: "PAD +", 0x4f: "END", 0x50: "DOWN", 0x51: "PAGE DOWN", 0x52: "INSERT", 0x53: "DELETE",
		0x0c: "-", 0x0d: "=", 0x1a: "[", 0x1b: "]", 0x27: ";", 0x28: "'", 0x29: "`", 0x2b: "\\", 0x33: ",", 0x34: ".", 0x35: "/", 0x37: "PAD *",
	}
	if n, ok := names[sc]; ok {
		return n
	}
	row := func(start uint8, s string) (string, bool) {
		if sc >= start && int(sc-start) < len(s) {
			return string(s[sc-start]), true
		}
		return "", false
	}
	for _, r := range []struct {
		start uint8
		s     string
	}{{0x02, "1234567890"}, {0x10, "QWERTYUIOP"}, {0x1e, "ASDFGHJKL"}, {0x2c, "ZXCVBNM"}} {
		if n, ok := row(r.start, r.s); ok {
			return n
		}
	}
	if sc >= 0x3b && sc <= 0x44 {
		return fmt.Sprintf("F%d", sc-0x3a)
	}
	return fmt.Sprintf("KEY %02X", sc)
}

// down and alt-down may share a key
func isDown(i int) bool { return i == 1 || i == 2 }
