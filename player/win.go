//go:build windows

package main

// Win32 front end: a resizable window (4:3, nearest-neighbour), keyboard input
// as raw scancodes, 70 Hz pacing, Alt+Enter / F11 fullscreen. Standard library only.

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

func init() { runtime.LockOSThread() }

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	winmm    = syscall.NewLazyDLL("winmm.dll")

	pRegisterClassExW   = user32.NewProc("RegisterClassExW")
	pCreateWindowExW    = user32.NewProc("CreateWindowExW")
	pDefWindowProcW     = user32.NewProc("DefWindowProcW")
	pDestroyWindow      = user32.NewProc("DestroyWindow")
	pPostQuitMessage    = user32.NewProc("PostQuitMessage")
	pPeekMessageW       = user32.NewProc("PeekMessageW")
	pTranslateMessage   = user32.NewProc("TranslateMessage")
	pDispatchMessageW   = user32.NewProc("DispatchMessageW")
	pShowWindow         = user32.NewProc("ShowWindow")
	pUpdateWindow       = user32.NewProc("UpdateWindow")
	pGetDC              = user32.NewProc("GetDC")
	pReleaseDC          = user32.NewProc("ReleaseDC")
	pGetClientRect      = user32.NewProc("GetClientRect")
	pAdjustWindowRect   = user32.NewProc("AdjustWindowRect")
	pLoadCursorW        = user32.NewProc("LoadCursorW")
	pLoadIconW          = user32.NewProc("LoadIconW")
	pMessageBoxW        = user32.NewProc("MessageBoxW")
	pGetSystemMetrics   = user32.NewProc("GetSystemMetrics")
	pSetWindowLongPtrW  = user32.NewProc("SetWindowLongPtrW")
	pGetWindowLongPtrW  = user32.NewProc("GetWindowLongPtrW")
	pSetWindowPos       = user32.NewProc("SetWindowPos")
	pGetWindowRect      = user32.NewProc("GetWindowRect")
	pMonitorFromWindow  = user32.NewProc("MonitorFromWindow")
	pGetMonitorInfoW    = user32.NewProc("GetMonitorInfoW")
	pBeginPaint         = user32.NewProc("BeginPaint")
	pEndPaint           = user32.NewProc("EndPaint")
	pFillRect           = user32.NewProc("FillRect")
	pSetProcessDPIAware = user32.NewProc("SetProcessDPIAware")
	pStretchDIBits      = gdi32.NewProc("StretchDIBits")
	pCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	pCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	pSelectObject       = gdi32.NewProc("SelectObject")
	pStretchBlt         = gdi32.NewProc("StretchBlt")
	pGdiFlush           = gdi32.NewProc("GdiFlush")
	pSetStretchBltMode  = gdi32.NewProc("SetStretchBltMode")
	pGetStockObject     = gdi32.NewProc("GetStockObject")
	pGetModuleHandleW   = kernel32.NewProc("GetModuleHandleW")
	pTimeBeginPeriod    = winmm.NewProc("timeBeginPeriod")
	pTimeEndPeriod      = winmm.NewProc("timeEndPeriod")

	pGetRawInputData         = user32.NewProc("GetRawInputData")
	pRegisterRawInputDevices = user32.NewProc("RegisterRawInputDevices")
	pClipCursor              = user32.NewProc("ClipCursor")
	pShowCursor              = user32.NewProc("ShowCursor")
	pMapWindowPoints         = user32.NewProc("MapWindowPoints")
	pSetWindowTextW          = user32.NewProc("SetWindowTextW")
	pInvalidateRect          = user32.NewProc("InvalidateRect")
)

