//go:build windows

package main

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

// Windows' Program Compatibility Assistant can silently attach compatibility
// settings to an exe (keyed by its full path) after a run it considers
// problematic, e.g. one that ended abnormally. They apply to every later
// launch from that path, and some of them break plain GDI drawing: the window
// stays white while the game runs. We remove such entries for our own exe from
// the current user's registry and, if they were active in this process,
// restart once without them. The embedded manifest (requestedExecutionLevel,
// supportedOS) also keeps the assistant from adding new ones.

var (
	advapi32          = syscall.NewLazyDLL("advapi32.dll")
	pRegOpenKeyExW    = advapi32.NewProc("RegOpenKeyExW")
	pRegEnumValueW    = advapi32.NewProc("RegEnumValueW")
	pRegDeleteValueW  = advapi32.NewProc("RegDeleteValueW")
	pRegCloseKey      = advapi32.NewProc("RegCloseKey")
	pGetPixel         = gdi32.NewProc("GetPixel")
	pSetDIBitsToDevic = gdi32.NewProc("SetDIBitsToDevice")
)

const (
	hkeyCurrentUser = 0x80000001
	keyQuerySet     = 0x0001 | 0x0002 // KEY_QUERY_VALUE | KEY_SET_VALUE
	compatRoot      = `Software\Microsoft\Windows NT\CurrentVersion\AppCompatFlags\`
)

// regValues lists the value names (and string data) of an HKCU key.
func regValues(h uintptr) map[string]string {
	out := map[string]string{}
	for i := uint32(0); ; i++ {
		name := make([]uint16, 1024)
		nlen := uint32(len(name))
		data := make([]uint16, 2048)
		dlen := uint32(len(data) * 2)
		var typ uint32
		r, _, _ := pRegEnumValueW.Call(h, uintptr(i), uintptr(unsafe.Pointer(&name[0])), uintptr(unsafe.Pointer(&nlen)),
			0, uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(&dlen)))
		if r == 259 { // ERROR_NO_MORE_ITEMS
			break
		}
		if r != 0 {
			if r == 234 { // ERROR_MORE_DATA: skip oversized values
				continue
			}
			break
		}
		v := ""
		if typ == 1 || typ == 2 { // REG_SZ, REG_EXPAND_SZ
			v = syscall.UTF16ToString(data[:dlen/2])
		}
		out[syscall.UTF16ToString(name[:nlen])] = v
	}
	return out
}

// clearCompatFlags removes compatibility entries for exe; it reports whether
// a compatibility layer entry (not just the assistant's bookkeeping) was found.
func clearCompatFlags(exe string) (hadLayer bool) {
	for _, sub := range []string{"Layers", `Compatibility Assistant\Store`, `Compatibility Assistant\Persisted`} {
		var h uintptr
		if r, _, _ := pRegOpenKeyExW.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(utf16(compatRoot+sub))), 0, keyQuerySet,
			uintptr(unsafe.Pointer(&h))); r != 0 {
			continue
		}
		for name, val := range regValues(h) {
			if !strings.EqualFold(name, exe) {
				continue
			}
			r, _, _ := pRegDeleteValueW.Call(h, uintptr(unsafe.Pointer(utf16(name))))
			logLine(fmt.Sprintf("compat: removed %s entry %q (%s), result %d", sub, val, name, r))
			if sub == "Layers" {
				hadLayer = true
			}
		}
		pRegCloseKey.Call(h)
	}
	return
}

// fixCompat runs before any window is created. It returns true if the process
// restarted itself and should exit.
func fixCompat() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	layer := os.Getenv("__COMPAT_LAYER")
	if layer != "" {
		logLine("compat: running with compatibility layer: " + layer)
	}
	hadLayer := clearCompatFlags(exe)
	if (layer == "" && !hadLayer) || os.Getenv("RAMPART_RESTARTED") != "" {
		return false
	}
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(e), "__COMPAT_LAYER=") {
			env = append(env, e)
		}
	}
	env = append(env, "RAMPART_RESTARTED=1")
	wd, _ := os.Getwd()
	p, err := os.StartProcess(exe, os.Args, &os.ProcAttr{Dir: wd, Env: env, Files: []*os.File{os.Stdin, os.Stdout, os.Stderr}})
	if err != nil {
		logLine("compat: restart failed: " + err.Error())
		return false
	}
	p.Release()
	logLine("compat: restarted without compatibility settings")
	return true
}

// ------------------------------------------------------------ display check

// Display methods, tried in order if frames don't reach the screen.
const (
	drawDIB       = iota // DIB section + StretchBlt
	drawStretchDI        // StretchDIBits
	drawSetDI            // SetDIBitsToDevice (unscaled, centred)
	drawMethods
)

var drawNames = []string{"DIB section + StretchBlt", "StretchDIBits", "SetDIBitsToDevice"}

// verify samples the window at a few points after a frame was drawn and
// compares them with the frame. After repeated mismatches it moves on to the
// next drawing method. Runs every 10th frame during the first ~30 seconds.
func (w *winApp) verify(hdc uintptr, x, y, ww, hh int32) {
	if w.verifyDone || w.painted%10 != 0 {
		return
	}
	if w.painted > 2000 {
		w.verifyDone = true
		logLine(fmt.Sprintf("display check: %d ok, %d mismatched, method %s", w.verifyOK, w.verifyBad, drawNames[w.method]))
		return
	}
	ok, tested := 0, 0
	for _, pt := range [][2]int{{80, 50}, {160, 100}, {240, 150}, {100, 160}, {220, 40}} {
		sx, sy := pt[0], pt[1]
		want := w.pix[sy*320+sx]
		if want == w.pix[sy*320+sx-1] && want == w.pix[sy*320+sx+1] && want == w.pix[(sy-1)*320+sx] && want == w.pix[(sy+1)*320+sx] {
			// sample the middle of that source pixel
			dx := x + int32((2*sx+1)*int(ww)/640)
			dy := y + int32((2*sy+1)*int(hh)/400)
			if w.method == drawSetDI {
				dx, dy = x+int32(sx), y+int32(sy)
			}
			got, _, _ := pGetPixel.Call(hdc, uintptr(dx), uintptr(dy))
			if got == 0xFFFFFFFF {
				continue // outside the visible area
			}
			tested++
			wantRef := uintptr(want>>16&255 | (want>>8&255)<<8 | (want&255)<<16)
			if got == wantRef {
				ok++
			} else if w.verifyBad < 5 {
				logLine(fmt.Sprintf("display check: (%d,%d) frame %06x, window %06x", sx, sy, wantRef, got))
			}
		}
	}
	if tested == 0 {
		return
	}
	if ok > 0 {
		w.verifyOK++
		w.badRun = 0
		return
	}
	w.verifyBad++
	w.badRun++
	if w.badRun >= 6 && w.method+1 < drawMethods {
		w.method++
		w.badRun = 0
		logLine("display check: frames are not reaching the window, switching to " + drawNames[w.method])
	}
}
