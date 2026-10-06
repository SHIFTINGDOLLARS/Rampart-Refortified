package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"strings"
)

// Machine: memory, VGA, PIT/PIC, keyboard, DOS services. Port of emu/emu.c.

const (
	CPUHz = 8_000_000 // emulated instructions per second (matches the validated C emulator)
)

type Machine struct {
	cpu  CPU
	mem  [0x110000]byte
	vram [4][0x10000]byte
	dac  [256][3]byte

	dacWIdx, dacWSub, dacRIdx, dacRSub int
	seqIdx                             uint8
	seq                                [8]uint8
	gcIdx                              uint8
	gc                                 [16]uint8
	crtcIdx                            uint8
	crtc                               [32]uint8
	latch                              [4]uint8
	vmode                              int

	// devices
	pitDiv     uint16
	pitLoHi    bool
	pitLatchLo uint8
	picMask    uint8
	picISR     uint8
	IRQCount   [8]int
	irqPending [8]bool
	kbdPort60  uint8
	port61     uint8
	pit40Tog   bool
	pit2LoHi   bool
	pit2Latch  uint8
	pit2Mode   uint8
	Pit2Div    uint16 // PC speaker tone divisor (PIT channel 2)

	nextTimer, timerPeriod, framePeriod uint64

	// DOS
	files    [32]*vfile
	fs       *VFS
	memTop   uint16
	pspSeg   uint16
	allocNxt uint16     // start of the DOS heap (after the program)
	blocks   []memBlock // allocated DOS blocks, sorted by segment
	exited   bool

	hooks [65536]func(*Machine) // code hooks in the game segment (CS 0x0810)

	// mouse (INT 33h)
	mouseX, mouseY         float64
	mouseMinX, mouseMaxX   int
	mouseMinY, mouseMaxY   int
	mouseButtons           uint16
	mouseMickX, mouseMickY float64
	MouseScale             float64 // host counts -> game pixels

	keyQueue   []uint8
	Log        func(string)
	Frame      int64
	nextFrame  uint64
	OnFrame    func(frame int64)
	PalMap     *[256]uint8                               // optional display-only palette index remap
	IdxFix     func(idx []uint8)                         // display-only edits to the indexed frame
	RGBFix     func(dst []uint32)                        // display-only edits to the final frame
	FrameHooks []func(m *Machine)                        // run at every 70 Hz frame boundary
	PalFix     func(pal *[256]uint32, dac *[256][3]byte) // display-only colour adjustments

	sound    SoundHooks // optional (Sound Blaster / OPL); nil-safe
	soundDue uint64
}

// SoundHooks lets an audio backend claim I/O ports and DMA.
type SoundHooks interface {
	PortIn(m *Machine, p uint16) (uint8, bool)
	PortOut(m *Machine, p uint16, v uint8) bool
	Tick(m *Machine) // called once per emulated instruction batch
}

func (m *Machine) logf(f string, a ...any) {
	if m.Log != nil {
		m.Log(fmt.Sprintf(f, a...))
	}
}

// ---------------------------------------------------------------- memory/VGA

func (m *Machine) chain4() bool { return m.seq[4]&0x08 != 0 }

func (m *Machine) vgaRead(a uint32) uint8 {
	o := a - 0xA0000
	if m.chain4() {
		return m.vram[o&3][o>>2&0x3fff]
	}
	for p := 0; p < 4; p++ {
		m.latch[p] = m.vram[p][o]
	}
	return m.vram[m.gc[4]&3][o]
}

func (m *Machine) vgaWrite(a uint32, v uint8) {
	o := a - 0xA0000
	if m.chain4() {
		if m.seq[2]&(1<<(o&3)) != 0 {
			m.vram[o&3][o>>2&0x3fff] = v
		}
		return
	}
	wm := m.gc[5] & 3
	for p := 0; p < 4; p++ {
		if m.seq[2]&(1<<p) == 0 {
			continue
		}
		var x uint8
		switch wm {
		case 1:
			x = m.latch[p]
		case 0:
			d := v
			rot := m.gc[3] & 7
			d = d>>rot | d<<(8-rot)
			if m.gc[1]&(1<<p) != 0 {
				if m.gc[0]&(1<<p) != 0 {
					d = 0xff
				} else {
					d = 0
				}
			}
			switch m.gc[3] >> 3 & 3 {
			case 1:
				d &= m.latch[p]
			case 2:
				d |= m.latch[p]
			case 3:
				d ^= m.latch[p]
			}
			x = d&m.gc[8] | m.latch[p]&^m.gc[8]
		case 2:
			var d uint8
			if v&(1<<p) != 0 {
				d = 0xff
			}
			x = d&m.gc[8] | m.latch[p]&^m.gc[8]
		default:
			var d uint8
			if m.gc[0]&(1<<p) != 0 {
				d = 0xff
			}
			mk := v & m.gc[8]
			x = d&mk | m.latch[p]&^mk
		}
		m.vram[p][o] = x
	}
}