const (
	wmDestroy      = 0x0002
	wmSize         = 0x0005
	wmActivate     = 0x0006
	wmKillFocus    = 0x0008
	wmPaint        = 0x000F
	wmClose        = 0x0010
	wmQuit         = 0x0012
	wmEraseBkgnd   = 0x0014
	wmKeyDown      = 0x0100
	wmKeyUp        = 0x0101
	wmChar         = 0x0102
	wmSysKeyDown   = 0x0104
	wmSysKeyUp     = 0x0105
	wmSysChar      = 0x0106
	wmMove         = 0x0003
	wmInput        = 0x00FF
	wmLButtonDown  = 0x0201
	wmLButtonUp    = 0x0202
	wmRButtonDown  = 0x0204
	wmRButtonUp    = 0x0205
	ridInput       = 0x10000003
	wsOverlapped   = 0x00CF0000 // WS_OVERLAPPEDWINDOW
	wsPopup        = 0x80000000
	wsVisible      = 0x10000000
	gwlStyle       = -16
	swShow         = 5
	pmRemove       = 1
	cwUseDefault   = 0x80000000
	idcArrow       = 32512
	idiApplication = 32512
	vkReturn       = 0x0D
	vkF4           = 0x73
	vkF11          = 0x7A
	swpFrameChange = 0x0020
	swpNoZOrder    = 0x0004
	swpNoOwnerZ    = 0x0200
	monDefNearest  = 2
	blackBrush     = 4
)

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type point struct{ X, Y int32 }
type msg struct {
	Hwnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       point
	LPrivate uint32
}
type rect struct{ Left, Top, Right, Bottom int32 }
type monitorInfo struct {
	CbSize    uint32
	RcMonitor rect
	RcWork    rect
	DwFlags   uint32
}
type paintStruct struct {
	Hdc         uintptr
	FErase      int32
	RcPaint     rect
	FRestore    int32
	FIncUpdate  int32
	RgbReserved [32]byte
}
type rawInputDevice struct {
	UsagePage uint16
	Usage     uint16
	Flags     uint32
	Target    uintptr
}

type bitmapInfoHeader struct {
	BiSize          uint32
	BiWidth         int32
	BiHeight        int32
	BiPlanes        uint16
	BiBitCount      uint16
	BiCompression   uint32
	BiSizeImage     uint32
	BiXPelsPerMeter int32
	BiYPelsPerMeter int32
	BiClrUsed       uint32
	BiClrImportant  uint32
}

type winApp struct {
	hwnd       uintptr
	a          *App
	pix        []uint32
	bmi        bitmapInfoHeader
	fullscreen bool
	savedRect  rect
	savedStyle uintptr
	captured   bool
	hidden     bool
	lastAbsX   int32
	lastAbsY   int32
	haveAbs    bool
	painted    int // frames presented
	blitFails  int // blit failures (logged)
	memDC      uintptr
	dibBits    []uint32 // pixels of the DIB section selected into memDC
	method     int      // drawDIB, drawStretchDI or drawSetDI
	verifyDone bool
	verifyOK   int
	verifyBad  int
	badRun     int
}

var theApp *winApp

func utf16(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }

func messageBox(text string) {
	pMessageBoxW.Call(0, uintptr(unsafe.Pointer(utf16(text))), uintptr(unsafe.Pointer(utf16("Rampart"))), 0x10)
}

func wndProc(hwnd, umsg, wparam, lparam uintptr) uintptr {
	w := theApp
	switch umsg {
	case wmClose:
		pDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		pPostQuitMessage.Call(0)
		return 0
	case wmPaint:
		var ps paintStruct
		hdc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		if w != nil && w.a != nil {
			w.present(hdc)
		}
		pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	case wmKillFocus:
		if w != nil && w.a != nil {
			w.a.FocusLost()
			w.release()
		}
	case wmSize, wmMove:
		if w != nil && w.captured {
			w.clip()
		}
	case wmInput:
		if w != nil && w.a != nil && w.captured {
			w.rawInput(lparam)
		}
	case wmLButtonDown, wmRButtonDown, wmLButtonUp, wmRButtonUp:
		if w == nil || w.a == nil {
			break
		}
		down := umsg == wmLButtonDown || umsg == wmRButtonDown
		btn := 0
		if umsg == wmRButtonDown || umsg == wmRButtonUp {
			btn = 1
		}
		if !w.captured {
			if down && !w.a.Menu.open {
				w.capture() // first click only grabs the mouse
			}
			return 0
		}
		w.a.MouseButton(btn, down)
		return 0
	case wmKeyDown, wmSysKeyDown, wmKeyUp, wmSysKeyUp:
		if w == nil || w.a == nil {
			break
		}
		isDown := umsg == wmKeyDown || umsg == wmSysKeyDown
		alt := lparam&(1<<29) != 0
		if isDown && (wparam == vkF11 || (alt && wparam == vkReturn)) {
			if lparam&(1<<30) == 0 {
				w.a.SetFullscreen(!w.fullscreen)
			}
			return 0
		}
		if isDown && alt && wparam == vkF4 {
			pDestroyWindow.Call(hwnd)
			return 0
		}
		sc := uint8(lparam >> 16)
		if sc == 0 || sc >= 0x80 {
			return 0
		}
		if isDown && lparam&(1<<30) != 0 {
			return 0 // auto-repeat
		}
		if isDown && sc == 0x58 && !w.a.Menu.open {
			w.release() // F12 opens the menu: give the cursor back
		}
		w.a.Key(sc, isDown)
		return 0
	case wmChar, wmSysChar:
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, umsg, wparam, lparam)
	return r
}

