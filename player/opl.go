package main

import "math"

// OPL2 (YM3812) FM synthesizer, written from the chip's documented behaviour:
// 9 two-operator channels, 4 waveforms, ADSR envelopes with key scaling,
// tremolo/vibrato, feedback, and the two status timers used for detection.
// Output is generated at the host sample rate.

const oplClock = 49716.0 // internal sample rate of the real chip

var oplMult = [16]float64{0.5, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 10, 12, 12, 15, 15}

// KSL attenuation (dB) at block 7 indexed by the top 4 bits of F-number; −6 dB per lower block.
var oplKSLTab = [16]float64{0, 9, 12, 13.875, 15, 16.125, 16.875, 17.625, 18, 18.75, 19.125, 19.5, 19.875, 20.25, 20.625, 21}

const (
	egOff = iota
	egAttack
	egDecay
	egSustain
	egRelease
)

type oplOp struct {
	am, vib, egt, ksr bool
	mult              uint8
	ksl, tl           uint8
	ar, dr, sl, rr    uint8
	ws                uint8

	phase   float64 // in cycles
	env     float64 // attenuation in dB (0 = loudest, 96 = silent)
	state   int
	out     float64
	prevOut float64
}

type oplCh struct {
	fnum  uint16
	block uint8
	key   bool
	fb    uint8
	con   uint8
	op    [2]oplOp
}

type OPL2 struct {
	reg      [256]uint8
	ch       [9]oplCh
	rate     float64
	waveSel  bool
	amDepth  bool
	vibDepth bool
	rhythm   bool
	nts      bool
	amPhase  float64
	vibPhase float64
	noise    uint32
	// timers (in microseconds of emulated time)
	t1Start, t2Start float64
	t1On, t2On       bool
	status           uint8
	index            uint8
}

func NewOPL2(rate float64) *OPL2 {
	o := &OPL2{rate: rate, noise: 1}
	for c := range o.ch {
		for k := range o.ch[c].op {
			o.ch[c].op[k].env = 96
		}
	}
	return o
}

// slot -> (channel, operator) for register offsets 0x00..0x15
func oplSlot(off uint8) (int, int, bool) {
	grp := int(off >> 3)
	idx := int(off & 7)
	if grp > 2 || idx > 5 {
		return 0, 0, false
	}
	ch := grp*3 + idx%3
	return ch, idx / 3, true
}

func (o *OPL2) Write(r uint8, v uint8, nowUS float64) {
	o.updateTimers(nowUS)
	o.reg[r] = v
	switch {
	case r == 0x01:
		o.waveSel = v&0x20 != 0
	case r == 0x02, r == 0x03:
		// timer preset values; latched in reg[]
	case r == 0x04:
		if v&0x80 != 0 { // reset IRQ flags
			o.status = 0
			return
		}
		if v&0x01 != 0 && !o.t1On {
			o.t1Start = nowUS
		}
		if v&0x02 != 0 && !o.t2On {
			o.t2Start = nowUS
		}
		o.t1On = v&0x01 != 0
		o.t2On = v&0x02 != 0
	case r == 0x08:
		o.nts = v&0x40 != 0
	case r >= 0x20 && r <= 0x35:
		if c, k, ok := oplSlot(r - 0x20); ok {
			op := &o.ch[c].op[k]
			op.am, op.vib, op.egt, op.ksr = v&0x80 != 0, v&0x40 != 0, v&0x20 != 0, v&0x10 != 0
			op.mult = v & 15
		}
	case r >= 0x40 && r <= 0x55:
		if c, k, ok := oplSlot(r - 0x40); ok {
			op := &o.ch[c].op[k]
			op.ksl, op.tl = v>>6, v&63
		}
	case r >= 0x60 && r <= 0x75:
		if c, k, ok := oplSlot(r - 0x60); ok {
			op := &o.ch[c].op[k]
			op.ar, op.dr = v>>4, v&15
		}
	case r >= 0x80 && r <= 0x95:
		if c, k, ok := oplSlot(r - 0x80); ok {
			op := &o.ch[c].op[k]
			op.sl, op.rr = v>>4, v&15
		}
	case r >= 0xa0 && r <= 0xa8:
		ch := &o.ch[r-0xa0]
		ch.fnum = ch.fnum&0x300 | uint16(v)
	case r >= 0xb0 && r <= 0xb8:
		ch := &o.ch[r-0xb0]
		ch.fnum = ch.fnum&0xff | uint16(v&3)<<8
		ch.block = (v >> 2) & 7
		key := v&0x20 != 0
		if key && !ch.key {
			o.keyOn(ch)
		} else if !key && ch.key {
			o.keyOff(ch)
		}
		ch.key = key
	case r == 0xbd:
		o.amDepth = v&0x80 != 0
		o.vibDepth = v&0x40 != 0
		o.rhythm = v&0x20 != 0
		if o.rhythm {
			// percussion key-ons: BD ch6, SD ch7 op2, TT ch8 op1, CY ch8 op2, HH ch7 op1
			o.percKey(&o.ch[6].op[0], v&0x10 != 0)
			o.percKey(&o.ch[6].op[1], v&0x10 != 0)
			o.percKey(&o.ch[7].op[1], v&0x08 != 0)
			o.percKey(&o.ch[8].op[0], v&0x04 != 0)
			o.percKey(&o.ch[8].op[1], v&0x02 != 0)
			o.percKey(&o.ch[7].op[0], v&0x01 != 0)
		}
	case r >= 0xc0 && r <= 0xc8:
		ch := &o.ch[r-0xc0]
		ch.fb = (v >> 1) & 7
		ch.con = v & 1
	case r >= 0xe0 && r <= 0xf5:
		if c, k, ok := oplSlot(r - 0xe0); ok {
			o.ch[c].op[k].ws = v & 3
		}
	}
}

