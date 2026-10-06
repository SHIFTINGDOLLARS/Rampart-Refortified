package main

// 8086/80186 interpreter. A direct port of emu/emu.c, which has been validated
// against the real game (see NOTES.md).

const (
	rAX = iota
	rCX
	rDX
	rBX
	rSP
	rBP
	rSI
	rDI
)
const (
	sES = iota
	sCS
	sSS
	sDS
)
const (
	fCF = 0x001
	fPF = 0x004
	fAF = 0x010
	fZF = 0x040
	fSF = 0x080
	fTF = 0x100
	fIF = 0x200
	fDF = 0x400
	fOF = 0x800
)

var parityTab [256]bool

func init() {
	for i := 0; i < 256; i++ {
		c := 0
		for b := i; b != 0; b >>= 1 {
			c += b & 1
		}
		parityTab[i] = c%2 == 0
	}
}

type CPU struct {
	r      [8]uint16
	sr     [4]uint16
	ip     uint16
	flags  uint16
	segOvr int
	rep    int
	// modrm
	mod, reg, rm int
	ea           uint32
	eaIsReg      bool

	icount uint64
	halted int
	m      *Machine
}

func (c *CPU) get8(i int) uint8 {
	if i < 4 {
		return uint8(c.r[i])
	}
	return uint8(c.r[i-4] >> 8)
}
func (c *CPU) set8(i int, v uint8) {
	if i < 4 {
		c.r[i] = c.r[i]&0xff00 | uint16(v)
	} else {
		c.r[i-4] = c.r[i-4]&0x00ff | uint16(v)<<8
	}
}

func (c *CPU) lin(s int, o uint16) uint32 { return uint32(c.sr[s])<<4 + uint32(o) }
func (c *CPU) fetch8() uint8 {
	v := c.m.rb(c.lin(sCS, c.ip))
	c.ip++
	return v
}
func (c *CPU) fetch16() uint16 {
	v := c.m.rw(c.lin(sCS, c.ip))
	c.ip += 2
	return v
}
func (c *CPU) push(v uint16) {
	c.r[rSP] -= 2
	c.m.ww(c.lin(sSS, c.r[rSP]), v)
}
func (c *CPU) pop() uint16 {
	v := c.m.rw(c.lin(sSS, c.r[rSP]))
	c.r[rSP] += 2
	return v
}

func (c *CPU) setf(f uint16, on bool) {
	if on {
		c.flags |= f
	} else {
		c.flags &^= f
	}
}
func (c *CPU) szp8(v uint8) {
	c.setf(fZF, v == 0)
	c.setf(fSF, v&0x80 != 0)
	c.setf(fPF, parityTab[v])
}
func (c *CPU) szp16(v uint16) {
	c.setf(fZF, v == 0)
	c.setf(fSF, v&0x8000 != 0)
	c.setf(fPF, parityTab[v&0xff])
}

// alu: 0 add 1 or 2 adc 3 sbb 4 and 5 sub 6 xor 7 cmp
func (c *CPU) alu(op int, a, b uint32, w bool) uint32 {
	var m, sb uint32 = 0xff, 0x80
	if w {
		m, sb = 0xffff, 0x8000
	}
	carry := uint32(c.flags & fCF)
	var res uint32
	switch op {
	case 0, 2:
		if op == 0 {
			carry = 0
		}
		res = a + b + carry
		c.setf(fCF, res > m)
		c.setf(fOF, (res^a)&(res^b)&sb != 0)
		c.setf(fAF, (a^b^res)&0x10 != 0)
	case 3, 5, 7:
		if op != 3 {
			carry = 0
		}
		res = a - b - carry
		c.setf(fCF, a < b+carry)
		c.setf(fOF, (a^b)&(a^res)&sb != 0)
		c.setf(fAF, (a^b^res)&0x10 != 0)
	case 1:
		res = a | b
		c.flags &^= fCF | fOF | fAF
	case 4:
		res = a & b
		c.flags &^= fCF | fOF | fAF
	case 6:
		res = a ^ b
		c.flags &^= fCF | fOF | fAF
	}
	res &= m
	if w {
		c.szp16(uint16(res))
	} else {
		c.szp8(uint8(res))
	}
	return res
}

