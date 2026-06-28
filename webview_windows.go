// Windows WebView backend in pure Go via purego + the Win32 API.
//
// This file is the Win32 windowing layer (window class + WndProc via
// purego.NewCallback, the GetMessage/Translate/Dispatch loop, WM_APP dispatch,
// teardown). The WebView2/COM engine is in webview2_windows.go. Together they
// reimplement webview's win32_edge.hh without cgo, so glaze needs no bundled
// webview.dll on Windows.
//
// Safety choice (per the COM/Win32 risk review): no Go pointer is ever handed
// across the C boundary. The engine is identified by an integer id passed via
// CreateWindow's lpCreateParams, stashed in GWLP_USERDATA, and looked up in a
// Go map; dispatched closures are keyed by an integer id passed via WM_APP's
// LPARAM. Only integers cross into C. purego has no Dlopen on Windows, so procs
// are resolved with syscall.LoadLibrary/GetProcAddress and bound via
// purego.RegisterFunc; the WndProc uses purego.NewCallback.

package glaze

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"github.com/ebitengine/purego"
)

const (
	cwUseDefault = ^int32(0x7fffffff) // 0x80000000 = CW_USEDEFAULT

	wsOverlappedWindow = 0x00CF0000
	wsThickFrame       = 0x00040000
	wsMaximizeBox      = 0x00010000

	swShow = 5

	gwlpUserData = -21
	gwlStyle     = -16

	wmDestroy       = 0x0002
	wmSize          = 0x0005
	wmSetFocus      = 0x0007
	wmClose         = 0x0010
	wmGetMinMaxInfo = 0x0024
	wmApp           = 0x8000
	wmQuit          = 0x0012
	wmNCCreate      = 0x0081

	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010
	swpNoMove     = 0x0002
)

// --- bound Win32 functions -------------------------------------------------

