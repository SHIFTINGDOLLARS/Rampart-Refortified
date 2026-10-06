package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Endless records: the best Endless runs, kept in endless.json next to the
// settings. A run is ranked by battles survived, then by score. When an Endless
// game ends (the game is back at its menus), the board opens over the screen;
// a run that makes the top ten gets its initials typed in. F12 can show it too.

const recordsMax = 10

type Record struct {
	Name   string `json:"name"`
	Rounds int    `json:"rounds"` // battles survived
	Score  uint32 `json:"score"`
	Cont   int    `json:"continues"`
	Level  int    `json:"level"` // ship level reached (0..4)
}

type Records struct {
	List []Record `json:"records"`
	Last string   `json:"last_name"`
	path string
}

func LoadRecords(dir string) *Records {
	r := &Records{Last: "AAA"}
	if dir == "" {
		return r
	}
	r.path = filepath.Join(dir, "endless.json")
	if b, err := os.ReadFile(r.path); err == nil {
		json.Unmarshal(b, r)
	}
	if len(r.Last) != 3 {
		r.Last = "AAA"
	}
	r.sort()
	return r
}

func (r *Records) Save() {
	if r.path == "" {
		return
	}
	b, _ := json.MarshalIndent(r, "", "  ")
	os.WriteFile(r.path, b, 0o644)
}

func (r *Records) sort() {
	sort.SliceStable(r.List, func(i, j int) bool {
		a, b := r.List[i], r.List[j]
		if a.Rounds != b.Rounds {
			return a.Rounds > b.Rounds
		}
		return a.Score > b.Score
	})
	if len(r.List) > recordsMax {
		r.List = r.List[:recordsMax]
	}
}

// rankOf returns where rec would land (-1: off the board).
func (r *Records) rankOf(rec Record) int {
	for i, o := range r.List {
		if rec.Rounds > o.Rounds || (rec.Rounds == o.Rounds && rec.Score > o.Score) {
			return i
		}
	}
	if len(r.List) < recordsMax {
		return len(r.List)
	}
	return -1
}

// ------------------------------------------------------------------ screen

const nameChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 "

type Board struct {
	app  *App
	open bool
	run  *Record // the run just finished (nil: just viewing)
	rank int     // its row while entering initials (-1: not on the board)
	name [3]byte
	pos  int
	done bool // initials entered
}

// Show opens the board for a finished run (or just to view, run == nil).
func (bd *Board) Show(run *Record) {
	bd.open, bd.run, bd.rank, bd.pos, bd.done = true, run, -1, 0, false
	copy(bd.name[:], bd.app.Rec.Last)
	if run != nil && len(run.Name) == 3 {
		copy(bd.name[:], run.Name) // typed on the game's own high-score screen
	}
	bd.done = true // just viewing
	if run != nil {
		bd.rank = bd.app.Rec.rankOf(*run)
		bd.done = bd.rank < 0
	}
}

func (bd *Board) commit() {
	r := bd.app.Rec
	if bd.run != nil && bd.rank >= 0 && !bd.done {
		rec := *bd.run
		rec.Name = string(bd.name[:])
		r.Last = rec.Name
		r.List = append(r.List, rec)
		r.sort()
		r.Save()
	}
	bd.done = true
}

func (bd *Board) close() {
	bd.commit()
	bd.open, bd.run = false, nil
}

