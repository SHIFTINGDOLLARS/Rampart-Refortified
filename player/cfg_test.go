package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestCfgRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := LoadSettings(dir)
	f := NewFeatures(s)
	v := NewVFS(dir)
	v.Transform, v.OnWrite = f.transform, f.onWrite
	v.MemOnly["RAMPART.CFG"] = true
	r, err := v.Open("rampart.cfg", false)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 0x66)
	if r.Read(buf) != 0x66 {
		t.Fatal("short read")
	}
	r.Close()
	// game changes options and keys, re-encrypts, writes via create (3Ch)
	p := append([]byte(nil), buf[:0x62]...)
	cfgCrypt(p)
	binary.LittleEndian.PutUint16(p[0x5c:], 1) // Enhanced
	binary.LittleEndian.PutUint16(p[0x5a:], 2) // difficulty
	p[0x32] = 0x10                             // kbd1 up = Q
	out := append([]byte(nil), p...)
	cfgCrypt(out)
	out = binary.LittleEndian.AppendUint32(out, cfgChecksum(p))
	w, err := v.Create("rampart.cfg")
	if err != nil {
		t.Fatal(err)
	}
	w.Write(out)
	w.Close()
	if _, err := os.Stat(filepath.Join(dir, "RAMPART.CFG")); err == nil {
		t.Fatal("cfg written to disk")
	}
	s2 := LoadSettings(dir)
	if s2.GameOptions.GameType != 1 || s2.GameOptions.Difficulty != 2 || s2.Keys1[0] != 0x10 {
		t.Fatalf("not saved: %+v %v", s2.GameOptions, s2.Keys1)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	t.Logf("%s", b)
}