func (m *Machine) rb(a uint32) uint8 {
	a &= 0xFFFFF
	if a >= 0xA0000 && a < 0xB0000 {
		return m.vgaRead(a)
	}
	return m.mem[a]
}
func (m *Machine) wb(a uint32, v uint8) {
	a &= 0xFFFFF
	if a >= 0xA0000 && a < 0xB0000 {
		m.vgaWrite(a, v)
		return
	}
	if a >= 0xF0000 {
		return
	}
	m.mem[a] = v
}
func (m *Machine) rw(a uint32) uint16    { return uint16(m.rb(a)) | uint16(m.rb(a+1))<<8 }
func (m *Machine) ww(a uint32, v uint16) { m.wb(a, uint8(v)); m.wb(a+1, uint8(v>>8)) }

// ------------------------------------------------------------------- ports

func (m *Machine) portIn(p uint16) uint8 {
	if m.sound != nil {
		if v, ok := m.sound.PortIn(m, p); ok {
			return v
		}
	}
	switch p {
	case 0x60:
		return m.kbdPort60
	case 0x61:
		return m.port61
	case 0x21:
		return m.picMask
	case 0x40:
		m.pit40Tog = !m.pit40Tog
		if m.pit40Tog {
			return uint8(m.cpu.icount >> 8)
		}
		return uint8(m.cpu.icount)
	case 0x201:
		return 0xF0
	case 0x3c5:
		return m.seq[m.seqIdx&7]
	case 0x3cf:
		return m.gc[m.gcIdx&15]
	case 0x3d5:
		return m.crtc[m.crtcIdx&31]
	case 0x3c9:
		v := m.dac[m.dacRIdx][m.dacRSub]
		m.dacRSub++
		if m.dacRSub == 3 {
			m.dacRSub = 0
			m.dacRIdx = (m.dacRIdx + 1) & 255
		}
		return v
	case 0x3da, 0x3ba:
		ph := m.cpu.icount % m.framePeriod
		var v uint8
		if ph < m.framePeriod/20 {
			v |= 0x08
		}
		if (m.cpu.icount/50)&1 != 0 {
			v |= 0x01
		}
		return v
	}
	return 0xff
}

func (m *Machine) portOut(p uint16, v uint8) {
	if m.sound != nil && m.sound.PortOut(m, p, v) {
		return
	}
	switch p {
	case 0x20:
		if v == 0x20 {
			m.picISR = 0
		}
	case 0x21:
		m.picMask = v
	case 0x43:
		switch v >> 6 {
		case 0:
			m.pitLoHi = false
		case 2:
			m.pit2LoHi = false
			m.pit2Mode = v
		}
	case 0x42:
		if !m.pit2LoHi {
			m.pit2Latch = v
			m.pit2LoHi = true
		} else {
			m.Pit2Div = uint16(m.pit2Latch) | uint16(v)<<8
			m.pit2LoHi = false
		}
	case 0x40:
		if !m.pitLoHi {
			m.pitLatchLo = v
			m.pitLoHi = true
		} else {
			m.pitDiv = uint16(m.pitLatchLo) | uint16(v)<<8
			m.pitLoHi = false
			d := uint32(m.pitDiv)
			if d == 0 {
				d = 65536
			}
			m.timerPeriod = uint64(cpuHzF * float64(d) / 1193182.0)
		}
	case 0x61:
		m.port61 = v
	case 0x3c4:
		m.seqIdx = v
	case 0x3c5:
		m.seq[m.seqIdx&7] = v
	case 0x3ce:
		m.gcIdx = v
	case 0x3cf:
		m.gc[m.gcIdx&15] = v
	case 0x3d4:
		m.crtcIdx = v
	case 0x3d5:
		m.crtc[m.crtcIdx&31] = v
	case 0x3c7:
		m.dacRIdx, m.dacRSub = int(v), 0
	case 0x3c8:
		m.dacWIdx, m.dacWSub = int(v), 0
	case 0x3c9:
		m.dac[m.dacWIdx][m.dacWSub] = v & 63
		m.dacWSub++
		if m.dacWSub == 3 {
			m.dacWSub = 0
			m.dacWIdx = (m.dacWIdx + 1) & 255
		}
	}
}