func (c *CPU) decodeModrm() {
	mb := c.fetch8()
	c.mod, c.reg, c.rm = int(mb>>6), int(mb>>3)&7, int(mb&7)
	if c.mod == 3 {
		c.eaIsReg = true
		return
	}
	c.eaIsReg = false
	var off uint16
	defseg := sDS
	r := &c.r
	switch c.rm {
	case 0:
		off = r[rBX] + r[rSI]
	case 1:
		off = r[rBX] + r[rDI]
	case 2:
		off = r[rBP] + r[rSI]
		defseg = sSS
	case 3:
		off = r[rBP] + r[rDI]
		defseg = sSS
	case 4:
		off = r[rSI]
	case 5:
		off = r[rDI]
	case 6:
		if c.mod == 0 {
			off = c.fetch16()
		} else {
			off = r[rBP]
			defseg = sSS
		}
	case 7:
		off = r[rBX]
	}
	if c.mod == 1 {
		off += uint16(int8(c.fetch8()))
	} else if c.mod == 2 {
		off += c.fetch16()
	}
	s := defseg
	if c.segOvr >= 0 {
		s = c.segOvr
	}
	c.ea = c.lin(s, off)
}

func (c *CPU) getRM8() uint8 {
	if c.eaIsReg {
		return c.get8(c.rm)
	}
	return c.m.rb(c.ea)
}
func (c *CPU) getRM16() uint16 {
	if c.eaIsReg {
		return c.r[c.rm]
	}
	return c.m.rw(c.ea)
}
func (c *CPU) setRM8(v uint8) {
	if c.eaIsReg {
		c.set8(c.rm, v)
	} else {
		c.m.wb(c.ea, v)
	}
}
func (c *CPU) setRM16(v uint16) {
	if c.eaIsReg {
		c.r[c.rm] = v
	} else {
		c.m.ww(c.ea, v)
	}
}

func (c *CPU) doInt(n int) {
	c.push(c.flags)
	c.push(c.sr[sCS])
	c.push(c.ip)
	c.flags &^= fIF | fTF
	c.ip = c.m.rw(uint32(n * 4))
	c.sr[sCS] = c.m.rw(uint32(n*4 + 2))
}

func (c *CPU) shiftOp(op int, v uint16, cnt int, w bool) uint16 {
	var m, sb uint32 = 0xff, 0x80
	bits := 8
	if w {
		m, sb, bits = 0xffff, 0x8000, 16
	}
	cnt &= 0x1f
	if cnt == 0 {
		return v
	}
	x := uint32(v)
	cf := false
	switch op {
	case 0: // rol
		for i := 0; i < cnt; i++ {
			cf = x&sb != 0
			x = (x << 1) & m
			if cf {
				x |= 1
			}
		}
		c.setf(fCF, cf)
		c.setf(fOF, (x&sb != 0) != cf)
	case 1: // ror
		for i := 0; i < cnt; i++ {
			cf = x&1 != 0
			x >>= 1
			if cf {
				x |= sb
			}
		}
		c.setf(fCF, cf)
		c.setf(fOF, (x^(x<<1))&sb != 0)
	case 2: // rcl
		for i := 0; i < cnt; i++ {
			nc := x&sb != 0
			x = (x << 1) & m
			if c.flags&fCF != 0 {
				x |= 1
			}
			c.setf(fCF, nc)
		}
		c.setf(fOF, (x&sb != 0) != (c.flags&fCF != 0))
	case 3: // rcr
		for i := 0; i < cnt; i++ {
			nc := x&1 != 0
			x >>= 1
			if c.flags&fCF != 0 {
				x |= sb
			}
			c.setf(fCF, nc)
		}
		c.setf(fOF, (x^(x<<1))&sb != 0)
	case 4, 6: // shl
		if cnt <= bits {
			cf = (x<<(cnt-1))&sb != 0
		}
		x = (x << cnt) & m
		c.setf(fCF, cf)
		c.setf(fOF, (x&sb != 0) != cf)
		c.szpw(x, w)
	case 5: // shr
		if cnt <= bits {
			cf = (x>>(cnt-1))&1 != 0
		}
		c.setf(fOF, x&sb != 0)
		if cnt >= bits {
			x = 0
		} else {
			x >>= cnt
		}
		c.setf(fCF, cf)
		c.szpw(x, w)
	case 7: // sar
		var sx int32
		if w {
			sx = int32(int16(x))
		} else {
			sx = int32(int8(x))
		}
		if cnt > bits {
			cnt = bits
		}
		cf = (sx>>(cnt-1))&1 != 0
		sx >>= cnt
		x = uint32(sx) & m
		c.setf(fCF, cf)
		c.setf(fOF, false)
		c.szpw(x, w)
	}
	return uint16(x)
}

