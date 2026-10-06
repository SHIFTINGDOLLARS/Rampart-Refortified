package main

import "os"

var osGetenv = os.Getenv

// AudioOut is driven by the front end once per emulated frame.
type AudioOut interface {
	EndFrame()
	Close()
}

// sampleSink receives mono 16-bit samples at SampleRate.
type sampleSink interface {
	Write(samples []int16)
	Close()
}

func getenvInt(k string) int {
	n := 0
	for _, c := range osGetenv(k) {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}