func (m *Machine) RaiseIRQ(n int) { m.irqPending[n] = true }

// --------------------------------------------------------------------- DOS

func (m *Machine) setCF(on bool) {
	c := &m.cpu
	a := c.lin(sSS, c.r[rSP]+4)
	f := m.rw(a)
	if on {
		f |= fCF
	} else {
		f &^= fCF
	}
	m.ww(a, f)
}

func (m *Machine) readName(a uint32) string {
	var sb strings.Builder
	for i := uint32(0); i < 127; i++ {
		ch := m.rb(a + i)
		if ch == 0 {
			break
		}
		sb.WriteByte(ch)
	}
	n := sb.String()
	if len(n) > 1 && n[1] == ':' {
		n = n[2:]
	}
	n = strings.TrimLeft(n, "\\/")
	if i := strings.LastIndexAny(n, "\\/"); i >= 0 {
		n = n[i+1:]
	}
	return strings.ToUpper(n)
}

func (m *Machine) int21() {
	c := &m.cpu
	r := &c.r
	ah := uint8(r[rAX] >> 8)
	switch ah {
	case 0x02, 0x09:
	case 0x25:
		n := uint32(r[rAX] & 0xff)
		m.ww(n*4, r[rDX])
		m.ww(n*4+2, c.sr[sDS])
	case 0x35:
		n := uint32(r[rAX] & 0xff)
		r[rBX] = m.rw(n * 4)
		c.sr[sES] = m.rw(n*4 + 2)
	case 0x30:
		r[rAX] = 0x0005
	case 0x2c:
		cs := c.icount * 100 / CPUHz
		r[rCX] = uint16((cs/360000)%24)<<8 | uint16((cs/6000)%60)
		r[rDX] = uint16((cs/100)%60)<<8 | uint16(cs%100)
	case 0x2a:
		r[rCX] = 1992
		r[rDX] = 0x0101
		r[rAX] = r[rAX]&0xff00 | 3
	case 0x3c, 0x3d:
		name := m.readName(c.lin(sDS, r[rDX]))
		h := 5
		for h < len(m.files) && m.files[h] != nil {
			h++
		}
		var f *vfile
		var err error
		if ah == 0x3c {
			f, err = m.fs.Create(name)
		} else {
			f, err = m.fs.Open(name, r[rAX]&3 != 0)
		}
		m.logf("[dos] %s %s -> %v", map[bool]string{true: "create", false: "open"}[ah == 0x3c], name, err)
		if err != nil || h >= len(m.files) {
			r[rAX] = 2
			m.setCF(true)
		} else {
			m.files[h] = f
			r[rAX] = uint16(h)
			m.setCF(false)
		}
	case 0x3e:
		if int(r[rBX]) < len(m.files) && m.files[r[rBX]] != nil {
			m.files[r[rBX]].Close()
			m.files[r[rBX]] = nil
		}
		m.setCF(false)
	case 0x3f:
		h := int(r[rBX])
		if h >= len(m.files) || m.files[h] == nil {
			r[rAX] = 6
			m.setCF(h != 0)
			if h == 0 {
				r[rAX] = 0
			}
			break
		}
		a := c.lin(sDS, r[rDX])
		buf := make([]byte, r[rCX])
		n := m.files[h].Read(buf)
		for i := 0; i < n; i++ {
			m.wb(a+uint32(i), buf[i])
		}
		r[rAX] = uint16(n)
		m.setCF(false)
	case 0x40:
		h := int(r[rBX])
		a := c.lin(sDS, r[rDX])
		n := int(r[rCX])
		if h == 1 || h == 2 {
			r[rAX] = uint16(n)
			m.setCF(false)
			break
		}
		if h >= len(m.files) || m.files[h] == nil {
			r[rAX] = 6
			m.setCF(true)
			break
		}
		buf := make([]byte, n)
		for i := range buf {
			buf[i] = m.rb(a + uint32(i))
		}
		m.files[h].Write(buf)
		r[rAX] = uint16(n)
		m.setCF(false)
	case 0x42:
		h := int(r[rBX])
		if h >= len(m.files) || m.files[h] == nil {
			r[rAX] = 6
			m.setCF(true)
			break
		}
		off := int64(int32(uint32(r[rCX])<<16 | uint32(r[rDX])))
		p := m.files[h].SeekTo(off, int(r[rAX]&0xff))
		r[rAX] = uint16(p)
		r[rDX] = uint16(p >> 16)
		m.setCF(false)
	case 0x44:
		r[rDX] = 0x80
		m.setCF(false)
	case 0x48:
		seg, max := m.dosAlloc(r[rBX])
		if seg == 0 {
			r[rAX], r[rBX] = 8, max
			m.setCF(true)
			break
		}
		r[rAX] = seg
		m.setCF(false)
	case 0x49:
		m.setCF(!m.dosFree(c.sr[sES]))
		if c.flags&1 != 0 {
			r[rAX] = 9
		}
	case 0x4a:
		if c.sr[sES] == m.pspSeg {
			m.allocNxt = m.pspSeg + r[rBX] + 1
			if m.allocNxt > m.memTop {
				r[rBX] = m.memTop - m.pspSeg
				m.setCF(true)
				break
			}
		} else if max, ok := m.dosResize(c.sr[sES], r[rBX]); !ok {
			r[rAX], r[rBX] = 8, max
			m.setCF(true)
			break
		}
		m.setCF(false)
	case 0x4c:
		m.exited = true
		c.halted = 1
	case 0x0b, 0x06, 0x07, 0x08:
		r[rAX] &= 0xff00
	case 0x19:
		r[rAX] = r[rAX]&0xff00 | 2
	case 0x0e:
		r[rAX] = r[rAX]&0xff00 | 26
	case 0x47:
		m.wb(c.lin(sDS, r[rSI]), 0)
		m.setCF(false)
	default:
		m.logf("[dos] unhandled int21 ah=%02x", ah)
		m.setCF(true)
	}
}