func (c *CPU) szpw(x uint32, w bool) {
	if w {
		c.szp16(uint16(x))
	} else {
		c.szp8(uint8(x))
	}
}

func (c *CPU) cond(cc int) bool {
	f := c.flags
	var r bool
	switch cc >> 1 {
	case 0:
		r = f&fOF != 0
	case 1:
		r = f&fCF != 0
	case 2:
		r = f&fZF != 0
	case 3:
		r = f&(fCF|fZF) != 0
	case 4:
		r = f&fSF != 0
	case 5:
		r = f&fPF != 0
	case 6:
		r = (f&fSF != 0) != (f&fOF != 0)
	default:
		r = f&fZF != 0 || (f&fSF != 0) != (f&fOF != 0)
	}
	if cc&1 != 0 {
		return !r
	}
	return r
}

func (c *CPU) dataSeg() int {
	if c.segOvr >= 0 {
		return c.segOvr
	}
	return sDS
}

func (c *CPU) step() {
	c.segOvr = -1
	c.rep = 0
	var op uint8
	for {
		op = c.fetch8()
		switch op {
		case 0x26:
			c.segOvr = sES
			continue
		case 0x2e:
			c.segOvr = sCS
			continue
		case 0x36:
			c.segOvr = sSS
			continue
		case 0x3e:
			c.segOvr = sDS
			continue
		case 0xf2:
			c.rep = 2
			continue
		case 0xf3:
			c.rep = 1
			continue
		case 0xf0:
			continue
		}
		break
	}
	c.icount++
	r := &c.r
	m := c.m
	if op < 0x40 && op&7 < 6 {
		aop := int(op >> 3)
		switch op & 7 {
		case 0:
			c.decodeModrm()
			v := c.alu(aop, uint32(c.getRM8()), uint32(c.get8(c.reg)), false)
			if aop != 7 {
				c.setRM8(uint8(v))
			}
		case 1:
			c.decodeModrm()
			v := c.alu(aop, uint32(c.getRM16()), uint32(r[c.reg]), true)
			if aop != 7 {
				c.setRM16(uint16(v))
			}
		case 2:
			c.decodeModrm()
			v := c.alu(aop, uint32(c.get8(c.reg)), uint32(c.getRM8()), false)
			if aop != 7 {
				c.set8(c.reg, uint8(v))
			}
		case 3:
			c.decodeModrm()
			v := c.alu(aop, uint32(r[c.reg]), uint32(c.getRM16()), true)
			if aop != 7 {
				r[c.reg] = uint16(v)
			}
		case 4:
			v := c.alu(aop, uint32(r[rAX]&0xff), uint32(c.fetch8()), false)
			if aop != 7 {
				r[rAX] = r[rAX]&0xff00 | uint16(v)
			}
		case 5:
			v := c.alu(aop, uint32(r[rAX]), uint32(c.fetch16()), true)
			if aop != 7 {
				r[rAX] = uint16(v)
			}
		}
		return
	}
	switch op {
	case 0x06:
		c.push(c.sr[sES])
	case 0x07:
		c.sr[sES] = c.pop()
	case 0x0e:
		c.push(c.sr[sCS])
	case 0x16:
		c.push(c.sr[sSS])
	case 0x17:
		c.sr[sSS] = c.pop()
	case 0x1e:
		c.push(c.sr[sDS])
	case 0x1f:
		c.sr[sDS] = c.pop()
	case 0x27: // daa
		al := uint8(r[rAX])
		oc := c.flags&fCF != 0
		old := al
		if al&15 > 9 || c.flags&fAF != 0 {
			al += 6
			c.flags |= fAF
		}
		if old > 0x99 || oc {
			al += 0x60
			c.flags |= fCF
		} else {
			c.flags &^= fCF
		}
		r[rAX] = r[rAX]&0xff00 | uint16(al)
		c.szp8(al)
	case 0x2f: // das
		al := uint8(r[rAX])
		oc := c.flags&fCF != 0
		old := al
		if al&15 > 9 || c.flags&fAF != 0 {
			al -= 6
			c.flags |= fAF
		}
		if old > 0x99 || oc {
			al -= 0x60
			c.flags |= fCF
		}
		r[rAX] = r[rAX]&0xff00 | uint16(al)
		c.szp8(al)
	case 0x37: // aaa
		if r[rAX]&15 > 9 || c.flags&fAF != 0 {
			r[rAX] += 0x106
			c.flags |= fAF | fCF
		} else {
			c.flags &^= fAF | fCF
		}
		r[rAX] &= 0xff0f
	case 0x3f: // aas
		if r[rAX]&15 > 9 || c.flags&fAF != 0 {
			r[rAX] -= 6
			r[rAX] -= 0x100
			c.flags |= fAF | fCF
		} else {
			c.flags &^= fAF | fCF
		}
		r[rAX] &= 0xff0f
	case 0x40, 0x41, 0x42, 0x43, 0x44, 0x45, 0x46, 0x47:
		cf := c.flags & fCF
		r[op&7] = uint16(c.alu(0, uint32(r[op&7]), 1, true))
		c.setf(fCF, cf != 0)
	case 0x48, 0x49, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e, 0x4f:
		cf := c.flags & fCF
		r[op&7] = uint16(c.alu(5, uint32(r[op&7]), 1, true))
		c.setf(fCF, cf != 0)
	case 0x50, 0x51, 0x52, 0x53, 0x55, 0x56, 0x57:
		c.push(r[op&7])
	case 0x54:
		c.push(r[rSP])
	case 0x58, 0x59, 0x5a, 0x5b, 0x5c, 0x5d, 0x5e, 0x5f:
		r[op&7] = c.pop()
	case 0x60:
		t := r[rSP]
		c.push(r[rAX])
		c.push(r[rCX])
		c.push(r[rDX])
		c.push(r[rBX])
		c.push(t)
		c.push(r[rBP])
		c.push(r[rSI])
		c.push(r[rDI])
	case 0x61:
		r[rDI] = c.pop()
		r[rSI] = c.pop()
		r[rBP] = c.pop()
		c.pop()
		r[rBX] = c.pop()
		r[rDX] = c.pop()
		r[rCX] = c.pop()
		r[rAX] = c.pop()
	case 0x68:
		c.push(c.fetch16())
	case 0x6a:
		c.push(uint16(int16(int8(c.fetch8()))))
	case 0x69, 0x6b:
		c.decodeModrm()
		a := int32(int16(c.getRM16()))
		var b int32
		if op == 0x69 {
			b = int32(int16(c.fetch16()))
		} else {
			b = int32(int8(c.fetch8()))
		}
		p := a * b
		r[c.reg] = uint16(p)
		c.setf(fCF|fOF, p != int32(int16(p)))
	case 0x70, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78, 0x79, 0x7a, 0x7b, 0x7c, 0x7d, 0x7e, 0x7f:
		d := int8(c.fetch8())
		if c.cond(int(op & 15)) {
			c.ip += uint16(d)
		}
	case 0x80, 0x82:
		c.decodeModrm()
		v := c.alu(c.reg, uint32(c.getRM8()), uint32(c.fetch8()), false)
		if c.reg != 7 {
			c.setRM8(uint8(v))
		}
	case 0x81:
		c.decodeModrm()
		v := c.alu(c.reg, uint32(c.getRM16()), uint32(c.fetch16()), true)
		if c.reg != 7 {
			c.setRM16(uint16(v))
		}
	case 0x83:
		c.decodeModrm()
		v := c.alu(c.reg, uint32(c.getRM16()), uint32(uint16(int16(int8(c.fetch8())))), true)
		if c.reg != 7 {
			c.setRM16(uint16(v))
		}
	case 0x84:
		c.decodeModrm()
		c.alu(4, uint32(c.getRM8()), uint32(c.get8(c.reg)), false)
	case 0x85:
		c.decodeModrm()
		c.alu(4, uint32(c.getRM16()), uint32(r[c.reg]), true)
	case 0x86:
		c.decodeModrm()
		t := c.getRM8()
		c.setRM8(c.get8(c.reg))
		c.set8(c.reg, t)
	case 0x87:
		c.decodeModrm()
		t := c.getRM16()
		c.setRM16(r[c.reg])
		r[c.reg] = t
	case 0x88:
		c.decodeModrm()
		c.setRM8(c.get8(c.reg))
	case 0x89:
		c.decodeModrm()
		c.setRM16(r[c.reg])
	case 0x8a:
		c.decodeModrm()
		c.set8(c.reg, c.getRM8())
	case 0x8b:
		c.decodeModrm()
		r[c.reg] = c.getRM16()
	case 0x8c:
		c.decodeModrm()
		c.setRM16(c.sr[c.reg&3])
	case 0x8e:
		c.decodeModrm()
		c.sr[c.reg&3] = c.getRM16()
	case 0x8d: // lea
		mb := c.fetch8()
		md, rg, rmm := mb>>6, int(mb>>3)&7, mb&7
		var off uint16
		switch rmm {
		case 0:
			off = r[rBX] + r[rSI]
		case 1:
			off = r[rBX] + r[rDI]
		case 2:
			off = r[rBP] + r[rSI]
		case 3:
			off = r[rBP] + r[rDI]
		case 4:
			off = r[rSI]
		case 5:
			off = r[rDI]
		case 6:
			if md == 0 {
				off = c.fetch16()
			} else {
				off = r[rBP]
			}
		case 7:
			off = r[rBX]
		}
		if md == 1 {
			off += uint16(int8(c.fetch8()))
		} else if md == 2 {
			off += c.fetch16()
		}
		r[rg] = off
	case 0x8f:
		c.decodeModrm()
		c.setRM16(c.pop())
	case 0x90:
	case 0x91, 0x92, 0x93, 0x94, 0x95, 0x96, 0x97:
		r[rAX], r[op&7] = r[op&7], r[rAX]
	case 0x98:
		r[rAX] = uint16(int16(int8(r[rAX])))
	case 0x99:
		if r[rAX]&0x8000 != 0 {
			r[rDX] = 0xffff
		} else {
			r[rDX] = 0
		}
	case 0x9a:
		o, s := c.fetch16(), c.fetch16()
		c.push(c.sr[sCS])
		c.push(c.ip)
		c.sr[sCS], c.ip = s, o
	case 0x9b:
	case 0x9c:
		c.push(c.flags | 0xf002)
	case 0x9d:
		c.flags = c.pop()&0x0fd5 | 0x0002
	case 0x9e:
		c.flags = c.flags&0xff00 | (r[rAX]>>8)&0xd5 | 2
	case 0x9f:
		r[rAX] = r[rAX]&0xff | (c.flags&0xff)<<8
	case 0xa0:
		o := c.fetch16()
		r[rAX] = r[rAX]&0xff00 | uint16(m.rb(c.lin(c.dataSeg(), o)))
	case 0xa1:
		o := c.fetch16()
		r[rAX] = m.rw(c.lin(c.dataSeg(), o))
	case 0xa2:
		o := c.fetch16()
		m.wb(c.lin(c.dataSeg(), o), uint8(r[rAX]))
	case 0xa3:
		o := c.fetch16()
		m.ww(c.lin(c.dataSeg(), o), r[rAX])
	case 0xa4, 0xa5, 0xa6, 0xa7, 0xaa, 0xab, 0xac, 0xad, 0xae, 0xaf:
		c.stringOp(op)
	case 0xa8:
		c.alu(4, uint32(r[rAX]&0xff), uint32(c.fetch8()), false)
	case 0xa9:
		c.alu(4, uint32(r[rAX]), uint32(c.fetch16()), true)
	case 0xb0, 0xb1, 0xb2, 0xb3, 0xb4, 0xb5, 0xb6, 0xb7:
		c.set8(int(op&7), c.fetch8())
	case 0xb8, 0xb9, 0xba, 0xbb, 0xbc, 0xbd, 0xbe, 0xbf:
		r[op&7] = c.fetch16()
	case 0xc0, 0xc1, 0xd0, 0xd1, 0xd2, 0xd3:
		c.decodeModrm()
		w := op&1 != 0
		var cnt int
		switch {
		case op <= 0xc1:
			cnt = int(c.fetch8())
		case op <= 0xd1:
			cnt = 1
		default:
			cnt = int(r[rCX] & 0xff)
		}
		if w {
			c.setRM16(c.shiftOp(c.reg, c.getRM16(), cnt, true))
		} else {
			c.setRM8(uint8(c.shiftOp(c.reg, uint16(c.getRM8()), cnt, false)))
		}
	case 0xc2:
		n := c.fetch16()
		c.ip = c.pop()
		r[rSP] += n
	case 0xc3:
		c.ip = c.pop()
	case 0xc4:
		c.decodeModrm()
		r[c.reg] = m.rw(c.ea)
		c.sr[sES] = m.rw(c.ea + 2)
	case 0xc5:
		c.decodeModrm()
		r[c.reg] = m.rw(c.ea)
		c.sr[sDS] = m.rw(c.ea + 2)
	case 0xc6:
		c.decodeModrm()
		c.setRM8(c.fetch8())
	case 0xc7:
		c.decodeModrm()
		c.setRM16(c.fetch16())
	case 0xc8:
		sz := c.fetch16()
		lv := int(c.fetch8())
		c.push(r[rBP])
		fp := r[rSP]
		for i := 1; i < lv; i++ {
			r[rBP] -= 2
			c.push(m.rw(c.lin(sSS, r[rBP])))
		}
		if lv > 0 {
			c.push(fp)
		}
		r[rBP] = fp
		r[rSP] -= sz
	case 0xc9:
		r[rSP] = r[rBP]
		r[rBP] = c.pop()
	case 0xca:
		n := c.fetch16()
		c.ip = c.pop()
		c.sr[sCS] = c.pop()
		r[rSP] += n
	case 0xcb:
		c.ip = c.pop()
		c.sr[sCS] = c.pop()
	case 0xcc:
		c.doInt(3)
	case 0xcd:
		c.doInt(int(c.fetch8()))
	case 0xce:
		if c.flags&fOF != 0 {
			c.doInt(4)
		}
	case 0xcf:
		c.ip = c.pop()
		c.sr[sCS] = c.pop()
		c.flags = c.pop()&0x0fd5 | 2
	case 0xd4:
		b := c.fetch8()
		al := uint8(r[rAX])
		if b == 0 {
			c.doInt(0)
			break
		}
		r[rAX] = uint16(al/b)<<8 | uint16(al%b)
		c.szp8(uint8(r[rAX]))
	case 0xd5:
		b := c.fetch8()
		v := uint8(r[rAX]) + uint8(r[rAX]>>8)*b
		r[rAX] = uint16(v)
		c.szp8(v)
	case 0xd6:
		if c.flags&fCF != 0 {
			r[rAX] |= 0xff
		} else {
			r[rAX] &= 0xff00
		}
	case 0xd7:
		r[rAX] = r[rAX]&0xff00 | uint16(m.rb(c.lin(c.dataSeg(), r[rBX]+r[rAX]&0xff)))
	case 0xd8, 0xd9, 0xda, 0xdb, 0xdc, 0xdd, 0xde, 0xdf:
		c.decodeModrm()
	case 0xe0:
		d := int8(c.fetch8())
		r[rCX]--
		if r[rCX] != 0 && c.flags&fZF == 0 {
			c.ip += uint16(d)
		}
	case 0xe1:
		d := int8(c.fetch8())
		r[rCX]--
		if r[rCX] != 0 && c.flags&fZF != 0 {
			c.ip += uint16(d)
		}
	case 0xe2:
		d := int8(c.fetch8())
		r[rCX]--
		if r[rCX] != 0 {
			c.ip += uint16(d)
		}
	case 0xe3:
		d := int8(c.fetch8())
		if r[rCX] == 0 {
			c.ip += uint16(d)
		}
	case 0xe4:
		r[rAX] = r[rAX]&0xff00 | uint16(m.portIn(uint16(c.fetch8())))
	case 0xe5:
		p := uint16(c.fetch8())
		r[rAX] = uint16(m.portIn(p)) | uint16(m.portIn(p+1))<<8
	case 0xe6:
		m.portOut(uint16(c.fetch8()), uint8(r[rAX]))
	case 0xe7:
		p := uint16(c.fetch8())
		m.portOut(p, uint8(r[rAX]))
		m.portOut(p+1, uint8(r[rAX]>>8))
	case 0xe8:
		d := c.fetch16()
		c.push(c.ip)
		c.ip += d
	case 0xe9:
		d := c.fetch16()
		c.ip += d
	case 0xea:
		o, s := c.fetch16(), c.fetch16()
		c.ip, c.sr[sCS] = o, s
	case 0xeb:
		d := int8(c.fetch8())
		c.ip += uint16(d)
	case 0xec:
		r[rAX] = r[rAX]&0xff00 | uint16(m.portIn(r[rDX]))
	case 0xed:
		r[rAX] = uint16(m.portIn(r[rDX])) | uint16(m.portIn(r[rDX]+1))<<8
	case 0xee:
		m.portOut(r[rDX], uint8(r[rAX]))
	case 0xef:
		m.portOut(r[rDX], uint8(r[rAX]))
		m.portOut(r[rDX]+1, uint8(r[rAX]>>8))
	case 0xf4:
		if c.flags&fIF == 0 {
			c.halted = 3
		} else if m.nextTimer > c.icount {
			c.icount = m.nextTimer
		}
	case 0xf5:
		c.flags ^= fCF
	case 0xf6, 0xf7:
		c.group3(op&1 != 0)
	case 0xf8:
		c.flags &^= fCF
	case 0xf9:
		c.flags |= fCF
	case 0xfa:
		c.flags &^= fIF
	case 0xfb:
		c.flags |= fIF
	case 0xfc:
		c.flags &^= fDF
	case 0xfd:
		c.flags |= fDF
	case 0xfe:
		c.decodeModrm()
		cf := c.flags & fCF
		v := c.getRM8()
		switch c.reg {
		case 0:
			v = uint8(c.alu(0, uint32(v), 1, false))
		case 1:
			v = uint8(c.alu(5, uint32(v), 1, false))
		default:
			c.bad(op)
			return
		}
		c.setf(fCF, cf != 0)
		c.setRM8(v)
	case 0xff:
		c.decodeModrm()
		switch c.reg {
		case 0:
			cf := c.flags & fCF
			c.setRM16(uint16(c.alu(0, uint32(c.getRM16()), 1, true)))
			c.setf(fCF, cf != 0)
		case 1:
			cf := c.flags & fCF
			c.setRM16(uint16(c.alu(5, uint32(c.getRM16()), 1, true)))
			c.setf(fCF, cf != 0)
		case 2:
			t := c.getRM16()
			c.push(c.ip)
			c.ip = t
		case 3:
			o, s := m.rw(c.ea), m.rw(c.ea+2)
			c.push(c.sr[sCS])
			c.push(c.ip)
			c.ip, c.sr[sCS] = o, s
		case 4:
			c.ip = c.getRM16()
		case 5:
			c.ip, c.sr[sCS] = m.rw(c.ea), m.rw(c.ea+2)
		case 6:
			c.push(c.getRM16())
		default:
			c.bad(op)
		}
	case 0x0f:
		o2 := c.fetch8()
		if o2 == 0xff {
			m.hostInt(int(c.fetch8()))
		} else {
			c.bad(op)
		}
	default:
		c.bad(op)
	}
}