func (bd *Board) Key(sc uint8) {
	if bd.done {
		switch sc {
		case 0x1c, 0x39, 0x01, 0x58: // Enter, Space, Esc, F12
			bd.close()
		}
		return
	}
	cycle := func(d int) {
		i := 0
		for k := range nameChars {
			if nameChars[k] == bd.name[bd.pos] {
				i = k
			}
		}
		bd.name[bd.pos] = nameChars[(i+d+len(nameChars))%len(nameChars)]
	}
	switch sc {
	case 0x48: // up
		cycle(-1)
	case 0x50: // down
		cycle(1)
	case 0x4b, 0x0e: // left / Backspace
		bd.pos = max(0, bd.pos-1)
	case 0x4d: // right
		bd.pos = min(2, bd.pos+1)
	case 0x1c: // Enter
		if bd.pos < 2 {
			bd.pos++
			return
		}
		bd.commit()
	case 0x01, 0x58: // Esc / F12: keep the name as it is
		bd.commit()
	default:
		if c := scanChar(sc); c != 0 {
			bd.name[bd.pos] = c
			if bd.pos < 2 {
				bd.pos++
			}
		}
	}
}

// scanChar maps letter/digit scancodes to characters (0: none).
func scanChar(sc uint8) byte {
	rows := []struct {
		start uint8
		s     string
	}{{0x02, "1234567890"}, {0x10, "QWERTYUIOP"}, {0x1e, "ASDFGHJKL"}, {0x2c, "ZXCVBNM"}}
	for _, r := range rows {
		if sc >= r.start && int(sc-r.start) < len(r.s) {
			return r.s[sc-r.start]
		}
	}
	return 0
}

func (bd *Board) Render(pix []uint32, frame int64) {
	for i, c := range pix {
		pix[i] = c >> 2 & 0x3f3f3f
	}
	for y := 8; y < 197; y++ {
		for x := 8; x < 312; x++ {
			c := pix[y*320+x]
			pix[y*320+x] = c>>1&0x7f7f7f + 0x0a0c14
		}
	}
	f := bd.app.font
	right := func(x, y int, s string, c uint32) { f.Draw(pix, x-TextWidth(s), y, s, c) }
	center := func(y int, s string, c uint32) { f.Draw(pix, (320-TextWidth(s))/2, y, s, c) }
	center(14, "ENDLESS RECORDS", 0xffe080)
	hy := 32
	f.Draw(pix, 44, hy, "NAME", 0x9090a0)
	right(170, hy, "ROUNDS", 0x9090a0)
	right(250, hy, "SCORE", 0x9090a0)
	right(300, hy, "CONT", 0x9090a0)

	list := append([]Record(nil), bd.app.Rec.List...)
	entering := bd.run != nil && bd.rank >= 0 && !bd.done
	if entering {
		rec := *bd.run
		rec.Name = string(bd.name[:])
		list = append(list[:bd.rank], append([]Record{rec}, list[bd.rank:]...)...)
		if len(list) > recordsMax {
			list = list[:recordsMax]
		}
	}
	y := hy + 14
	for i, r := range list {
		mine := bd.run != nil && i == bd.rank
		col := uint32(0)
		if mine {
			fillRect(pix, 14, y-2, 292, 11, 0x283860)
			col = 0xffe080
		}
		right(36, y, fmt.Sprintf("%d", i+1), col)
		for k := 0; k < 3; k++ {
			ch := string(r.Name[k])
			if mine && entering && k == bd.pos && frame/18%2 == 0 {
				ch = "-"
			}
			f.Draw(pix, 44+8*k, y, ch, col)
		}
		right(170, y, fmt.Sprintf("%d", r.Rounds), col)
		right(250, y, fmt.Sprintf("%d", r.Score), col)
		right(300, y, fmt.Sprintf("%d", r.Cont), col)
		y += 12
	}
	if len(list) == 0 {
		center(80, "NO RUNS YET", 0)
		center(96, "PICK THE ENDLESS BOX ON THE ISLAND MAP", 0x9090a0)
	}
	foot := "ENTER: CLOSE"
	switch {
	case entering:
		foot = "TYPE OR ARROWS: INITIALS   ENTER: OK"
	case bd.run != nil && bd.rank < 0:
		foot = fmt.Sprintf("YOUR RUN: %d ROUNDS  %d   ENTER: CLOSE", bd.run.Rounds, bd.run.Score)
	}
	center(186, foot, 0xc0c0c0)
}