func (m *Machine) hostInt(n int) {
	c := &m.cpu
	r := &c.r
	switch n {
	case 0x21:
		m.int21()
	case 0x10:
		ah := uint8(r[rAX] >> 8)
		switch {
		case ah == 0x00:
			m.vmode = int(r[rAX] & 0x7f)
			if m.vmode == 0x13 {
				m.seq = [8]uint8{}
				m.seq[2], m.seq[4] = 0xf, 0x0e
				m.gc = [16]uint8{}
				m.gc[8], m.gc[5] = 0xff, 0x40
				m.crtc = [32]uint8{}
				m.crtc[0x13], m.crtc[0x18], m.crtc[0x07], m.crtc[0x09] = 40, 0xff, 0x1f, 0x41
				if r[rAX]&0x80 == 0 {
					m.vram = [4][0x10000]byte{}
				}
			}
		case ah == 0x0f:
			r[rAX] = 40<<8 | uint16(m.vmode)
			r[rBX] &= 0xff
		case ah == 0x1a:
			r[rAX] = r[rAX]&0xff00 | 0x1a
			r[rBX] = 0x0008
		case ah == 0x10 && r[rAX]&0xff == 0x12:
			a := c.lin(sES, r[rDX])
			for i := 0; i < int(r[rCX]); i++ {
				for k := 0; k < 3; k++ {
					m.dac[(int(r[rBX])+i)&255][k] = m.rb(a+uint32(i*3+k)) & 63
				}
			}
		}
	case 0x16:
		r[rAX] = 0
		a := c.lin(sSS, r[rSP]+4)
		m.ww(a, m.rw(a)|fZF)
	case 0x1a:
		t := uint64(float64(c.icount) / CPUHz * 18.2065)
		r[rCX] = uint16(t >> 16)
		r[rDX] = uint16(t)
		r[rAX] &= 0xff00
	case 0x33:
		m.int33()
	case 0x08:
		m.picISR = 0
	case 0x09:
		m.picISR = 0
	}
}

// ------------------------------------------------------------------ loader

