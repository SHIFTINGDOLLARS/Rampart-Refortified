package main

// Sound Blaster 2.0 emulation (DSP 2.01 at 0x220, IRQ 7, DMA 1) with an OPL2 at
// 0x388 and 0x228, plus the 8237 DMA controller it needs. Output is mono
// 16-bit at SampleRate, generated in lockstep with emulated time.

const SampleRate = 44100

type dmaChan struct {
	baseAddr, curAddr   uint16
	baseCount, curCount uint16
	page                uint8
	mode                uint8
	masked              bool
}

type SoundBlaster struct {
	KeyOns, OPLWrites int      // statistics (tests)
	Voice             *Captain // Yappy Captain lines, mixed in
	opl               *OPL2
	oplIndex          uint8

	// DSP
	resetBit   bool
	rdQueue    []uint8
	lastRead   uint8
	cmd        uint8
	params     []uint8
	needParams int
	timeConst  uint8
	blockSize  uint16 // for auto-init / high-speed (0x48)
	playing    bool
	autoInit   bool
	exitAuto   bool
	paused     bool
	silence    bool
	blockLen   int
	blockLeft  int
	speaker    bool
	sample     uint8
	pos        float64
	irq8       bool
	testReg    uint8

	// DMA controller
	dma      [4]dmaChan
	flipflop bool
	dmaTC    uint8

	// output
	sampleIdx uint64
	buf       []int16
	hpPrevIn  float64
	hpPrevOut float64
	ioPenalty uint64
	Gain      float64

	// PC speaker
	spkLastT   uint64  // icount up to which spkAccum is integrated
	spkAccum   float64 // ∫ level dt (in instructions) since the last output sample
	spkSampleT uint64  // icount of the last output sample
	spkPhaseT0 uint64  // icount where the PIT channel 2 square wave phase restarted
	spkUsed    bool
}

const sbBase = 0x220

// The card follows whatever IRQ/DMA the game's setup screen chose (DS:6C17 / DS:6C19),
// so changing them in-game keeps sound working. Defaults: IRQ 7, DMA 1.
const gameDS = 0x165e0

func (s *SoundBlaster) irqLine(m *Machine) int {
	v := int(m.mem[gameDS+0x6c17])
	if v >= 2 && v <= 7 {
		return v
	}
	return 7
}

func (s *SoundBlaster) dmaChan(m *Machine) int {
	v := int(m.mem[gameDS+0x6c19])
	if v >= 0 && v <= 3 {
		return v
	}
	return 1
}

func NewSoundBlaster() *SoundBlaster {
	return &SoundBlaster{opl: NewOPL2(SampleRate), timeConst: 0xA6, sample: 128, ioPenalty: CPUHz / 1_000_000, Gain: 1}
}

func (s *SoundBlaster) nowUS(m *Machine) float64 { return float64(m.cpu.icount) * 1e6 / cpuHzF }

func isSoundPort(p uint16) bool {
	return (p >= sbBase && p <= sbBase+0xF) || p == 0x388 || p == 0x389 || p <= 0x0F ||
		p == 0x81 || p == 0x82 || p == 0x83 || p == 0x87
}

var sndLog = 0

func (s *SoundBlaster) PortIn(m *Machine, p uint16) (uint8, bool) {
	if !isSoundPort(p) {
		return 0, false
	}
	if sndLog > 0 {
		v, ok := s.portIn(m, p)
		sndLog--
		m.logf("IN  %03x -> %02x  @%04x:%04x", p, v, m.cpu.sr[sCS], m.cpu.ip)
		return v, ok
	}
	return s.portIn(m, p)
}

