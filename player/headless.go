//go:build !windows

package main

// Headless test runner (Linux/macOS), driving the same App as the Windows build.
// Script lines: <frame> <cmd> ...
//   key <sc-hex> [hold]   mouse <dx> <dy>   mbtn <0|1> <0|1>   shot <file.ppm>
//   peek|dump|poke <seg> <off> ...   menu (press F12)   quit
// Usage: rampart-headless <script> <maxframes> [savedir]
// RAMPART_WAV=file.wav records audio; RAMPART_NOFEATURES=1 disables game patches;
// RAMPART_TRACE=30c5,325f logs registers whenever the game reaches those IPs.

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

var oplLog func(int64)

type scriptEv struct {
	frame int64
	f     []string
}

func main() {
	if len(os.Args) < 3 {
		fatal("usage: %s <script> <maxframes> [savedir]", os.Args[0])
	}
	maxFrames, _ := strconv.ParseInt(os.Args[2], 10, 64)
	save := os.TempDir() + "/rampart-headless"
	if len(os.Args) > 3 {
		save = os.Args[3]
	}
	var evs []scriptEv
	sf, err := os.Open(os.Args[1])
	if err != nil {
		fatal("%v", err)
	}
	sc := bufio.NewScanner(sf)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == "" || l[0] == '#' {
			continue
		}
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		fr, _ := strconv.ParseInt(f[0], 10, 64)
		evs = append(evs, scriptEv{fr, f[1:]})
	}
	sf.Close()

	app, err := NewApp(save, openSink())
	if err != nil {
		fatal("%v", err)
	}
	if os.Getenv("RAMPART_NOFEATURES") != "" {
		app.M.hooks = [65536]func(*Machine){}
		app.VFS.Transform, app.VFS.OnWrite = nil, nil
	}
	app.Log = func(s string) { fmt.Fprintln(os.Stderr, s) }
	if os.Getenv("RAMPART_OPLLOG") != "" { // tests: OPL key-ons per 350 frames
		lastK, lastW := 0, 0
		prevOn := app.M.OnFrame
		_ = prevOn
		oplLog = func(f int64) {
			if f%350 == 0 {
				m := app.M
				fmt.Fprintf(os.Stderr, "[opl f%d] irq=%v keyons=%d writes=%d isr=%02x mask=%02x pend=%v if=%v int8=%04x:%04x\n", f, m.IRQCount, app.sb.KeyOns-lastK, app.sb.OPLWrites-lastW,
					m.picISR, m.picMask, m.irqPending, m.cpu.flags&fIF != 0, uint16(m.rb(0x22))|uint16(m.rb(0x23))<<8, uint16(m.rb(0x20))|uint16(m.rb(0x21))<<8)
				lastK, lastW = app.sb.KeyOns, app.sb.OPLWrites
				m.IRQCount = [8]int{}
			}
		}
	}
	if os.Getenv("RAMPART_BOTLOG") != "" {
		botLog = func(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...) }
	}
	if v := os.Getenv("RAMPART_FORTRESS"); v != "" { // tests: the walls standing at frame v never fall
		at, _ := strconv.ParseInt(v, 10, 64)
		var keep map[uint32][2]uint8
		app.M.FrameHooks = append(app.M.FrameHooks, func(m *Machine) {
			if m.Frame == at {
				keep = map[uint32][2]uint8{}
				for i := uint32(0); i < gn; i++ {
					if m.dsb(0x35bb+i)&0x1f == 1 {
						keep[i] = [2]uint8{m.dsb(0x35bb + i), m.dsb(0x3a29 + i)}
					}
				}
			}
			for i, v := range keep {
				if m.dsb(0x35bb+i)&0x1f != 1 {
					m.dssb(0x35bb+i, v[0])
					m.dssb(0x3a29+i, v[1])
				}
			}
		})
	}
	if os.Getenv("RAMPART_AIMGRUNT") != "" { // tests: player 1's crosshair follows a grunt
		app.M.FrameHooks = append(app.M.FrameHooks, func(m *Machine) {
			if m.dsb(0x24cb) != 1 || m.dsb(0x24ca) != 1 || m.dsw(0x231e)&0x8000 == 0 {
				return
			}
			for i := 0; i < gn; i++ {
				if m.dsb(0x35bb+uint32(i))&0x1f == 0x0b {
					m.dssw(0x236b, uint16((i%gw-1)*8+4))
					m.dssw(0x236d, uint16((i/gw-1)*8+4))
					return
				}
			}
		})
	}
	if os.Getenv("RAMPART_CAPTLOG") != "" {
		captainLog = func(n []string, busy bool) {
			fmt.Fprintf(os.Stderr, "[captain f%d] %v busy=%v\n", app.M.Frame, n, busy)
		}
	}
	if os.Getenv("RAMPART_VET1") != "" { // tests: Veteran box on island region 1
		prev := app.M.hooks[0x681c]
		app.M.hooks[0x681c] = func(m *Machine) { m.cpu.r[rBX] = 4; prev(m) }
	}
	if os.Getenv("RAMPART_SHIPLOG") != "" { // tests: 1P ship planner state, at planning and after it
		logShips := func(tag string) func(m *Machine) {
			return func(m *Machine) {
				si := uint32(m.dsw(0x24c2))
				act := 0
				for i := uint32(0); i < 16; i++ {
					if m.dsb(0x64eb+13*i) != 0xff {
						act++
					}
				}
				fmt.Fprintf(os.Stderr, "[ships f%d] %s round=%d played=%d level=%d cont=%d pool=%d,%d,%d spawn=%d,%d,%d rel=%d active=%d\n",
					m.Frame, tag, m.dsw(0x22d8), m.dsb(si+0x2344), m.dsb(si+0x2342), m.dsb(si+0x2343),
					m.dsb(0x67d4), m.dsb(0x67d5), m.dsb(0x67d6), m.dsb(0x67d1), m.dsb(0x67d2), m.dsb(0x67d3), m.dsb(0x67d7), act)
			}
		}
		for _, h := range []struct {
			ip  uint16
			tag string
		}{{0x5a77, "planned"}} {
			prev := app.M.hooks[h.ip]
			lg := logShips(h.tag)
			app.M.hooks[h.ip] = func(m *Machine) {
				if prev != nil {
					prev(m)
				}
				lg(m)
			}
		}
	}
	if os.Getenv("RAMPART_GOD") != "" { // tests: the 1P player is never defeated
		until, _ := strconv.ParseInt(os.Getenv("RAMPART_GOD_UNTIL"), 10, 64)
		app.M.hooks[0x6eda] = func(m *Machine) {
			si := uint32(m.dsw(0x24c2))
			if until == 0 || m.Frame < until {
				m.dssb(si+0x2337, 1)
			}
			fmt.Fprintf(os.Stderr, "[god f%d] round=%d level=%d score=%d\n", m.Frame, m.dsw(0x22d8),
				m.dsb(si+0x2342), uint32(m.dsw(si+0x232b))|uint32(m.dsw(si+0x232d))<<16)
		}
	}
	if t := os.Getenv("RAMPART_TRACE"); t != "" { // comma-separated IPs to log
		for _, h := range strings.Split(t, ",") {
			ip, _ := strconv.ParseUint(h, 16, 16)
			prev := app.M.hooks[ip]
			app.M.hooks[ip] = func(m *Machine) {
				c := m.cpu
				fmt.Fprintf(os.Stderr, "[trace f%d] %04x ax=%04x bx=%04x cx=%04x dx=%04x si=%04x di=%04x\n",
					m.Frame, ip, c.r[rAX], c.r[rBX], c.r[rCX], c.r[rDX], c.r[rSI], c.r[rDI])
				if prev != nil {
					prev(m)
				}
			}
		}
	}
	app.M.Log = app.Log
	type held struct {
		code  uint8
		until int64
	}
	var helds []held
	var padSt [2]PadState
	evi := 0
	quit := false
	pix := make([]uint32, 320*200)
	frame := int64(0)
	handle := func(frame int64) {
		for i := 0; i < len(helds); i++ {
			if helds[i].until <= frame {
				app.Key(helds[i].code, false)
				helds[i] = helds[len(helds)-1]
				helds = helds[:len(helds)-1]
				i--
			}
		}
		for evi < len(evs) && evs[evi].frame <= frame {
			e := evs[evi].f
			evi++
			m := app.M
			switch e[0] {
			case "key":
				code, _ := strconv.ParseUint(e[1], 16, 8)
				hold := int64(3)
				if len(e) > 2 {
					hold, _ = strconv.ParseInt(e[2], 10, 64)
				}
				app.Key(uint8(code), true)
				helds = append(helds, held{uint8(code), frame + hold})
			case "callat": // when the game next reaches IP e[1], near-call e[2] (tests only)
				at, _ := strconv.ParseUint(e[1], 16, 16)
				to, _ := strconv.ParseUint(e[2], 16, 16)
				prev := m.hooks[at]
				m.hooks[at] = func(m *Machine) {
					m.hooks[at] = prev
					m.cpu.push(m.cpu.ip)
					m.cpu.ip = uint16(to)
				}
			case "castle": // castle <side 0-2> <preset index>
				sd, _ := strconv.Atoi(e[1])
				pr, _ := strconv.Atoi(e[2])
				app.F.CastleColor[sd] = pr
			case "idx": // raw palette indices + DAC (6-bit)
				var b [320*200 + 768]uint8
				m.RenderIdx(b[:64000])
				for i := 0; i < 256; i++ {
					copy(b[64000+3*i:], m.dac[i][:])
				}
				os.WriteFile(e[1], b[:], 0o644)
			case "shot":
				app.Render(pix)
				if os.Getenv("RAMPART_GBLOG") != "" {
					gbDebug = func(t string) { fmt.Fprintf(os.Stderr, "[gbdbg f%d] %s\n", frame, t) }
					app.Render(pix)
					gbDebug = nil
					fmt.Fprintf(os.Stderr, "[tint f%d] %d px\n", frame, len(app.F.gbTint))
				}
				if app.Board.open {
					fmt.Fprintf(os.Stderr, "[board f%d] open rank=%d done=%v name=%s\n", frame, app.Board.rank, app.Board.done, app.Board.name)
				}
				writePPM(e[1], pix)
			case "pad": // pad <0|1> <buttons hex> [lx ly lt rt]; buttons -1: disconnected
				i, _ := strconv.Atoi(e[1])
				b, _ := strconv.ParseInt(e[2], 16, 32)
				v := [4]int{}
				for k := 0; k < 4 && 3+k < len(e); k++ {
					v[k], _ = strconv.Atoi(e[3+k])
				}
				padSt[i] = PadState{Connected: b >= 0, Buttons: uint16(b), LX: int16(v[0]), LY: int16(v[1]), LT: uint8(v[2]), RT: uint8(v[3])}
			case "quit":
				quit = true
			case "mouse":
				dx, _ := strconv.ParseFloat(e[1], 64)
				dy, _ := strconv.ParseFloat(e[2], 64)
				app.MouseMove(dx, dy)
			case "mbtn":
				b, _ := strconv.Atoi(e[1])
				app.MouseButton(b, e[2] == "1")
			case "peek", "dump", "poke":
				sg, _ := strconv.ParseUint(e[1], 16, 32)
				of, _ := strconv.ParseUint(e[2], 16, 32)
				base := uint32(sg*16 + of)
				switch e[0] {
				case "peek":
					n, _ := strconv.ParseUint(e[3], 16, 32)
					fmt.Fprintf(os.Stderr, "[f%d] peek %s:%s", frame, e[1], e[2])
					for i := uint32(0); i < uint32(n); i++ {
						fmt.Fprintf(os.Stderr, " %02x", m.rb(base+i))
					}
					fmt.Fprintln(os.Stderr)
				case "dump":
					n, _ := strconv.ParseUint(e[3], 16, 32)
					b := make([]byte, n)
					for i := range b {
						b[i] = m.rb(base + uint32(i))
					}
					os.WriteFile(e[4], b, 0o644)
				case "poke":
					for i := 0; i+1 < len(e[3]); i += 2 {
						v, _ := strconv.ParseUint(e[3][i:i+2], 16, 8)
						m.wb(base+uint32(i/2), uint8(v))
					}
				}
			}
		}
	}
	// Script events fire at emulated frame boundaries (exact timing) while the game
	// runs, and between frames while the menu has it paused.
	app.OnFrame = func(f int64) {
		frame = f
		handle(f)
		if oplLog != nil {
			oplLog(f)
		}
	}
	app.M.OnFrame = app.OnFrame
	for !quit && !app.Quitting() && frame < maxFrames {
		app.SetPad(0, padSt[0])
		app.SetPad(1, padSt[1])
		if app.Menu.open || app.Board.open {
			frame++
			handle(frame)
			continue
		}
		app.Frame()
	}
	fmt.Fprintf(os.Stderr, "end: frame %d icount %d cs:ip %04x:%04x\n", frame, app.M.cpu.icount, app.M.cpu.sr[sCS], app.M.cpu.ip)
	app.Close()
}

func writePPM(path string, pix []uint32) {
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	fmt.Fprintf(w, "P6\n320 200\n255\n")
	for _, p := range pix {
		w.WriteByte(byte(p >> 16))
		w.WriteByte(byte(p >> 8))
		w.WriteByte(byte(p))
	}
	w.Flush()
}
