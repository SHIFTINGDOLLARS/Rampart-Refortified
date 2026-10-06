package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
)

// App ties together the emulated machine, game patches, settings, audio and the
// options menu. Both front ends (Windows window, headless test runner) drive it.

type App struct {
	S       *Settings
	F       *Features
	VFS     *VFS
	M       *Machine
	sb      *SoundBlaster
	sink    sampleSink
	Menu    *Menu
	Board   *Board   // Endless records
	Rec     *Records // saved Endless runs
	font    *Font
	saveDir string

	runningSound string
	boardTick    int64
	down         [256]bool
	quit         bool
	Log          func(string)

	OnFullscreen func(on bool)
	OnFrame      func(frame int64) // test hook: called at each frame boundary
}

func NewApp(saveDir string, sink sampleSink) (*App, error) {
	if gameErr != nil {
		return nil, gameErr
	}
	a := &App{S: LoadSettings(saveDir), VFS: NewVFS(saveDir), sink: sink, font: LoadFont(), saveDir: saveDir}
	a.F = NewFeatures(a.S)
	a.Menu = &Menu{app: a, waitKey: -1}
	a.Board = &Board{app: a}
	a.Rec = LoadRecords(saveDir)
	a.migrateCfg(saveDir)
	writeAbout(saveDir)
	return a, a.start()
}

func (a *App) start() error {
	m, err := NewMachine(a.VFS, "")
	if err != nil {
		return err
	}
	m.Log = a.Log
	m.OnFrame = a.OnFrame
	if v := getenvInt("RAMPART_SNDLOG"); v > 0 {
		sndLog = v
	}
	a.F.landMap = [4]uint8{0, 1, 2, 3}
	a.F.soloCol, a.F.solo = -1, false
	a.F.Install(m)
	a.sb = NewSoundBlaster()
	a.sb.Voice = a.F.captain
	m.sound = a.sb
	a.M = m
	a.runningSound = a.S.Sound
	a.applyVolume()
	a.applyMouse()
	return nil
}

func (a *App) Restart() {
	a.releaseAll()
	if err := a.start(); err != nil {
		a.quit = true
	}
}

func (a *App) Quit() { a.quit = true }

func (a *App) Quitting() bool { return a.quit }

func (a *App) SetFullscreen(on bool) {
	a.S.Fullscreen = on
	if a.OnFullscreen != nil {
		a.OnFullscreen(on)
	}
}

func (a *App) applyVolume() {
	g := float64(a.S.Volume) / 100
	if a.S.Sound == "off" {
		g = 0
	}
	a.sb.Gain = g
}

func (a *App) applyMouse() { a.M.MouseScale = 0.4 * float64(a.S.MouseSpeed) / 100 }

// applyKeys pushes key bindings into the running game's memory (DS:6C47 / 6C61).
func (a *App) applyKeys() {
	for i := 0; i < 7; i++ {
		a.M.dssw(0x6c47+uint32(2*i), uint16(a.S.Keys1[i]))
		a.M.dssw(0x6c61+uint32(2*i), uint16(a.S.Keys2[i]))
	}
	a.S.Save()
}

func (a *App) releaseAll() {
	for sc, d := range a.down {
		if d {
			a.down[sc] = false
			a.M.KeyEvent(uint8(sc) | 0x80)
		}
	}
}

// Key delivers a host key (set-1 scancode). F12 toggles the options menu.
func (a *App) Key(sc uint8, down bool) {
	if a.Board.open {
		if down {
			a.Board.Key(sc)
		}
		return
	}
	if a.Menu.open {
		if down {
			a.Menu.Key(sc)
		}
		return
	}
	if sc == 0x58 { // F12
		if down {
			a.releaseAll()
			a.M.mouseButtons = 0
			a.Menu.Open()
		}
		return
	}
	if down {
		if a.down[sc] {
			return
		}
		a.down[sc] = true
		a.M.KeyEvent(sc)
	} else if a.down[sc] {
		a.down[sc] = false
		a.M.KeyEvent(sc | 0x80)
	}
}

func (a *App) MouseMove(dx, dy float64) {
	if !a.Menu.open && !a.Board.open {
		a.M.MouseMove(dx, dy)
	}
}

func (a *App) MouseButton(b int, down bool) {
	if !a.Menu.open && !a.Board.open {
		a.M.MouseButton(b, down)
	}
}

func (a *App) FocusLost() {
	a.releaseAll()
	a.M.mouseButtons = 0
}

// Frame advances one 70 Hz frame (paused while the menu is open).
func (a *App) Frame() {
	if a.quit {
		return
	}
	if r := a.F.pendingRun; r != nil && !a.Menu.open {
		a.F.pendingRun = nil
		a.releaseAll()
		a.M.mouseButtons = 0
		a.Board.Show(r)
	}
	if a.Menu.open || a.Board.open {
		return
	}
	ok := a.M.RunFrame()
	a.sb.catchUp(a.M)
	s := a.sb.TakeSamples()
	if a.sink != nil {
		a.sink.Write(s)
	}
	if !ok {
		a.quit = true
	}
}

func (a *App) Render(pix []uint32) {
	a.M.Render(pix)
	a.F.joinOverlay(a.M, pix, a.font)
	if a.Board.open {
		a.Board.Render(pix, a.boardTick)
		a.boardTick++
	}
	if a.Menu.open {
		a.Menu.Render(pix)
	}
}

func (a *App) Close() {
	a.S.Save()
	if a.sink != nil {
		a.sink.Close()
	}
}

// Earlier versions kept the game's encrypted RAMPART.CFG in the save folder.
// Fold it into settings.json and remove it.
func (a *App) migrateCfg(saveDir string) {
	if saveDir == "" {
		return
	}
	p := filepath.Join(saveDir, "RAMPART.CFG")
	b, err := os.ReadFile(p)
	if err != nil {
		return
	}
	if len(b) >= 0x66 {
		plain := append([]byte(nil), b[:0x62]...)
		cfgCrypt(plain)
		if cfgChecksum(plain) == binary.LittleEndian.Uint32(b[0x62:]) {
			a.F.importCfg(b)
			a.S.Save()
		}
	}
	os.Remove(p)
}