func (s *SoundBlaster) portIn(m *Machine, p uint16) (uint8, bool) {
	m.cpu.icount += s.ioPenalty
	switch p {
	case 0x388, sbBase + 8:
		return s.opl.Status(s.nowUS(m)), true
	case 0x389, sbBase + 9:
		return 0xff, true
	case sbBase + 0xA:
		if len(s.rdQueue) > 0 {
			s.lastRead = s.rdQueue[0]
			s.rdQueue = s.rdQueue[1:]
		}
		return s.lastRead, true
	case sbBase + 0xC:
		return 0x7F, true // write buffer always ready
	case sbBase + 0xE:
		s.irq8 = false
		if len(s.rdQueue) > 0 {
			return 0xFF, true
		}
		return 0x7F, true
	case 0x08:
		v := s.dmaTC
		s.dmaTC = 0
		return v, true
	}
	if p <= 0x07 {
		ch := &s.dma[p>>1]
		var v uint16
		if p&1 == 0 {
			v = ch.curAddr
		} else {
			v = ch.curCount
		}
		s.flipflop = !s.flipflop
		if s.flipflop {
			return uint8(v), true
		}
		return uint8(v >> 8), true
	}
	if p >= sbBase && p <= sbBase+0xF {
		return 0xff, true
	}
	return 0xff, true
}

func (s *SoundBlaster) PortOut(m *Machine, p uint16, v uint8) bool {
	if p == 0x61 || p == 0x42 || p == 0x43 {
		if sndLog > 0 {
			sndLog--
			m.logf("SPK %03x <- %02x  t=%.4f", p, v, float64(m.cpu.icount)/cpuHzF)
		}
		s.speakerWrite(m, p, v) // observe only; the machine applies the write
		return false
	}
	if !isSoundPort(p) {
		return false
	}
	if sndLog > 0 && !(p == 0x388 || p == 0x389 || p == sbBase+8 || p == sbBase+9) {
		sndLog--
		m.logf("OUT %03x <- %02x  t=%.4f", p, v, float64(m.cpu.icount)/cpuHzF)
	}
	m.cpu.icount += s.ioPenalty
	s.catchUp(m) // register changes take effect at the right sample
	switch p {
	case 0x388, sbBase + 8:
		s.oplIndex = v
	case 0x389, sbBase + 9:
		if s.oplIndex >= 0xb0 && s.oplIndex <= 0xb8 && v&0x20 != 0 {
			s.KeyOns++
		}
		s.OPLWrites++
		s.opl.Write(s.oplIndex, v, s.nowUS(m))
	case sbBase + 6:
		if v&1 != 0 {
			s.resetBit = true
		} else if s.resetBit {
			s.resetBit = false
			s.dspReset()
		}
	case sbBase + 0xC:
		s.dspWrite(m, v)
	case 0x0A:
		s.dma[v&3].masked = v&4 != 0
	case 0x0B:
		s.dma[v&3].mode = v
	case 0x0C:
		s.flipflop = false
	case 0x0D:
		s.flipflop = false
		for i := range s.dma {
			s.dma[i].masked = true
		}
	case 0x0F:
		for i := range s.dma {
			s.dma[i].masked = v&(1<<i) != 0
		}
	case 0x87:
		s.dma[0].page = v
	case 0x83:
		s.dma[1].page = v
	case 0x81:
		s.dma[2].page = v
	case 0x82:
		s.dma[3].page = v
	default:
		if p <= 0x07 {
			ch := &s.dma[p>>1]
			lo := !s.flipflop
			s.flipflop = !s.flipflop
			if p&1 == 0 {
				if lo {
					ch.baseAddr = ch.baseAddr&0xff00 | uint16(v)
				} else {
					ch.baseAddr = ch.baseAddr&0xff | uint16(v)<<8
				}
				ch.curAddr = ch.baseAddr
			} else {
				if lo {
					ch.baseCount = ch.baseCount&0xff00 | uint16(v)
				} else {
					ch.baseCount = ch.baseCount&0xff | uint16(v)<<8
				}
				ch.curCount = ch.baseCount
			}
		}
	}
	return true
}

func (s *SoundBlaster) dspReset() {
	s.playing, s.paused, s.autoInit, s.exitAuto, s.silence = false, false, false, false, false
	s.rdQueue = []uint8{0xAA}
	s.needParams = 0
	s.cmd = 0
	s.sample = 128
}