// rawInput reads relative mouse motion from a WM_INPUT message.
func (w *winApp) rawInput(lparam uintptr) {
	var buf [8]uint64 // 64 bytes, enough for RAWINPUT with a mouse
	size := uint32(unsafe.Sizeof(buf))
	r, _, _ := pGetRawInputData.Call(lparam, ridInput, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 24)
	if r == 0 || uint32(r) == 0xFFFFFFFF {
		return
	}
	b := (*[64]byte)(unsafe.Pointer(&buf[0]))
	if *(*uint32)(unsafe.Pointer(&b[0])) != 0 { // RIM_TYPEMOUSE
		return
	}
	flags := *(*uint16)(unsafe.Pointer(&b[24]))
	x := *(*int32)(unsafe.Pointer(&b[36]))
	y := *(*int32)(unsafe.Pointer(&b[40]))
	if flags&1 != 0 { // absolute (remote desktop, some tablets)
		if w.haveAbs {
			sw, _, _ := pGetSystemMetrics.Call(0)
			sh, _, _ := pGetSystemMetrics.Call(1)
			w.a.MouseMove(float64(x-w.lastAbsX)*float64(sw)/65535, float64(y-w.lastAbsY)*float64(sh)/65535)
		}
		w.lastAbsX, w.lastAbsY, w.haveAbs = x, y, true
		return
	}
	if x != 0 || y != 0 {
		w.a.MouseMove(float64(x), float64(y))
	}
}

func (w *winApp) clip() {
	var r rect
	pGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&r)))
	pMapWindowPoints.Call(w.hwnd, 0, uintptr(unsafe.Pointer(&r)), 2)
	pClipCursor.Call(uintptr(unsafe.Pointer(&r)))
}

func (w *winApp) capture() {
	w.captured = true
	w.haveAbs = false
	w.clip()
	if !w.hidden {
		pShowCursor.Call(0)
		w.hidden = true
	}
	w.setTitle()
}

func (w *winApp) release() {
	if w.captured {
		w.captured = false
		pClipCursor.Call(0)
		w.a.M.mouseButtons = 0
	}
	if w.hidden {
		pShowCursor.Call(1)
		w.hidden = false
	}
	w.setTitle()
}

func (w *winApp) setTitle() {
	t := ModName + " " + Version + "   -   F12: options   -   click the window to use the mouse"
	if w.captured {
		t = ModName + " " + Version + "   -   F12: options   -   mouse captured (Alt+Tab or F12 frees it)"
	}
	pSetWindowTextW.Call(w.hwnd, uintptr(unsafe.Pointer(utf16(t))))
}

