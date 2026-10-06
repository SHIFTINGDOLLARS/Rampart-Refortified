//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

// XInput polling. XInputGetState fills a 16-byte XINPUT_STATE:
// u32 packet, u16 buttons, u8 left trigger, u8 right trigger, s16 LX, LY, RX, RY.

type xinputState struct {
	Packet  uint32
	Buttons uint16
	LT, RT  uint8
	LX, LY  int16
	RX, RY  int16
}

type xinput struct {
	get     *syscall.LazyProc
	dll     string
	retryAt [4]int64 // disconnected slots are re-checked now and then: the call is slow
	frame   int64
}

func newXInput() *xinput {
	for _, name := range []string{"xinput1_4.dll", "xinput1_3.dll", "xinput9_1_0.dll"} {
		d := syscall.NewLazyDLL(name)
		if d.Load() != nil {
			continue
		}
		p := d.NewProc("XInputGetState")
		if p.Find() != nil {
			continue
		}
		return &xinput{get: p, dll: name}
	}
	return nil
}

// poll reads the controllers and hands the first two connected ones to the app
// as pad 1 and pad 2.
func (x *xinput) poll(a *App) {
	x.frame++
	n := 0
	for i := 0; i < 4 && n < 2; i++ {
		if x.frame < x.retryAt[i] {
			continue
		}
		var s xinputState
		r, _, _ := x.get.Call(uintptr(i), uintptr(unsafe.Pointer(&s)))
		if r != 0 {
			x.retryAt[i] = x.frame + 140 // about 2 s
			continue
		}
		a.SetPad(n, PadState{Connected: true, Buttons: s.Buttons, LX: s.LX, LY: s.LY, LT: s.LT, RT: s.RT})
		n++
	}
	for ; n < 2; n++ {
		a.SetPad(n, PadState{})
	}
}