func (c *CPU) stringOp(op uint8) {
	r := &c.r
	m := c.m
	w := op&1 != 0
	step := uint16(1)
	if w {
		step = 2
	}
	if c.flags&fDF != 0 {
		step = -step
	}
	s := c.dataSeg()
	kind := op &^ 1
	if c.rep != 0 && r[rCX] == 0 {
		return
	}
	for {
		switch kind {
		case 0xa4:
			if w {
				m.ww(c.lin(sES, r[rDI]), m.rw(c.lin(s, r[rSI])))
			} else {
				m.wb(c.lin(sES, r[rDI]), m.rb(c.lin(s, r[rSI])))
			}
			r[rSI] += step
			r[rDI] += step
		case 0xa6:
			if w {
				c.alu(7, uint32(m.rw(c.lin(s, r[rSI]))), uint32(m.rw(c.lin(sES, r[rDI]))), true)
			} else {
				c.alu(7, uint32(m.rb(c.lin(s, r[rSI]))), uint32(m.rb(c.lin(sES, r[rDI]))), false)
			}
			r[rSI] += step
			r[rDI] += step
		case 0xaa:
			if w {
				m.ww(c.lin(sES, r[rDI]), r[rAX])
			} else {
				m.wb(c.lin(sES, r[rDI]), uint8(r[rAX]))
			}
			r[rDI] += step
		case 0xac:
			if w {
				r[rAX] = m.rw(c.lin(s, r[rSI]))
			} else {
				r[rAX] = r[rAX]&0xff00 | uint16(m.rb(c.lin(s, r[rSI])))
			}
			r[rSI] += step
		case 0xae:
			if w {
				c.alu(7, uint32(r[rAX]), uint32(m.rw(c.lin(sES, r[rDI]))), true)
			} else {
				c.alu(7, uint32(r[rAX]&0xff), uint32(m.rb(c.lin(sES, r[rDI]))), false)
			}
			r[rDI] += step
		}
		if c.rep == 0 {
			return
		}
		r[rCX]--
		c.icount++
		if r[rCX] == 0 {
			return
		}
		if kind == 0xa6 || kind == 0xae {
			if c.rep == 1 && c.flags&fZF == 0 {
				return
			}
			if c.rep == 2 && c.flags&fZF != 0 {
				return
			}
		}
	}
}