func (w *winApp) present(hdc uintptr) {
	var cr rect
	pGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&cr)))
	cw, ch := cr.Right, cr.Bottom
	if cw <= 0 || ch <= 0 {
		return
	}
	// 320x200 shown at 4:3, as on a CRT
	ww, hh := cw, cw*3/4
	if hh > ch {
		hh = ch
		ww = ch * 4 / 3
	}
	x, y := (cw-ww)/2, (ch-hh)/2
	brush, _, _ := pGetStockObject.Call(blackBrush)
	if x > 0 {
		pFillRect.Call(hdc, uintptr(unsafe.Pointer(&rect{0, 0, x, ch})), brush)
		pFillRect.Call(hdc, uintptr(unsafe.Pointer(&rect{x + ww, 0, cw, ch})), brush)
	}
	if y > 0 {
		pFillRect.Call(hdc, uintptr(unsafe.Pointer(&rect{0, 0, cw, y})), brush)
		pFillRect.Call(hdc, uintptr(unsafe.Pointer(&rect{0, y + hh, cw, ch})), brush)
	}
	pSetStretchBltMode.Call(hdc, 3) // COLORONCOLOR
	var r uintptr
	var e error
	switch w.method {
	case drawDIB:
		copy(w.dibBits, w.pix)
		pGdiFlush.Call()
		r, _, e = pStretchBlt.Call(hdc, uintptr(x), uintptr(y), uintptr(ww), uintptr(hh), w.memDC, 0, 0, 320, 200, 0x00CC0020)
	case drawStretchDI:
		r, _, e = pStretchDIBits.Call(hdc, uintptr(x), uintptr(y), uintptr(ww), uintptr(hh),
			0, 0, 320, 200, uintptr(unsafe.Pointer(&w.pix[0])), uintptr(unsafe.Pointer(&w.bmi)), 0, 0x00CC0020)
	default: // unscaled, centred in a black window
		x, y, ww, hh = (cw-320)/2, (ch-200)/2, 320, 200
		pFillRect.Call(hdc, uintptr(unsafe.Pointer(&rect{0, 0, cw, ch})), brush)
		r, _, e = pSetDIBitsToDevic.Call(hdc, uintptr(x), uintptr(y), 320, 200, 0, 0, 0, 200,
			uintptr(unsafe.Pointer(&w.pix[0])), uintptr(unsafe.Pointer(&w.bmi)), 0)
	}
	w.painted++
	if r == 0 {
		w.blitFails++
		if w.blitFails <= 5 {
			logLine(fmt.Sprintf("%s failed: %v (client %dx%d, fullscreen %v)", drawNames[w.method], e, cw, ch, w.fullscreen))
		}
		if w.blitFails >= 3 && w.method+1 < drawMethods {
			w.method++
			w.blitFails = 0
			logLine("display: switching to " + drawNames[w.method])
		}
		return
	}
	w.verify(hdc, x, y, ww, hh)
}

// initGDI makes a 320x200 32-bit DIB section in a memory DC; frames are copied
// into it and stretched to the window with StretchBlt.
func (w *winApp) initGDI() {
	hdc, _, _ := pGetDC.Call(w.hwnd)
	defer pReleaseDC.Call(w.hwnd, hdc)
	w.memDC, _, _ = pCreateCompatibleDC.Call(hdc)
	var bits *uint32 // set by Windows to the DIB section's pixels
	dib, _, e := pCreateDIBSection.Call(hdc, uintptr(unsafe.Pointer(&w.bmi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if w.memDC == 0 || dib == 0 || bits == nil {
		logLine(fmt.Sprintf("DIB section unavailable (dc %x, dib %x: %v) - using StretchDIBits", w.memDC, dib, e))
		w.method = drawStretchDI
		return
	}
	pSelectObject.Call(w.memDC, dib)
	w.dibBits = unsafe.Slice(bits, 320*200)
	logLine("display: DIB section + StretchBlt")
}

// draw presents the frame through WM_PAINT (BeginPaint/EndPaint) rather than a
// stray GetDC, so the compositor always picks it up, windowed or fullscreen.
func (w *winApp) draw() {
	pInvalidateRect.Call(w.hwnd, 0, 0)
	pUpdateWindow.Call(w.hwnd)
}

func (w *winApp) setFullscreen(on bool) {
	if on == w.fullscreen {
		return
	}
	w.release()
	if on {
		pGetWindowRect.Call(w.hwnd, uintptr(unsafe.Pointer(&w.savedRect)))
		st, _, _ := pGetWindowLongPtrW.Call(w.hwnd, gwlStyleU())
		w.savedStyle = st
		mon, _, _ := pMonitorFromWindow.Call(w.hwnd, monDefNearest)
		mi := monitorInfo{CbSize: uint32(unsafe.Sizeof(monitorInfo{}))}
		pGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi)))
		pSetWindowLongPtrW.Call(w.hwnd, gwlStyleU(), wsPopup|wsVisible)
		r := mi.RcMonitor
		pSetWindowPos.Call(w.hwnd, 0, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), swpFrameChange|swpNoOwnerZ)
	} else {
		pSetWindowLongPtrW.Call(w.hwnd, gwlStyleU(), w.savedStyle)
		r := w.savedRect
		pSetWindowPos.Call(w.hwnd, 0, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), swpFrameChange|swpNoZOrder|swpNoOwnerZ)
	}
	w.fullscreen = on
	pInvalidateRect.Call(w.hwnd, 0, 0)
	var cr rect
	pGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&cr)))
	logLine(fmt.Sprintf("fullscreen %v: client %dx%d", on, cr.Right, cr.Bottom))
}

