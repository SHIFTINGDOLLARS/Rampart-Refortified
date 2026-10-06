//go:build !windows

package main

import (
	"encoding/binary"
	"os"
)

// Headless: write a WAV file when RAMPART_WAV is set.
type wavSink struct {
	f *os.File
	n uint32
}

func openSink() sampleSink {
	p := os.Getenv("RAMPART_WAV")
	if p == "" {
		return nil
	}
	f, err := os.Create(p)
	if err != nil {
		return nil
	}
	f.Write(make([]byte, 44))
	return &wavSink{f: f}
}

func (w *wavSink) Write(s []int16) {
	binary.Write(w.f, binary.LittleEndian, s)
	w.n += uint32(len(s) * 2)
}

func (w *wavSink) Close() {
	h := make([]byte, 44)
	copy(h, "RIFF")
	binary.LittleEndian.PutUint32(h[4:], 36+w.n)
	copy(h[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1)
	binary.LittleEndian.PutUint16(h[22:], 1)
	binary.LittleEndian.PutUint32(h[24:], SampleRate)
	binary.LittleEndian.PutUint32(h[28:], SampleRate*2)
	binary.LittleEndian.PutUint16(h[32:], 2)
	binary.LittleEndian.PutUint16(h[34:], 16)
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], w.n)
	w.f.WriteAt(h, 0)
	w.f.Close()
}