var dspParamCount = map[uint8]int{
	0x10: 1, 0x14: 2, 0x16: 2, 0x17: 2, 0x24: 2, 0x38: 1, 0x40: 1, 0x48: 2,
	0x74: 2, 0x75: 2, 0x76: 2, 0x77: 2, 0x80: 2, 0xE0: 1, 0xE4: 1,
}

func (s *SoundBlaster) dspWrite(m *Machine, v uint8) {
	if s.needParams > 0 {
		s.params = append(s.params, v)
		s.needParams--
		if s.needParams == 0 {
			s.dspExec(m)
		}
		return
	}
	s.cmd = v
	s.params = s.params[:0]
	if n, ok := dspParamCount[v]; ok {
		s.needParams = n
		return
	}
	s.dspExec(m)
}

func (s *SoundBlaster) startDMA(length int, auto bool) {
	s.playing = true
	s.paused = false
	s.silence = false
	s.autoInit = auto
	s.exitAuto = false
	s.blockLen = length
	s.blockLeft = length
}

func (s *SoundBlaster) dspExec(m *Machine) {
	p := s.params
	w := func() int { return int(p[0]) | int(p[1])<<8 }
	switch s.cmd {
	case 0x10:
		s.sample = p[0]
	case 0x14, 0x16, 0x17, 0x74, 0x75, 0x76, 0x77:
		s.startDMA(w()+1, false)
	case 0x1C, 0x1F, 0x7D, 0x7F:
		s.startDMA(int(s.blockSize)+1, true)
	case 0x90:
		s.startDMA(int(s.blockSize)+1, true)
	case 0x91:
		s.startDMA(int(s.blockSize)+1, false)
	case 0x80:
		s.startDMA(w()+1, false)
		s.silence = true
	case 0x40:
		s.timeConst = p[0]
	case 0x48:
		s.blockSize = uint16(w())
	case 0xD0:
		s.paused = true
	case 0xD4:
		s.paused = false
	case 0xDA:
		s.exitAuto = true
	case 0xD1:
		s.speaker = true
	case 0xD3:
		s.speaker = false
	case 0xD8:
		if s.speaker {
			s.rdQueue = append(s.rdQueue, 0xFF)
		} else {
			s.rdQueue = append(s.rdQueue, 0x00)
		}
	case 0xE0:
		s.rdQueue = append(s.rdQueue, ^p[0])
	case 0xE1:
		s.rdQueue = append(s.rdQueue, 0x02, 0x01)
	case 0xE4:
		s.testReg = p[0]
	case 0xE8:
		s.rdQueue = append(s.rdQueue, s.testReg)
	case 0x20:
		s.rdQueue = append(s.rdQueue, 0x80)
	case 0xF2:
		s.irq8 = true
		m.RaiseIRQ(s.irqLine(m))
	case 0x24, 0x2C, 0x98, 0x99:
		// recording: not supported
	}
}

// dmaRead fetches one byte from DMA channel n; returns false if the channel is masked.
func (s *SoundBlaster) dmaRead(m *Machine, n int) (uint8, bool) {
	ch := &s.dma[n]
	if ch.masked {
		return 0, false
	}
	a := uint32(ch.page)<<16 | uint32(ch.curAddr)
	v := m.mem[a&0xFFFFF]
	if ch.mode&0x20 != 0 {
		ch.curAddr--
	} else {
		ch.curAddr++
	}
	ch.curCount--
	if ch.curCount == 0xFFFF { // terminal count
		s.dmaTC |= 1 << n
		if ch.mode&0x10 != 0 {
			ch.curAddr, ch.curCount = ch.baseAddr, ch.baseCount
		} else {
			ch.masked = true
		}
	}
	return v, true
}

func (s *SoundBlaster) rate() float64 {
	return 1e6 / float64(256-int(s.timeConst))
}