func gwlStyleU() uintptr { return ^uintptr(15) }

func saveDir() string {
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "Rampart")
	}
	exe, _ := os.Executable()
	return filepath.Join(filepath.Dir(exe), "RampartSave")
}

func main() {
	defer func() {
		if r := recover(); r != nil {
			logLine(fmt.Sprintf("crash: %v", r))
			messageBox(fmt.Sprintf("Rampart crashed:\n%v", r))
			os.Exit(1)
		}
	}()
	dir := saveDir()
	os.MkdirAll(dir, 0o755)
	openLog(dir)
	if fixCompat() {
		return
	}
	pSetProcessDPIAware.Call()
	pTimeBeginPeriod.Call(1)
	defer pTimeEndPeriod.Call(1)

	sink := openSink()
	logLine(fmt.Sprintf("audio device: %v (waveOutOpen=%d)", sink != nil, lastWaveErr))
	a, err := NewApp(dir, sink)
	if err != nil {
		messageBox("Could not start Rampart.\n\n" + err.Error())
		return
	}
	a.Log = logLine
	a.M.Log = logLine
	logLine("sound setting: " + a.S.Sound)
	w := &winApp{a: a, pix: make([]uint32, 320*200)}
	w.bmi = bitmapInfoHeader{BiSize: 40, BiWidth: 320, BiHeight: -200, BiPlanes: 1, BiBitCount: 32}
	theApp = w
	a.OnFullscreen = w.setFullscreen

	hinst, _, _ := pGetModuleHandleW.Call(0)
	cursor, _, _ := pLoadCursorW.Call(0, idcArrow)
	black, _, _ := pGetStockObject.Call(blackBrush)
	icon, _, _ := pLoadIconW.Call(hinst, 1) // embedded icon resource #1
	if icon == 0 {
		icon, _, _ = pLoadIconW.Call(0, idiApplication)
	}
	cls := utf16("RampartWindow")
	wc := wndClassEx{
		cbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		style:         0x0003, // CS_HREDRAW|CS_VREDRAW
		lpfnWndProc:   syscall.NewCallback(wndProc),
		hInstance:     hinst,
		hIcon:         icon,
		hCursor:       cursor,
		hbrBackground: black,
		lpszClassName: cls,
		hIconSm:       icon,
	}
	if r, _, e := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		messageBox("RegisterClassEx failed: " + e.Error())
		return
	}
	wr := rect{0, 0, 960, 720}
	pAdjustWindowRect.Call(uintptr(unsafe.Pointer(&wr)), wsOverlapped, 0)
	ww, wh := wr.Right-wr.Left, wr.Bottom-wr.Top
	sw, _, _ := pGetSystemMetrics.Call(0)
	sh, _, _ := pGetSystemMetrics.Call(1)
	x, y := max((int32(sw)-ww)/2, 0), max((int32(sh)-wh)/2, 0)
	hwnd, _, e := pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(utf16("Rampart"))),
		wsOverlapped|wsVisible, uintptr(x), uintptr(y), uintptr(ww), uintptr(wh), 0, 0, hinst, 0)
	if hwnd == 0 {
		messageBox("CreateWindow failed: " + e.Error())
		return
	}
	w.hwnd = hwnd
	w.initGDI()
	w.setTitle()
	pShowWindow.Call(hwnd, swShow)
	pUpdateWindow.Call(hwnd)
	rid := rawInputDevice{UsagePage: 1, Usage: 2, Flags: 0, Target: hwnd}
	pRegisterRawInputDevices.Call(uintptr(unsafe.Pointer(&rid)), 1, unsafe.Sizeof(rid))
	// A saved fullscreen setting is applied once the window has shown real frames:
	// switching a window that has never painted straight to a monitor-sized popup
	// could leave it white.
	wantFull := a.S.Fullscreen

	const frameDur = time.Second / 70
	pads := newXInput()
	if pads != nil {
		logLine("gamepads: " + pads.dll)
	} else {
		logLine("gamepads: no XInput DLL")
	}
	next := time.Now()
	var mm msg
	quit := false
	for !quit {
		for {
			r, _, _ := pPeekMessageW.Call(uintptr(unsafe.Pointer(&mm)), 0, 0, 0, pmRemove)
			if r == 0 {
				break
			}
			if mm.Message == wmQuit {
				quit = true
				break
			}
			pTranslateMessage.Call(uintptr(unsafe.Pointer(&mm)))
			pDispatchMessageW.Call(uintptr(unsafe.Pointer(&mm)))
		}
		if quit || a.Quitting() {
			break
		}
		now := time.Now()
		if now.Sub(next) > 250*time.Millisecond {
			next = now
		}
		ran := 0
		for !now.Before(next) && ran < 4 {
			if pads != nil {
				pads.poll(a)
			}
			a.Frame()
			if a.M.Frame == 700 {
				logLine(fmt.Sprintf("game sound state: card=%d music=%d digital=%d",
					a.M.dsb(0x6c15), a.M.dsb(0x55e8), a.M.dsb(0x55e9)))
				var sum uint64
				for _, c := range w.pix {
					sum += uint64(c>>16&255 + c>>8&255 + c&255)
				}
				var cr rect
				pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&cr)))
				logLine(fmt.Sprintf("display: frame brightness %d/765, painted %d, client %dx%d, fullscreen %v, method %s",
					sum/uint64(len(w.pix)), w.painted, cr.Right, cr.Bottom, w.fullscreen, drawNames[w.method]))
				writeBMP(filepath.Join(dir, "frame700.bmp"), w.pix)
			}
			next = next.Add(frameDur)
			ran++
		}
		if ran > 0 {
			a.Render(w.pix)
			w.draw()
			if wantFull && w.painted >= 3 {
				wantFull = false
				w.setFullscreen(true)
			}
		}
		if d := time.Until(next); d > 0 {
			time.Sleep(d)
		}
	}
	w.release()
	a.Close()
	if !quit {
		pDestroyWindow.Call(hwnd)
	}
}