var (
	getModuleHandleW  func(name uintptr) uintptr
	registerClassExW  func(wc *wndClassExW) uint16
	createWindowExW   func(exStyle uint32, class, name *uint16, style uint32, x, y, w, h int32, parent, menu, inst, param uintptr) uintptr
	defWindowProcW    func(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr
	getMessageW       func(m *msgStruct, hwnd uintptr, min, max uint32) int32
	translateMessage  func(m *msgStruct) int32
	dispatchMessageW  func(m *msgStruct) uintptr
	postQuitMessage   func(code int32)
	postMessageW      func(hwnd uintptr, msg uint32, wp, lp uintptr) int32
	showWindow        func(hwnd uintptr, cmd int32) int32
	updateWindow      func(hwnd uintptr) int32
	destroyWindow     func(hwnd uintptr) int32
	setWindowLongPtrW func(hwnd uintptr, index int32, val uintptr) uintptr
	getWindowLongPtrW func(hwnd uintptr, index int32) uintptr
	setWindowTextW    func(hwnd uintptr, text *uint16) int32
	setWindowPos      func(hwnd, after uintptr, x, y, w, h int32, flags uint32) int32
)

// wndClassExW mirrors WNDCLASSEXW. The blank fields are left zero and unread by
// Go but kept so the struct's size/layout match what RegisterClassExW expects.
type wndClassExW struct {
	cbSize        uint32
	_             uint32 // style
	lpfnWndProc   uintptr
	_             int32 // cbClsExtra
	_             int32 // cbWndExtra
	hInstance     uintptr
	_             uintptr // hIcon
	_             uintptr // hCursor
	_             uintptr // hbrBackground
	_             *uint16 // lpszMenuName
	lpszClassName *uint16
	_             uintptr // hIconSm
}

type point struct{ X, Y int32 }

// minMaxInfo mirrors Win32 MINMAXINFO; wndProc fills it on WM_GETMINMAXINFO to
// enforce the HintMin/HintMax sizes (the equivalent of win32_edge.hh's m_minsz/
// m_maxsz handling).
type minMaxInfo struct {
	ptReserved     point
	ptMaxSize      point
	ptMaxPosition  point
	ptMinTrackSize point
	ptMaxTrackSize point
}

// msgStruct mirrors MSG. Only message is read by Go; the blank fields are filled
// by GetMessage and kept for the struct's C layout.
type msgStruct struct {
	_       uintptr // hwnd
	message uint32
	_       uint32  // padding after message
	_       uintptr // wParam
	_       uintptr // lParam
	_       uint32  // time
	_       point   // pt
	_       uint32  // lPrivate
}

var (
	winInitOnce sync.Once
	winInitErr  error

	wndProcCB uintptr // shared WndProc trampoline
)

func ensureWinInit() error {
	winInitOnce.Do(func() {
		user32, err := syscall.LoadLibrary("user32.dll")
		if err != nil {
			winInitErr = fmt.Errorf("webview: load user32.dll: %w", err)
			return
		}
		kernel32, err := syscall.LoadLibrary("kernel32.dll")
		if err != nil {
			winInitErr = fmt.Errorf("webview: load kernel32.dll: %w", err)
			return
		}
		reg := func(fn any, dll syscall.Handle, name string) {
			if winInitErr != nil {
				return
			}
			addr, e := syscall.GetProcAddress(dll, name)
			if e != nil {
				winInitErr = fmt.Errorf("webview: resolve %s: %w", name, e)
				return
			}
			purego.RegisterFunc(fn, addr)
		}
		reg(&getModuleHandleW, kernel32, "GetModuleHandleW")
		reg(&registerClassExW, user32, "RegisterClassExW")
		reg(&createWindowExW, user32, "CreateWindowExW")
		reg(&defWindowProcW, user32, "DefWindowProcW")
		reg(&getMessageW, user32, "GetMessageW")
		reg(&translateMessage, user32, "TranslateMessage")
		reg(&dispatchMessageW, user32, "DispatchMessageW")
		reg(&postQuitMessage, user32, "PostQuitMessage")
		reg(&postMessageW, user32, "PostMessageW")
		reg(&showWindow, user32, "ShowWindow")
		reg(&updateWindow, user32, "UpdateWindow")
		reg(&destroyWindow, user32, "DestroyWindow")
		reg(&setWindowLongPtrW, user32, "SetWindowLongPtrW")
		reg(&getWindowLongPtrW, user32, "GetWindowLongPtrW")
		reg(&setWindowTextW, user32, "SetWindowTextW")
		reg(&setWindowPos, user32, "SetWindowPos")
		if winInitErr != nil {
			return
		}
		wndProcCB = purego.NewCallback(wndProc)
	})
	return winInitErr
}

// utf16 returns a NUL-terminated UTF-16 pointer for s.
func utf16(s string) *uint16 {
	u := make([]uint16, 0, len(s)+1)
	for _, r := range s {
		if r < 0x10000 {
			u = append(u, uint16(r))
		} else {
			r -= 0x10000
			u = append(u, uint16(0xD800+(r>>10)), uint16(0xDC00+(r&0x3FF)))
		}
	}
	u = append(u, 0)
	return &u[0]
}

// --- engine registry (integer id <-> engine; no Go pointer crosses to C) ---

var (
	regMu     sync.Mutex
	registry  = map[uintptr]*webview{}
	engineSeq uintptr

	uiThreadOnce sync.Once

	// windowCount tracks live owned windows so a user-initiated close of the last
	// one ends Run() (mirrors the macOS backend's ref-count).
	windowCount int32
)

func registerEngine(w *webview) uintptr {
	regMu.Lock()
	engineSeq++
	id := engineSeq
	registry[id] = w
	regMu.Unlock()
	return id
}

func unregisterEngine(id uintptr) {
	regMu.Lock()
	delete(registry, id)
	regMu.Unlock()
}

func lookupEngine(id uintptr) *webview {
	regMu.Lock()
	defer regMu.Unlock()
	return registry[id]
}

// wndProc is the single window procedure for all engine windows. It recovers
// the engine via the integer id stored in GWLP_USERDATA (seeded in WM_NCCREATE
// from lpCreateParams).
func wndProc(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	var id uintptr
	if msg == wmNCCreate {
		// lp -> CREATESTRUCTW; lpCreateParams is the first field (offset 0).
		// Reinterpret the LPARAM bits as a pointer without a direct
		// uintptr->Pointer conversion (keeps go vet happy).
		cs := *(*unsafe.Pointer)(unsafe.Pointer(&lp))
		id = *(*uintptr)(cs)
		setWindowLongPtrW(hwnd, gwlpUserData, id)
	} else {
		id = getWindowLongPtrW(hwnd, gwlpUserData)
	}
	w := lookupEngine(id)
	if w == nil {
		return defWindowProcW(hwnd, msg, wp, lp)
	}

	switch msg {
	case wmApp:
		w.dispatchMu.Lock()
		f := w.dispatchMap[lp]
		delete(w.dispatchMap, lp)
		w.dispatchMu.Unlock()
		if f != nil {
			f()
		}
		return 0
	case wmSize:
		w.resizeWebView()
		return 0
	case wmSetFocus:
		// The host window gained keyboard focus (launch, Alt-Tab back, a title-bar
		// click); forward it into the WebView2 content so the keyboard and a screen
		// reader's cursor land in the page, matching the macOS/Linux backends. A
		// no-op until the controller exists (embed does the initial focus instead).
		if w.controller != 0 {
			asController(w.controller).MoveFocus(moveFocusReasonProgrammatic)
		}
		return 0
	case wmGetMinMaxInfo:
		// Enforce HintMin/HintMax, mirroring win32_edge.hh's WM_GETMINMAXINFO.
		mmi := (*minMaxInfo)(ptr(lp))
		if w.maxWidth > 0 && w.maxHeight > 0 {
			mmi.ptMaxSize = point{w.maxWidth, w.maxHeight}
			mmi.ptMaxTrackSize = point{w.maxWidth, w.maxHeight}
		}
		if w.minWidth > 0 && w.minHeight > 0 {
			mmi.ptMinTrackSize = point{w.minWidth, w.minHeight}
		}
		return 0
	case wmClose:
		// WM_CLOSE is the user-initiated close (the X button / Alt+F4); Destroy()
		// calls destroyWindow directly and never routes through here. destroyWindow
		// runs WM_DESTROY synchronously (which decrements windowCount), so once the
		// last owned window is closed this way we post WM_QUIT to end Run().
		owned := w.ownsWindow
		destroyWindow(hwnd)
		if owned && atomic.LoadInt32(&windowCount) <= 0 {
			postQuitMessage(0)
		}
		return 0
	case wmDestroy:
		// Closed via the OS or by Destroy(): reclaim the engine registry entry so
		// the webview is not pinned when Destroy() is never called (unregisterEngine
		// is idempotent), and drop the owned-window ref-count.
		unregisterEngine(id)
		if w.ownsWindow {
			atomic.AddInt32(&windowCount, -1)
		}
		w.window = 0
		setWindowLongPtrW(hwnd, gwlpUserData, 0)
		return 0
	default:
		return defWindowProcW(hwnd, msg, wp, lp)
	}
}

// --- webview ---------------------------------------------------------------

// webview is the Windows implementation of the WebView interface.
type webview struct {
	id         uintptr
	hinst      uintptr
	window     uintptr
	ownsWindow bool

	// WebView2 / COM state.
	controller uintptr // ICoreWebView2Controller*
	webview2   uintptr // ICoreWebView2*
	envH       *comHandler
	ctrlH      *comHandler
	msgH       *comHandler
	scriptH    *comHandler
	ready      bool
	scriptDone bool
	lastScript string

	// Window size constraints from SetSize(HintMin/HintMax); enforced in
	// wndProc's WM_GETMINMAXINFO handler.
	minWidth, minHeight int32
	maxWidth, maxHeight int32

	mu       sync.Mutex
	bindings map[string]func(id, req string) (any, error)
	// userScriptSrcs holds the persistent document-start scripts (the bridge +
	// Init() scripts), excluding bind scripts. installedScriptIDs holds the
	// WebView2 ids of every doc-start script currently installed, so Bind/Unbind
	// can remove and rebuild them (matching the macOS/Linux backends).
	userScriptSrcs     []string
	installedScriptIDs []string

	// Per-engine Dispatch queue: closures posted via WM_APP, keyed by an integer
	// id (no Go pointer crosses to C). Per-engine so Destroy can drop any pending
	// closures instead of leaking them in a shared global map.
	dispatchMu  sync.Mutex
	dispatchMap map[uintptr]func()
	dispatchSeq uintptr
}

var classNamePtr = utf16("glaze_webview")

// New creates a new window and a web view.
func New(debug bool) (WebView, error) { return NewWindow(debug, nil) }

// NewWindow creates a web view. If window is non-nil it must be an existing
// HWND to embed into; otherwise a new window is created and owned by this
// WebView. The first successful call pins the calling goroutine to its OS
// thread; create and drive every WebView from that same goroutine, because the
// WebView2 COM apartment and the message pump are thread-bound (use Dispatch to
// re-enter that thread from background goroutines).
func NewWindow(debug bool, window unsafe.Pointer) (WebView, error) {
	err := ensureWinInit()
	if err != nil {
		return nil, err
	}
	uiThreadOnce.Do(runtime.LockOSThread)

	w := &webview{
		ownsWindow:  window == nil,
		bindings:    map[string]func(id, req string) (any, error){},
		dispatchMap: map[uintptr]func(){},
	}
	w.id = registerEngine(w)
	w.hinst = getModuleHandleW(0)

	if w.ownsWindow {
		wc := wndClassExW{
			lpfnWndProc:   wndProcCB,
			hInstance:     w.hinst,
			lpszClassName: classNamePtr,
		}
		wc.cbSize = uint32(unsafe.Sizeof(wc))
		registerClassExW(&wc) // idempotent across instances (same class name)

		w.window = createWindowExW(
			0, classNamePtr, utf16(""), wsOverlappedWindow,
			cwUseDefault, cwUseDefault, 640, 480,
			0, 0, w.hinst, w.id, // lpCreateParams = engine id (integer)
		)
		if w.window == 0 {
			unregisterEngine(w.id)
			return nil, errNoWindow
		}
		atomic.AddInt32(&windowCount, 1)
	} else {
		w.window = uintptr(window)
		// Associate the engine id so wndProc-routed messages resolve (best
		// effort; the host owns the real window procedure).
		setWindowLongPtrW(w.window, gwlpUserData, w.id)
	}

	err = w.embed(debug)
	if err != nil {
		return nil, err
	}
	return w, nil
}

func (w *webview) Run() {
	var m msgStruct
	for getMessageW(&m, 0, 0, 0) > 0 {
		translateMessage(&m)
		dispatchMessageW(&m)
	}
}

func (w *webview) Terminate() {
	// PostQuitMessage posts WM_QUIT to the CALLING thread's queue. Bindings run
	// on goroutines (off the UI thread), so route it to the UI thread via the
	// dispatch queue, matching the native Windows backend.
	w.Dispatch(func() { postQuitMessage(0) })
}

func (w *webview) Dispatch(f func()) {
	w.dispatchMu.Lock()
	w.dispatchSeq++
	id := w.dispatchSeq
	w.dispatchMap[id] = f
	w.dispatchMu.Unlock()
	if postMessageW(w.window, wmApp, 0, id) == 0 {
		// The window is already gone: reclaim the entry rather than leak it.
		w.dispatchMu.Lock()
		delete(w.dispatchMap, id)
		w.dispatchMu.Unlock()
	}
}

func (w *webview) Window() unsafe.Pointer {
	p := w.window
	return *(*unsafe.Pointer)(unsafe.Pointer(&p))
}

func (w *webview) SetTitle(title string) { setWindowTextW(w.window, utf16(title)) }

func (w *webview) SetSize(width, height int, hint Hint) {
	// HintMin/HintMax only record constraints (enforced via WM_GETMINMAXINFO);
	// they do not resize the window, matching win32_edge.hh.
	switch hint {
	case HintMin:
		w.minWidth, w.minHeight = int32(width), int32(height)
		return
	case HintMax:
		w.maxWidth, w.maxHeight = int32(width), int32(height)
		return
	}
	style := getWindowLongPtrW(w.window, gwlStyle)
	if hint == HintFixed {
		style &^= uintptr(wsThickFrame | wsMaximizeBox)
	} else {
		style |= uintptr(wsThickFrame | wsMaximizeBox)
	}
	setWindowLongPtrW(w.window, gwlStyle, style)
	setWindowPos(w.window, 0, 0, 0, int32(width), int32(height),
		swpNoZOrder|swpNoActivate|swpNoMove)
	if w.ownsWindow {
		showWindow(w.window, swShow)
		updateWindow(w.window)
	}
}

func (w *webview) Destroy() {
	if w.controller != 0 {
		// Close the controller, then release the references we took in
		// handlerInvoke (ICoreWebView2 was AddRef'd, the controller too),
		// matching win32_edge.hh's teardown order.
		asController(w.controller).Close()
		if w.webview2 != 0 {
			asWebView2(w.webview2).Release()
			w.webview2 = 0
		}
		asController(w.controller).Release()
		w.controller = 0
	}
	if w.window != 0 && w.ownsWindow {
		destroyWindow(w.window)
		w.window = 0
	}
	// Drop any Dispatch closures that were queued but never delivered (their
	// WM_APP messages die with the window), instead of leaking them.
	w.dispatchMu.Lock()
	w.dispatchMap = map[uintptr]func(){}
	w.dispatchMu.Unlock()
	unregisterEngine(w.id)
}