func (s *SoundBlaster) genSample(m *Machine) {
	if s.playing && !s.paused {
		s.pos += s.rate() / SampleRate
		for s.pos >= 1 && s.playing {
			s.pos--
			if !s.silence {
				if b, ok := s.dmaRead(m, s.dmaChan(m)); ok {
					s.sample = b
				} else {
					s.pos = 0
					break
				}
			} else {
				s.sample = 128
			}
			s.blockLeft--
			if s.blockLeft <= 0 {
				s.irq8 = true
				m.RaiseIRQ(s.irqLine(m))
				if s.autoInit && !s.exitAuto {
					s.blockLeft = s.blockLen
				} else {
					s.playing = false
				}
			}
		}
	}
	mix := s.opl.Sample()*0.18 + s.speakerSample(m)*0.22
	if s.speaker {
		mix += (float64(s.sample) - 128) / 128 * 0.6
	}
	mix += s.Voice.Sample()
	// DC blocker (the real card's output is AC-coupled)
	out := mix - s.hpPrevIn + 0.9985*s.hpPrevOut
	s.hpPrevIn, s.hpPrevOut = mix, out
	v := out * 32767 * s.Gain
	if v > 32767 {
		v = 32767
	} else if v < -32768 {
		v = -32768
	}
	s.buf = append(s.buf, int16(v))
}

func (s *SoundBlaster) sampleDue(i uint64) uint64 { return i * CPUHz / SampleRate }

func (s *SoundBlaster) catchUp(m *Machine) {
	for m.cpu.icount >= s.sampleDue(s.sampleIdx+1) {
		s.sampleIdx++
		s.genSample(m)
	}
	m.soundDue = s.sampleDue(s.sampleIdx + 1)
}

func (s *SoundBlaster) Tick(m *Machine) { s.catchUp(m) }

// TakeSamples returns and clears the samples generated so far.
func (s *SoundBlaster) TakeSamples() []int16 {
	out := s.buf
	s.buf = make([]int16, 0, 1024)
	return out
}

// ---------------------------------------------------------------- PC speaker

// speaker level integrated over [a, b] (instruction counts) for the current port 61 / PIT2 state
func (s *SoundBlaster) spkIntegral(m *Machine, a, b uint64) float64 {
	if b <= a || m.port61&2 == 0 {
		return 0
	}
	dt := float64(b - a)
	if m.port61&1 == 0 { // gate low: channel 2 output stays high, speaker follows bit 1
		return dt
	}
	div := float64(m.Pit2Div)
	if div == 0 {
		div = 65536
	}
	f := 1193182.0 / div / cpuHzF // cycles per instruction
	if f*cpuHzF > 18000 {
		return dt * 0.5
	}
	F := func(t uint64) float64 {
		x := float64(t-s.spkPhaseT0) * f
		fl := x - float64(int64(x))
		h := fl
		if h > 0.5 {
			h = 0.5
		}
		return float64(int64(x))*0.5 + h
	}
	if a < s.spkPhaseT0 {
		a = s.spkPhaseT0
	}
	return (F(b) - F(a)) / f
}

func (s *SoundBlaster) spkAdvance(m *Machine, now uint64) {
	if now > s.spkLastT {
		s.spkAccum += s.spkIntegral(m, s.spkLastT, now)
		s.spkLastT = now
	}
}

func (s *SoundBlaster) speakerWrite(m *Machine, p uint16, v uint8) {
	s.catchUp(m)
	s.spkAdvance(m, m.cpu.icount)
	switch p {
	case 0x61:
		if v&1 != 0 && m.port61&1 == 0 {
			s.spkPhaseT0 = m.cpu.icount
		}
	case 0x42:
		s.spkPhaseT0 = m.cpu.icount
	}
}

// speakerSample returns the speaker's average output over the last sample period, −1..1.
func (s *SoundBlaster) speakerSample(m *Machine) float64 {
	t := s.sampleDue(s.sampleIdx)
	s.spkAdvance(m, t)
	span := float64(t - s.spkSampleT)
	s.spkSampleT = t
	if m.port61&2 != 0 {
		s.spkUsed = true
	}
	if span <= 0 || !s.spkUsed {
		s.spkAccum = 0
		return 0
	}
	avg := s.spkAccum / span
	s.spkAccum = 0
	return avg*2 - 1
}