// ----------------------------------------------------------------- logging

var logFile *os.File
var logLines int

func openLog(dir string) {
	flag := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if os.Getenv("RAMPART_RESTARTED") != "" {
		flag = os.O_CREATE | os.O_WRONLY | os.O_APPEND // keep the first run's lines
	}
	logFile, _ = os.OpenFile(filepath.Join(dir, "rampart.log"), flag, 0o644)
	logLine("Rampart player started " + time.Now().Format(time.RFC3339))
}

func logLine(s string) {
	if logFile == nil || logLines > 3000 {
		return
	}
	logLines++
	fmt.Fprintln(logFile, s)
	logFile.Sync()
}

// writeBMP saves a 320x200 frame (diagnostics: shows what the game drew,
// independent of how Windows displays it).
func writeBMP(path string, pix []uint32) {
	const W, H = 320, 200
	b := make([]byte, 54+W*H*4)
	le32 := func(o int, v uint32) { b[o], b[o+1], b[o+2], b[o+3] = byte(v), byte(v>>8), byte(v>>16), byte(v>>24) }
	b[0], b[1] = 'B', 'M'
	le32(2, uint32(len(b)))
	le32(10, 54)
	le32(14, 40)
	le32(18, W)
	le32(22, uint32(0x100000000-H)) // top-down
	b[26], b[28] = 1, 32
	for i, c := range pix[:W*H] {
		le32(54+4*i, c)
	}
	os.WriteFile(path, b, 0o644)
}