func (o *OPL2) percKey(op *oplOp, on bool) {
	if on && (op.state == egOff || op.state == egRelease) {
		op.state = egAttack
		op.phase = 0
	} else if !on && op.state != egOff {
		op.state = egRelease
	}
}

func (o *OPL2) keyOn(ch *oplCh) {
	for k := range ch.op {
		op := &ch.op[k]
		op.state = egAttack
		op.phase = 0
	}
}

func (o *OPL2) keyOff(ch *oplCh) {
	for k := range ch.op {
		if ch.op[k].state != egOff {
			ch.op[k].state = egRelease
		}
	}
}

func (o *OPL2) updateTimers(nowUS float64) {
	if o.t1On && o.reg[4]&0x40 == 0 {
		period := float64(256-int(o.reg[2])) * 80
		if nowUS-o.t1Start >= period {
			o.status |= 0xC0
		}
	}
	if o.t2On && o.reg[4]&0x20 == 0 {
		period := float64(256-int(o.reg[3])) * 320
		if nowUS-o.t2Start >= period {
			o.status |= 0xA0
		}
	}
}

func (o *OPL2) Status(nowUS float64) uint8 {
	o.updateTimers(nowUS)
	return o.status | 0x06 // OPL2 reports bits 1-2 set on many boards; detection masks with 0xE0
}

// effective envelope rate (0..63) for a 4-bit rate value
func (o *OPL2) effRate(ch *oplCh, op *oplOp, r uint8) int {
	if r == 0 {
		return 0
	}
	ksn := int(ch.block) << 1
	if o.nts {
		ksn |= int(ch.fnum>>8) & 1
	} else {
		ksn |= int(ch.fnum>>9) & 1
	}
	if !op.ksr {
		ksn >>= 2
	}
	a := int(r)*4 + ksn
	if a > 63 {
		a = 63
	}
	return a
}

func rateTime(base float64, a int) float64 { // seconds
	return base / 1000 * math.Pow(2, float64(1-a/4)) * 4 / float64(4+a&3)
}

func (o *OPL2) stepEnv(ch *oplCh, op *oplOp, dt float64) {
	switch op.state {
	case egAttack:
		a := o.effRate(ch, op, op.ar)
		if a == 0 {
			return
		}
		if a >= 60 {
			op.env = 0
			op.state = egDecay
			return
		}
		t := rateTime(2826.24, a)
		k := math.Log(512) / t
		op.env *= math.Exp(-k * dt)
		if op.env < 0.1875 {
			op.env = 0
			op.state = egDecay
		}
	case egDecay:
		sl := float64(op.sl) * 3
		if op.sl == 15 {
			sl = 93
		}
		a := o.effRate(ch, op, op.dr)
		if a > 0 {
			op.env += 96 / rateTime(39280, a) * dt
		}
		if op.env >= sl {
			op.env = sl
			op.state = egSustain
		}
	case egSustain:
		if !op.egt { // percussive: keep decaying at the release rate
			a := o.effRate(ch, op, op.rr)
			if a > 0 {
				op.env += 96 / rateTime(39280, a) * dt
			}
		}
	case egRelease:
		a := o.effRate(ch, op, op.rr)
		if a > 0 {
			op.env += 96 / rateTime(39280, a) * dt
		}
	}
	if op.env >= 96 {
		op.env = 96
		if op.state == egRelease || (op.state == egSustain && !op.egt) {
			op.state = egOff
		}
	}
}

