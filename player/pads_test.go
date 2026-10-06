package main

import "testing"

func TestPadAim(t *testing.T) {
	ps := &Pads{}
	m := &Machine{}
	ps.p[0].st = PadState{Connected: true, LX: 32767}
	sum := 0
	for i := 0; i < 70; i++ {
		_, cx, dy := ps.read(m, CtlJoy1, true, 100)
		sum += cx
		if dy != 0 {
			t.Fatal("stray vertical move")
		}
	}
	if sum < 270 || sum > 285 {
		t.Fatalf("full tilt: %d px in a second, want ~280", sum)
	}
	ps.p[0].st.LX = 12000
	sum = 0
	for i := 0; i < 70; i++ {
		_, cx, _ := ps.read(m, CtlJoy1, true, 100)
		sum += cx
	}
	if sum < 3 || sum > 40 {
		t.Fatalf("light tilt: %d px in a second", sum)
	}
}

func TestPadCellRepeat(t *testing.T) {
	ps := &Pads{}
	m := &Machine{}
	ps.p[0].st = PadState{Connected: true, Buttons: padRight}
	var steps []int64
	for f := int64(0); f < 40; f++ {
		m.Frame = f
		if _, cx, _ := ps.read(m, CtlJoy1, false, 100); cx != 0 {
			steps = append(steps, f)
		}
	}
	want := []int64{0, 14, 18, 22, 26, 30, 34, 38}
	if len(steps) != len(want) {
		t.Fatalf("steps at %v, want %v", steps, want)
	}
	for i := range want {
		if steps[i] != want[i] {
			t.Fatalf("steps at %v, want %v", steps, want)
		}
	}
}

func TestPadLatches(t *testing.T) {
	ps := &Pads{}
	m := &Machine{}
	ps.p[0].st = PadState{Connected: true}
	ps.p[0].fireL = true
	if ax, _, _ := ps.read(m, CtlJoy1, false, 100); ax&1 == 0 {
		t.Fatal("fire press lost")
	}
	if ax, _, _ := ps.read(m, CtlJoy1, false, 100); ax&1 != 0 {
		t.Fatal("fire press repeated")
	}
}
