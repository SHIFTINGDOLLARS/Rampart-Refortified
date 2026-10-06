//go:build windows

package main

import (
	"runtime"
	"syscall"
	"unsafe"
)

// waveOut backend: mono 16-bit PCM at SampleRate, streamed in ~23 ms chunks.

var (
	pWaveOutOpen            = winmm.NewProc("waveOutOpen")
	pWaveOutClose           = winmm.NewProc("waveOutClose")
	pWaveOutPrepareHeader   = winmm.NewProc("waveOutPrepareHeader")
	pWaveOutUnprepareHeader = winmm.NewProc("waveOutUnprepareHeader")
	pWaveOutWrite           = winmm.NewProc("waveOutWrite")
	pWaveOutReset           = winmm.NewProc("waveOutReset")
)

type waveFormatEx struct {
	wFormatTag      uint16
	nChannels       uint16
	nSamplesPerSec  uint32
	nAvgBytesPerSec uint32
	nBlockAlign     uint16
	wBitsPerSample  uint16
	cbSize          uint16
}

type waveHdr struct {
	lpData          uintptr
	dwBufferLength  uint32
	dwBytesRecorded uint32
	dwUser          uintptr
	dwFlags         uint32
	dwLoops         uint32
	lpNext          uintptr
	reserved        uintptr
}

const (
	whdrDone     = 0x01
	whdrPrepared = 0x02
	whdrInQueue  = 0x10
	nWaveBufs    = 16
	chunkSamples = 1024
	maxQueued    = 8 // ~185 ms: beyond this, drop audio to keep latency low
)

var lastWaveErr int

type waveSink struct {
	hwo     uintptr
	hdrs    []waveHdr
	bufs    [][]int16
	used    []bool
	pending []int16
	pinner  runtime.Pinner
}

func openSink() sampleSink {
	w := &waveSink{
		hdrs: make([]waveHdr, nWaveBufs),
		bufs: make([][]int16, nWaveBufs),
		used: make([]bool, nWaveBufs),
	}
	wfx := waveFormatEx{wFormatTag: 1, nChannels: 1, nSamplesPerSec: SampleRate,
		nAvgBytesPerSec: SampleRate * 2, nBlockAlign: 2, wBitsPerSample: 16}
	r, _, _ := pWaveOutOpen.Call(uintptr(unsafe.Pointer(&w.hwo)), uintptr(0xFFFFFFFF),
		uintptr(unsafe.Pointer(&wfx)), 0, 0, 0)
	lastWaveErr = int(r)
	if r != 0 {
		return nil // no audio device: play silently
	}
	w.pinner.Pin(&w.hdrs[0])
	for i := range w.bufs {
		w.bufs[i] = make([]int16, chunkSamples)
		w.pinner.Pin(&w.bufs[i][0])
		h := &w.hdrs[i]
		h.lpData = uintptr(unsafe.Pointer(&w.bufs[i][0]))
		h.dwBufferLength = chunkSamples * 2
		pWaveOutPrepareHeader.Call(w.hwo, uintptr(unsafe.Pointer(h)), unsafe.Sizeof(*h))
	}
	return w
}

func (w *waveSink) flags(i int) uint32 {
	return *(*uint32)(unsafe.Pointer(&w.hdrs[i].dwFlags))
}

func (w *waveSink) queued() int {
	n := 0
	for i := range w.hdrs {
		if w.used[i] && w.flags(i)&whdrDone == 0 {
			n++
		}
	}
	return n
}

func (w *waveSink) Write(s []int16) {
	w.pending = append(w.pending, s...)
	for len(w.pending) >= chunkSamples {
		chunk := w.pending[:chunkSamples]
		if w.queued() < maxQueued {
			for i := range w.hdrs {
				if !w.used[i] || w.flags(i)&whdrDone != 0 {
					copy(w.bufs[i], chunk)
					h := &w.hdrs[i]
					h.dwFlags &^= whdrDone
					w.used[i] = true
					pWaveOutWrite.Call(w.hwo, uintptr(unsafe.Pointer(h)), unsafe.Sizeof(*h))
					break
				}
			}
		}
		w.pending = w.pending[chunkSamples:]
	}
	if cap(w.pending) > 1<<16 {
		w.pending = append([]int16(nil), w.pending...)
	}
}

func (w *waveSink) Close() {
	pWaveOutReset.Call(w.hwo)
	for i := range w.hdrs {
		pWaveOutUnprepareHeader.Call(w.hwo, uintptr(unsafe.Pointer(&w.hdrs[i])), unsafe.Sizeof(w.hdrs[i]))
	}
	pWaveOutClose.Call(w.hwo)
	w.pinner.Unpin()
}

var _ = syscall.Errno(0)