func oplWave(ws uint8, ph float64) float64 {
	ph -= math.Floor(ph)
	s := math.Sin(2 * math.Pi * ph)
	switch ws {
	case 1:
		if s < 0 {
			return 0
		}
	case 2:
		return math.Abs(s)
	case 3:
		if math.Mod(ph, 0.5) >= 0.25 {
			return 0
		}
		return math.Abs(s)
	}
	return s
}

func (o *OPL2) opOut(ch *oplCh, op *oplOp, mod float64, amDB, vibMul float64) float64 {
	if op.state == egOff {
		op.prevOut, op.out = op.out, 0
		return 0
	}
	freq := float64(uint32(ch.fnum)<<ch.block) * oplClock / 1048576.0 * oplMult[op.mult]
	if op.vib {
		freq *= vibMul
	}
	ws := uint8(0)
	if o.waveSel {
		ws = op.ws
	}
	att := op.env + float64(op.tl)*0.75
	if op.ksl != 0 {
		k := oplKSLTab[ch.fnum>>6] - 6*float64(7-ch.block)
		if k > 0 {
			switch op.ksl {
			case 1:
				att += k * 0.5
			case 2:
				att += k * 0.25
			case 3:
				att += k
			}
		}
	}
	if op.am {
		att += amDB
	}
	var v float64
	if att < 96 {
		v = oplWave(ws, op.phase+mod) * math.Exp(-att*(math.Ln10/20))
	}
	op.phase += freq / o.rate
	if op.phase > 1e6 {
		op.phase -= math.Floor(op.phase)
	}
	op.prevOut, op.out = op.out, v
	return v
}

// Sample generates one output sample (roughly ±1.0 per loud channel).
func (o *OPL2) Sample() float64 {
	dt := 1 / o.rate
	o.amPhase += 3.7 * dt
	o.vibPhase += 6.07 * dt
	tri := func(p float64) float64 { p -= math.Floor(p); return 1 - math.Abs(2*p-1) } // 0..1..0
	amMax, cents := 1.0, 7.0
	if o.amDepth {
		amMax = 4.8
	}
	if o.vibDepth {
		cents = 14
	}
	amDB := tri(o.amPhase) * amMax
	vibMul := math.Pow(2, (tri(o.vibPhase)*2-1)*cents/1200)

	sum := 0.0
	last := 9
	if o.rhythm {
		last = 6
	}
	for c := 0; c < last; c++ {
		ch := &o.ch[c]
		m, car := &ch.op[0], &ch.op[1]
		o.stepEnv(ch, m, dt)
		o.stepEnv(ch, car, dt)
		fbMod := 0.0
		if ch.fb > 0 {
			fbMod = (m.out + m.prevOut) * 4095 / math.Pow(2, float64(9-ch.fb)) / 1024
		}
		mo := o.opOut(ch, m, fbMod, amDB, vibMul)
		if ch.con == 0 {
			sum += o.opOut(ch, car, mo*4095/1024, amDB, vibMul)
		} else {
			sum += mo + o.opOut(ch, car, 0, amDB, vibMul)
		}
	}
	if o.rhythm {
		sum += o.rhythmSample(dt, amDB, vibMul)
	}
	return sum
}

// Simplified percussion: bass drum is a normal 2-op voice; the other four use
// their operator envelopes over tone (TT) or noise-modulated tone (SD, HH, CY).
func (o *OPL2) rhythmSample(dt, amDB, vibMul float64) float64 {
	sum := 0.0
	ch6, ch7, ch8 := &o.ch[6], &o.ch[7], &o.ch[8]
	for i := range ch6.op {
		o.stepEnv(ch6, &ch6.op[i], dt)
	}
	fb := 0.0
	if ch6.fb > 0 {
		fb = (ch6.op[0].out + ch6.op[0].prevOut) * 4095 / math.Pow(2, float64(9-ch6.fb)) / 1024
	}
	bdMod := o.opOut(ch6, &ch6.op[0], fb, amDB, vibMul)
	if ch6.con == 1 {
		bdMod = 0
	}
	sum += 2 * o.opOut(ch6, &ch6.op[1], bdMod*4095/1024, amDB, vibMul)

	o.noise ^= o.noise << 13
	o.noise ^= o.noise >> 17
	o.noise ^= o.noise << 5
	noise := float64(int32(o.noise)) / 2147483648.0

	for _, p := range []struct {
		ch    *oplCh
		k     int
		noisy float64
	}{{ch7, 0, 0.7}, {ch7, 1, 0.5}, {ch8, 0, 0}, {ch8, 1, 0.8}} {
		op := &p.ch.op[p.k]
		o.stepEnv(p.ch, op, dt)
		t := o.opOut(p.ch, op, 0, amDB, vibMul)
		sum += 2 * (t*(1-p.noisy) + noise*math.Abs(t)*p.noisy*1.5)
	}
	return sum
}