func NewMachine(fs *VFS, cmdline string) (*Machine, error) {
	m := &Machine{fs: fs, memTop: 0x9FFF, vmode: 3}
	m.cpu.m = m
	for n := 0; n < 256; n++ {
		a := 0xF0000 + n*4
		m.mem[a], m.mem[a+1], m.mem[a+2], m.mem[a+3] = 0x0f, 0xff, byte(n), 0xcf
		m.mem[n*4], m.mem[n*4+1], m.mem[n*4+2], m.mem[n*4+3] = byte(n*4), byte(n*4>>8), 0x00, 0xf0
	}
	m.mem[0x449], m.mem[0x44a], m.mem[0x463], m.mem[0x464] = 3, 80, 0xd4, 0x03
	m.timerPeriod = uint64(cpuHzF / 18.2065)
	m.framePeriod = uint64(cpuHzF / 70.0)
	m.nextTimer = m.timerPeriod

	buf, err := fs.ReadAll("RAMPART.EXE")
	if err != nil {
		return nil, err
	}
	h := func(i int) uint16 { return binary.LittleEndian.Uint16(buf[i*2:]) }
	hdr := uint32(h(4)) * 16
	size := uint32(h(2)-1)*512 + uint32(h(1))
	if h(1) == 0 {
		size += 512
	}
	if size > uint32(len(buf)) {
		size = uint32(len(buf))
	}
	m.pspSeg = 0x0800
	load := m.pspSeg + 0x10
	env := uint32(0x0700)
	envs := "PATH=C:\\\x00COMSPEC=C:\\COMMAND.COM\x00\x00\x01\x00C:\\RAMPART.EXE\x00"
	copy(m.mem[env*16:], envs)
	copy(m.mem[uint32(load)*16:], buf[hdr:size])
	for i := 0; i < int(h(3)); i++ {
		o := binary.LittleEndian.Uint16(buf[int(h(12))+i*4:])
		s := binary.LittleEndian.Uint16(buf[int(h(12))+i*4+2:])
		a := uint32(load+s)*16 + uint32(o)
		v := uint16(m.mem[a]) | uint16(m.mem[a+1])<<8
		v += load
		m.mem[a], m.mem[a+1] = byte(v), byte(v>>8)
	}
	psp := m.mem[uint32(m.pspSeg)*16:]
	psp[0], psp[1], psp[2], psp[3] = 0xcd, 0x20, byte(m.memTop), byte(m.memTop>>8)
	psp[0x2c], psp[0x2d] = byte(env), byte(env>>8)
	psp[0x80] = byte(len(cmdline))
	copy(psp[0x81:], cmdline)
	psp[0x81+len(cmdline)] = 13
	c := &m.cpu
	c.sr[sCS] = load + h(11)
	c.ip = h(10)
	c.sr[sSS] = load + h(7)
	c.r[rSP] = h(8)
	c.sr[sDS], c.sr[sES] = m.pspSeg, m.pspSeg
	c.flags = 0x0202
	m.allocNxt = load + uint16((size-hdr+15)/16) + h(5) + 1
	return m, nil
}

// ----------------------------------------------------------------- running

// KeyEvent queues a raw set-1 scancode (bit 7 = release).
func (m *Machine) KeyEvent(sc uint8) { m.keyQueue = append(m.keyQueue, sc) }

// RunFrame executes instructions up to and including the next 70 Hz frame
// boundary (same loop order as the validated C emulator). OnFrame, if set, is
// called at the boundary before one queued key byte is delivered.
// Returns false when the program has exited or crashed.
func (m *Machine) RunFrame() bool {
	c := &m.cpu
	if m.nextFrame == 0 {
		m.nextFrame = m.framePeriod
	}
	for c.halted == 0 {
		if c.sr[sCS] == gameCS {
			if h := m.hooks[c.ip]; h != nil {
				h(m)
			}
		}
		c.step()
		if c.icount >= m.nextTimer {
			if m.timerPeriod != 0 {
				m.nextTimer += m.timerPeriod
			} else {
				m.nextTimer += uint64(cpuHzF / 18.2)
			}
			m.RaiseIRQ(0)
		}
		if m.sound != nil && c.icount >= m.soundDue {
			m.sound.Tick(m)
		}
		boundary := false
		if c.icount >= m.nextFrame {
			m.nextFrame += m.framePeriod
			m.Frame++
			for _, h := range m.FrameHooks {
				h(m)
			}
			if m.OnFrame != nil {
				m.OnFrame(m.Frame)
			}
			if len(m.keyQueue) > 0 && !m.irqPending[1] {
				m.kbdPort60 = m.keyQueue[0]
				m.keyQueue = m.keyQueue[1:]
				m.RaiseIRQ(1)
			}
			boundary = true
		}
		if c.flags&fIF != 0 && m.picISR == 0 {
			for n := 0; n < 8; n++ {
				if m.irqPending[n] && m.picMask&(1<<n) == 0 {
					m.irqPending[n] = false
					m.picISR = 1 << n
					m.IRQCount[n]++
					c.doInt(8 + n)
					break
				}
			}
		}
		if boundary {
			break
		}
	}
	if c.halted == 2 || c.halted == 3 {
		m.logf("CPU stopped (%d) at %04x:%04x", c.halted, c.sr[sCS], c.ip)
	}
	return c.halted == 0
}