func (c *CPU) group3(w bool) {
	c.decodeModrm()
	r := &c.r
	switch c.reg {
	case 0, 1:
		if w {
			c.alu(4, uint32(c.getRM16()), uint32(c.fetch16()), true)
		} else {
			c.alu(4, uint32(c.getRM8()), uint32(c.fetch8()), false)
		}
	case 2:
		if w {
			c.setRM16(^c.getRM16())
		} else {
			c.setRM8(^c.getRM8())
		}
	case 3:
		if w {
			v := c.getRM16()
			c.setRM16(uint16(c.alu(5, 0, uint32(v), true)))
			c.setf(fCF, v != 0)
		} else {
			v := c.getRM8()
			c.setRM8(uint8(c.alu(5, 0, uint32(v), false)))
			c.setf(fCF, v != 0)
		}
	case 4:
		if w {
			p := uint32(r[rAX]) * uint32(c.getRM16())
			r[rAX], r[rDX] = uint16(p), uint16(p>>16)
			c.setf(fCF|fOF, r[rDX] != 0)
		} else {
			p := uint16(r[rAX]&0xff) * uint16(c.getRM8())
			r[rAX] = p
			c.setf(fCF|fOF, p>>8 != 0)
		}
	case 5:
		if w {
			p := int32(int16(r[rAX])) * int32(int16(c.getRM16()))
			r[rAX], r[rDX] = uint16(p), uint16(uint32(p)>>16)
			c.setf(fCF|fOF, p != int32(int16(p)))
		} else {
			p := int16(int8(r[rAX])) * int16(int8(c.getRM8()))
			r[rAX] = uint16(p)
			c.setf(fCF|fOF, p != int16(int8(p)))
		}
	case 6:
		if w {
			d := uint32(c.getRM16())
			n := uint32(r[rDX])<<16 | uint32(r[rAX])
			if d == 0 || n/d > 0xffff {
				c.doInt(0)
				return
			}
			r[rAX], r[rDX] = uint16(n/d), uint16(n%d)
		} else {
			d := uint16(c.getRM8())
			n := r[rAX]
			if d == 0 || n/d > 0xff {
				c.doInt(0)
				return
			}
			r[rAX] = (n%d)<<8 | n/d
		}
	case 7:
		if w {
			d := int32(int16(c.getRM16()))
			n := int32(uint32(r[rDX])<<16 | uint32(r[rAX]))
			if d == 0 || n/d > 32767 || n/d < -32768 {
				c.doInt(0)
				return
			}
			r[rAX], r[rDX] = uint16(n/d), uint16(n%d)
		} else {
			d := int16(int8(c.getRM8()))
			n := int16(r[rAX])
			if d == 0 || n/d > 127 || n/d < -128 {
				c.doInt(0)
				return
			}
			r[rAX] = uint16(uint8(n%d))<<8 | uint16(uint8(n/d))
		}
	}
}

func (c *CPU) bad(op uint8) {
	c.m.logf("unimplemented opcode %02x at %04x:%04x", op, c.sr[sCS], c.ip-1)
	c.halted = 2
}
