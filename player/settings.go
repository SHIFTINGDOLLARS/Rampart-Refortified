package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Player settings (saved as settings.json next to the game's own saves).
// These sit on top of RAMPART.CFG: sound card and key bindings are written into
// the game's config whenever it is loaded, so the game always sees them.

const (
	ColBlue = iota
	ColRed
	ColOrange
	ColAuto // solo only: use the color of the controller you joined with
)

// Controller ids used by the game's join screen.
const (
	CtlKeys1 = 1 // keyboard 1
	CtlKeys2 = 2 // keyboard 2
	CtlMouse = 4
	CtlJoy1  = 5
	CtlJoy2  = 6
)

type Settings struct {
	Sound        string `json:"sound"`         // "soundblaster", "speaker", "off"
	Volume       int    `json:"volume"`        // 0..100
	YappyCaptain bool   `json:"yappy_captain"` // play the arcade's commander lines
	BotSkill     int    `json:"bot_skill"`     // 0 easy, 1 normal, 2 hard
	Fullscreen   bool   `json:"fullscreen"`

	// Color per controller in multiplayer (and in solo when SoloColor is auto).
	ColorKeys1 int `json:"color_keys1"`
	ColorMouse int `json:"color_mouse"`
	ColorKeys2 int `json:"color_keys2"`
	ColorPad1  int `json:"color_pad1"` // 0..2, or 3: join order
	ColorPad2  int `json:"color_pad2"`
	PadSpeed   int `json:"pad_speed"` // percent
	SoloColor  int `json:"solo_color"`

	Keys1 [7]uint8 `json:"keys1"` // up, down, alt-down, left, right, fire, rotate (scancodes)
	Keys2 [7]uint8 `json:"keys2"`

	MouseSpeed   int    `json:"mouse_speed"`   // percent
	Balloons     string `json:"balloons"`      // "original" (Enhanced only), "everywhere", "off"
	SuperCannons string `json:"super_cannons"` // same values
	GruntBusters string `json:"grunt_busters"` // "solo" (single player), "everywhere", "off"

	Rules Rules `json:"rules"` // difficulty modifiers

	// The rest of the game's own config (RAMPART.CFG), stored here unencrypted.
	// The game only ever sees an encrypted copy generated in memory.
	GameOptions GameOptions `json:"game_options"`
	GameCfg     wordList    `json:"game_cfg_words,omitempty"` // all 49 words as last saved by the game

	path string
}

// Options from the game's own menu (words 45..48 of RAMPART.CFG, DS:6C6F..6C75).
type GameOptions struct {
	Difficulty   int16 `json:"difficulty"`
	GameType     int16 `json:"game_type"` // 0 Classic, 1 Enhanced
	CannonKill   int16 `json:"cannon_kill"`
	BattleLength int16 `json:"battle_length"`
}

var defaultKeys1 = [7]uint8{0x48, 0x50, 0x4c, 0x4b, 0x4d, 0x1c, 0x1b} // arrows, keypad 5, Enter, ]
var defaultKeys2 = [7]uint8{0x11, 0x1f, 0x1f, 0x1e, 0x20, 0x13, 0x14} // W S S A D, R, T
var oldKeys2 = [7]uint8{0x20, 0x2e, 0x2e, 0x2d, 0x2f, 0x2a, 0x1d}     // game default (D C C X V Shift Ctrl)

func DefaultSettings() *Settings {
	return &Settings{
		Sound: "soundblaster", Volume: 80,
		ColorKeys1: ColBlue, ColorMouse: ColRed, ColorKeys2: ColOrange, SoloColor: ColAuto,
		ColorPad1: ColAuto, ColorPad2: ColAuto, PadSpeed: 100,
		Keys1: defaultKeys1, Keys2: defaultKeys2,
		MouseSpeed: 100, BotSkill: 1, Balloons: "original", SuperCannons: "original", GruntBusters: "solo", Rules: DefaultRules(),
	}
}

func LoadSettings(dir string) *Settings {
	s := DefaultSettings()
	if dir == "" {
		return s
	}
	s.path = filepath.Join(dir, "settings.json")
	if b, err := os.ReadFile(s.path); err == nil {
		json.Unmarshal(b, s)
	}
	s.sanitize()
	return s
}

func (s *Settings) sanitize() {
	if s.Sound != "speaker" && s.Sound != "off" && s.Sound != "adlib" {
		s.Sound = "soundblaster"
	}
	if s.Volume < 0 {
		s.Volume = 0
	}
	if s.Volume > 100 {
		s.Volume = 100
	}
	for _, c := range []*int{&s.ColorKeys1, &s.ColorMouse, &s.ColorKeys2} {
		if *c < 0 || *c > 2 {
			*c = 0
		}
	}
	for _, c := range []*int{&s.ColorPad1, &s.ColorPad2} {
		if *c < 0 || *c > 3 {
			*c = ColAuto
		}
	}
	if s.PadSpeed < 25 || s.PadSpeed > 300 {
		s.PadSpeed = 100
	}
	if s.SoloColor < 0 || s.SoloColor > 3 {
		s.SoloColor = ColAuto
	}
	if s.MouseSpeed < 10 || s.MouseSpeed > 400 {
		s.MouseSpeed = 100
	}
	for _, v := range []*string{&s.Balloons, &s.SuperCannons} {
		if *v != "everywhere" && *v != "off" {
			*v = "original"
		}
	}
	s.BotSkill = min(2, max(0, s.BotSkill))
	if s.GruntBusters != "everywhere" && s.GruntBusters != "off" {
		s.GruntBusters = "solo"
	}
	s.Rules.sanitize()
}

// Rules are difficulty modifiers applied through game hooks (features.go).
type Rules struct {
	BuildTime    int    `json:"build_time"`    // percent of the normal rebuild time
	CannonTime   int    `json:"cannon_time"`   // percent of the cannon-placement time
	BattleTime   int    `json:"battle_time"`   // percent of the firing time
	ExtraCannons int    `json:"extra_cannons"` // added to every player's cannons each round
	CannonHP     int    `json:"cannon_hp"`     // percent of the shots a cannon takes
	Grunts       string `json:"grunts"`        // "none", "normal", "more", "invasion" (single player)
	Ironman      bool   `json:"ironman"`       // single player: no continues
	Endless      bool   `json:"endless"`       // single player: the region is never won
	EndlessMap   int    `json:"endless_map"`   // map for the ENDLESS box: 0 random, 1..7
	SmartPieces  int    `json:"smart_pieces"`  // 0 off, 1 assist (half the pieces), 2 exact
	SmartSolo    bool   `json:"smart_solo"`    // Smart Pieces also in single player
}

var percentSteps = []int{25, 50, 75, 100, 125, 150, 200, 300}
var gruntModes = []string{"none", "normal", "more", "invasion"}

func DefaultRules() Rules {
	return Rules{BuildTime: 100, CannonTime: 100, BattleTime: 100, CannonHP: 100, Grunts: "normal", SmartPieces: 1}
}

func (r *Rules) sanitize() {
	d := DefaultRules()
	for _, p := range []struct{ v, def *int }{{&r.BuildTime, &d.BuildTime}, {&r.CannonTime, &d.CannonTime},
		{&r.BattleTime, &d.BattleTime}, {&r.CannonHP, &d.CannonHP}} {
		ok := false
		for _, st := range percentSteps {
			ok = ok || *p.v == st
		}
		if !ok {
			*p.v = *p.def
		}
	}
	r.ExtraCannons = min(10, max(-3, r.ExtraCannons))
	r.EndlessMap = min(7, max(0, r.EndlessMap))
	r.SmartPieces = min(2, max(0, r.SmartPieces))
	ok := false
	for _, g := range gruntModes {
		ok = ok || r.Grunts == g
	}
	if !ok {
		r.Grunts = "normal"
	}
}

func (s *Settings) Save() {
	if s.path == "" {
		return
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	os.WriteFile(s.path, b, 0o644)
}

// itemAllowed reports whether a cannon-phase extra ("original", "everywhere",
// "off") may be placed in the current game type.
func itemAllowed(mode string, enhanced bool) bool {
	switch mode {
	case "everywhere":
		return true
	case "off":
		return false
	}
	return enhanced
}

// ColorFor returns the color a controller plays as (or -1 for "use join order").
func (s *Settings) ColorFor(ctl uint8) int {
	switch ctl {
	case CtlKeys1:
		return s.ColorKeys1
	case CtlKeys2:
		return s.ColorKeys2
	case CtlMouse:
		return s.ColorMouse
	case CtlJoy1, CtlJoy2:
		c := s.ColorPad1
		if ctl == CtlJoy2 {
			c = s.ColorPad2
		}
		if c < 3 {
			return c
		}
	}
	return -1
}

// colorsDistinct reports whether no two controls are set to the same colour
// (pads set to "as joined" don't count).
func (s *Settings) colorsDistinct() bool {
	seen := map[int]bool{}
	for _, c := range []int{s.ColorKeys1, s.ColorKeys2, s.ColorMouse, s.ColorPad1, s.ColorPad2} {
		if c < 3 {
			if seen[c] {
				return false
			}
			seen[c] = true
		}
	}
	return true
}

// wordList is stored in JSON as one line of numbers ("3 7 1 1 ...").
type wordList []int16

func (l wordList) MarshalJSON() ([]byte, error) {
	parts := make([]string, len(l))
	for i, v := range l {
		parts[i] = strconv.Itoa(int(v))
	}
	return json.Marshal(strings.Join(parts, " "))
}

func (l *wordList) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return nil // ignore malformed values
	}
	var out wordList
	for _, f := range strings.Fields(s) {
		v, err := strconv.Atoi(f)
		if err != nil {
			return nil
		}
		out = append(out, int16(v))
	}
	*l = out
	return nil
}