// Render writes the current 320x200 screen as 0x00RRGGBB pixels.
func (m *Machine) Render(dst []uint32) {
	var pal [256]uint32
	for i := 0; i < 256; i++ {
		r := uint32(m.dac[i][0]) * 255 / 63
		g := uint32(m.dac[i][1]) * 255 / 63
		b := uint32(m.dac[i][2]) * 255 / 63
		pal[i] = r<<16 | g<<8 | b
	}
	if m.PalFix != nil {
		m.PalFix(&pal, &m.dac)
	}
	if m.vmode != 0x13 {
		for i := range dst[:320*200] {
			dst[i] = 0
		}
		return
	}
	var idx [320 * 200]uint8
	m.RenderIdx(idx[:])
	if m.IdxFix != nil {
		m.IdxFix(idx[:])
	}
	if m.PalMap != nil {
		for i, c := range idx {
			dst[i] = pal[m.PalMap[c]]
		}
	} else {
		for i, c := range idx {
			dst[i] = pal[c]
		}
	}
	if m.RGBFix != nil {
		m.RGBFix(dst[:320*200])
	}
}

// RenderIdx writes the displayed 320x200 picture as palette indices.
func (m *Machine) RenderIdx(dst []uint8) {
	start := uint32(m.crtc[0x0c])<<8 | uint32(m.crtc[0x0d])
	lc := int(m.crtc[0x18]) | int(m.crtc[0x07]&0x10)<<4 | int(m.crtc[0x09]&0x40)<<3
	dbl := m.crtc[0x09]&0x80 != 0 || m.crtc[0x09]&0x1f != 0
	offset := uint32(80)
	if m.crtc[0x13] != 0 {
		offset = uint32(m.crtc[0x13]) * 2
	}
	ch4 := m.chain4()
	for y := 0; y < 200; y++ {
		scan := y
		if dbl {
			scan = y * 2
		}
		var row uint32
		if scan > lc && lc < 0x3ff {
			if dbl {
				row = uint32((scan-lc-1)/2) * offset
			} else {
				row = uint32(scan-lc-1) * offset
			}
		} else {
			row = start + uint32(y)*offset
		}
		out := dst[y*320 : y*320+320]
		for x := 0; x < 320; x++ {
			if ch4 {
				a := (row*4 + uint32(x)) & 0xffff
				out[x] = m.vram[a&3][a>>2&0x3fff]
			} else {
				a := (row + uint32(x>>2)) & 0xffff
				out[x] = m.vram[x&3][a]
			}
		}
	}
}

// ---------------------------------------------------------------- mouse

func (m *Machine) int33() {
	r := &m.cpu.r
	clamp := func() {
		m.mouseX = min(max(m.mouseX, float64(m.mouseMinX)), float64(m.mouseMaxX))
		m.mouseY = min(max(m.mouseY, float64(m.mouseMinY)), float64(m.mouseMaxY))
	}
	switch r[rAX] {
	case 0x00, 0x21: // reset
		m.mouseMinX, m.mouseMaxX, m.mouseMinY, m.mouseMaxY = 0, 639, 0, 199
		m.mouseX, m.mouseY = 320, 100
		r[rAX], r[rBX] = 0xffff, 2
	case 0x03:
		r[rBX] = m.mouseButtons
		r[rCX], r[rDX] = uint16(int(m.mouseX)), uint16(int(m.mouseY))
	case 0x04:
		m.mouseX, m.mouseY = float64(int16(r[rCX])), float64(int16(r[rDX]))
		clamp()
	case 0x07:
		m.mouseMinX, m.mouseMaxX = int(int16(r[rCX])), int(int16(r[rDX]))
		if m.mouseMinX > m.mouseMaxX {
			m.mouseMinX, m.mouseMaxX = m.mouseMaxX, m.mouseMinX
		}
		clamp()
	case 0x08:
		m.mouseMinY, m.mouseMaxY = int(int16(r[rCX])), int(int16(r[rDX]))
		if m.mouseMinY > m.mouseMaxY {
			m.mouseMinY, m.mouseMaxY = m.mouseMaxY, m.mouseMinY
		}
		clamp()
	case 0x0b:
		r[rCX], r[rDX] = uint16(int16(m.mouseMickX)), uint16(int16(m.mouseMickY))
		m.mouseMickX, m.mouseMickY = 0, 0
	case 0x24:
		r[rBX], r[rCX] = 0x0800, 0x0400
	}
}

// MouseMove applies relative host mouse motion.
func (m *Machine) MouseMove(dx, dy float64) {
	k := m.MouseScale
	if k == 0 {
		k = 0.5
	}
	m.mouseX += dx * k
	m.mouseY += dy * k
	m.mouseMickX += dx
	m.mouseMickY += dy
	if m.mouseMaxX > 0 || m.mouseMaxY > 0 {
		m.mouseX = min(max(m.mouseX, float64(m.mouseMinX)), float64(m.mouseMaxX))
		m.mouseY = min(max(m.mouseY, float64(m.mouseMinY)), float64(m.mouseMaxY))
	}
}

// MouseButton sets button b (0 left, 1 right) state.
func (m *Machine) MouseButton(b int, down bool) {
	if down {
		m.mouseButtons |= 1 << b
	} else {
		m.mouseButtons &^= 1 << b
	}
}

func fatal(f string, a ...any) {
	fmt.Fprintf(os.Stderr, f+"\n", a...)
	os.Exit(1)
}

var cpuHzF = float64(CPUHz)

// ------------------------------------------------------------ DOS memory

// memBlock is a DOS allocation: usable paragraphs [seg, seg+size), with the
// paragraph before it standing in for the MCB.
type memBlock struct{ seg, size uint16 }

// dosAlloc is first-fit over the heap; it returns 0 and the largest free block
// when nothing fits. (Before 7.4 frees were ignored, and the game, which
// allocates and frees a buffer every time it loads a song, ran out of memory
// after a few dozen songs: the music stopped.)
func (m *Machine) dosAlloc(need uint16) (seg, largest uint16) {
	cur := uint32(m.allocNxt)
	place := func(end uint32) bool {
		if end > cur && end-cur >= uint32(need)+1 {
			return true
		}
		if end > cur+1 && uint16(end-cur-1) > largest {
			largest = uint16(end - cur - 1)
		}
		return false
	}
	for i, b := range m.blocks {
		if place(uint32(b.seg) - 1) {
			s := uint16(cur + 1)
			m.blocks = append(m.blocks[:i], append([]memBlock{{s, need}}, m.blocks[i:]...)...)
			return s, 0
		}
		cur = max(cur, uint32(b.seg)+uint32(b.size))
	}
	if place(uint32(m.memTop)) {
		s := uint16(cur + 1)
		m.blocks = append(m.blocks, memBlock{s, need})
		return s, 0
	}
	return 0, largest
}

func (m *Machine) dosFree(seg uint16) bool {
	for i, b := range m.blocks {
		if b.seg == seg {
			m.blocks = append(m.blocks[:i], m.blocks[i+1:]...)
			return true
		}
	}
	return false
}

func (m *Machine) dosResize(seg, size uint16) (uint16, bool) {
	for i, b := range m.blocks {
		if b.seg != seg {
			continue
		}
		end := uint32(m.memTop)
		if i+1 < len(m.blocks) {
			end = uint32(m.blocks[i+1].seg) - 1
		}
		if uint32(seg)+uint32(size) > end {
			return uint16(end - uint32(seg)), false
		}
		m.blocks[i].size = size
		return 0, true
	}
	return 0, false
}
