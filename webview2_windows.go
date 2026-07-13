// Phase 2: the WebView2 engine over COM.
//
// Reimplements the WebView2 half of webview's win32_edge.hh: create the
// environment + controller asynchronously via completion-handler COM objects we
// implement, get ICoreWebView2, wire add_WebMessageReceived +
// AddScriptToExecuteOnDocumentCreated, and Navigate/SetHtml/Eval/Bind with the
// same wire-compatible JS bridge as the macOS/Linux backends.
//
// STATUS: written from the authoritative WebView2 vtable layouts (jchv/go-webview2)
// + the win32_edge.hh flow + ebiten's DirectX COM idiom. The dev host is macOS,
// so this is validated by cross-compilation only; runtime validation must happen
// on the windows-latest CI runner (which ships the Edge WebView2 Runtime).
//
// COM idiom (outbound): each interface is a `struct{ vtbl *...Vtbl }`; the vtbl
// is a struct of uintptr slots in exact IDL order; a method call is
// purego.SyscallN(i.vtbl.Method, this, args...). Inbound handler objects we
// implement use a Go-built vtable {QueryInterface, AddRef, Release, Invoke} of
// purego.NewCallback pointers; the objects live in package-global memory
// (kept alive, and Go's GC is non-moving) so the pointers handed to WebView2
// stay valid across the async creation window.

package glaze

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"github.com/ebitengine/purego"
)

var errNoWindow = errors.New("webview2: failed to create window")

// bridgePostFn for the Windows WebView2 backend: the chrome.webview channel
// (WebKit's messageHandlers used on macOS/Linux do not exist here).
const bridgePostFn = `function(message) {
  return window.chrome.webview.postMessage(message);
}`

// dbg prints spike diagnostics to stderr when WEBVIEW2_DEBUG is set (the
// headless CI self-test asserts on stdout, so stderr stays out of the way).
var debugEnabled = os.Getenv("WEBVIEW2_DEBUG") != ""

func dbg(format string, a ...any) {
	if debugEnabled {
		fmt.Fprintf(os.Stderr, "[webview2] "+format+"\n", a...)
	}
}

// ptr reinterprets a uintptr's bits as an unsafe.Pointer without a direct
// uintptr->Pointer conversion (keeps go vet happy).
func ptr(u uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&u)) }

// --- GUID / IID ------------------------------------------------------------

type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

func guidEqual(a, b *guid) bool {
	return a.Data1 == b.Data1 && a.Data2 == b.Data2 && a.Data3 == b.Data3 && a.Data4 == b.Data4
}

var (
	iidIUnknown             = guid{0x00000000, 0x0000, 0x0000, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidEnvironmentComplete  = guid{0x4E8A3389, 0xC9D8, 0x4BD2, [8]byte{0xB6, 0xB5, 0x12, 0x4F, 0xEE, 0x6C, 0xC1, 0x4D}}
	iidControllerComplete   = guid{0x6C4819F3, 0xC9B7, 0x4260, [8]byte{0x81, 0x27, 0xC9, 0xF5, 0xBD, 0xE7, 0xF6, 0x8C}}
	iidMessageReceived      = guid{0x57213F19, 0x00E6, 0x49FA, [8]byte{0x8E, 0x07, 0x89, 0x8E, 0xA0, 0x1E, 0xCB, 0xD2}}
	iidScriptAdded          = guid{0xB99369F3, 0x9B11, 0x47B5, [8]byte{0xBC, 0x6F, 0x8E, 0x78, 0x95, 0xFC, 0xEA, 0x17}}
	iidWebResourceRequested = guid{0xAB00B74C, 0x15F1, 0x4646, [8]byte{0x80, 0xE8, 0xE7, 0x63, 0x41, 0xD2, 0x5D, 0x71}}
)

// --- COM vtable layouts (exact IDL order; uintptr per slot) ----------------

type iUnknownVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
}

type iCoreWebView2EnvironmentVtbl struct {
	iUnknownVtbl
	CreateCoreWebView2Controller  uintptr
	CreateWebResourceResponse     uintptr
	GetBrowserVersionString       uintptr
	AddNewBrowserVersionAvailable uintptr
	RemoveNewBrowserVersionAvail  uintptr
}

type iCoreWebView2ControllerVtbl struct {
	iUnknownVtbl
	GetIsVisible                   uintptr
	PutIsVisible                   uintptr
	GetBounds                      uintptr
	PutBounds                      uintptr
	GetZoomFactor                  uintptr
	PutZoomFactor                  uintptr
	AddZoomFactorChanged           uintptr
	RemoveZoomFactorChanged        uintptr
	SetBoundsAndZoomFactor         uintptr
	MoveFocus                      uintptr
	AddMoveFocusRequested          uintptr
	RemoveMoveFocusRequested       uintptr
	AddGotFocus                    uintptr
	RemoveGotFocus                 uintptr
	AddLostFocus                   uintptr
	RemoveLostFocus                uintptr
	AddAcceleratorKeyPressed       uintptr
	RemoveAcceleratorKeyPressed    uintptr
	GetParentWindow                uintptr
	PutParentWindow                uintptr
	NotifyParentWindowPositionChng uintptr
	Close                          uintptr
	GetCoreWebView2                uintptr
}

type iCoreWebView2Vtbl struct {
	iUnknownVtbl
	GetSettings                         uintptr
	GetSource                           uintptr
	Navigate                            uintptr
	NavigateToString                    uintptr
	AddNavigationStarting               uintptr
	RemoveNavigationStarting            uintptr
	AddContentLoading                   uintptr
	RemoveContentLoading                uintptr
	AddSourceChanged                    uintptr
	RemoveSourceChanged                 uintptr
	AddHistoryChanged                   uintptr
	RemoveHistoryChanged                uintptr
	AddNavigationCompleted              uintptr
	RemoveNavigationCompleted           uintptr
	AddFrameNavigationStarting          uintptr
	RemoveFrameNavigationStarting       uintptr
	AddFrameNavigationCompleted         uintptr
	RemoveFrameNavigationCompleted      uintptr
	AddScriptDialogOpening              uintptr
	RemoveScriptDialogOpening           uintptr
	AddPermissionRequested              uintptr
	RemovePermissionRequested           uintptr
	AddProcessFailed                    uintptr
	RemoveProcessFailed                 uintptr
	AddScriptToExecuteOnDocumentCreated uintptr
	RemoveScriptToExecuteOnDocCreated   uintptr
	ExecuteScript                       uintptr
	CapturePreview                      uintptr
	Reload                              uintptr
	PostWebMessageAsJSON                uintptr
	PostWebMessageAsString              uintptr
	AddWebMessageReceived               uintptr
	RemoveWebMessageReceived            uintptr
	// The interface continues; we declare through AddWebResourceRequestedFilter
	// (needed for custom-scheme serving) so its vtbl offset is correct. Later
	// methods are omitted; none are called.
	CallDevToolsProtocolMethod             uintptr
	GetBrowserProcessID                    uintptr
	GetCanGoBack                           uintptr
	GetCanGoForward                        uintptr
	GoBack                                 uintptr
	GoForward                              uintptr
	GetDevToolsProtocolEventReceiver       uintptr
	Stop                                   uintptr
	AddNewWindowRequested                  uintptr
	RemoveNewWindowRequested               uintptr
	AddDocumentTitleChanged                uintptr
	RemoveDocumentTitleChanged             uintptr
	GetDocumentTitle                       uintptr
	AddHostObjectToScript                  uintptr
	RemoveHostObjectFromScript             uintptr
	OpenDevToolsWindow                     uintptr
	AddContainsFullScreenElementChanged    uintptr
	RemoveContainsFullScreenElementChanged uintptr
	GetContainsFullScreenElement           uintptr
	AddWebResourceRequested                uintptr
	RemoveWebResourceRequested             uintptr
	AddWebResourceRequestedFilter          uintptr
	RemoveWebResourceRequestedFilter       uintptr
}

type iCoreWebView2SettingsVtbl struct {
	iUnknownVtbl
	GetIsScriptEnabled                uintptr
	PutIsScriptEnabled                uintptr
	GetIsWebMessageEnabled            uintptr
	PutIsWebMessageEnabled            uintptr
	GetAreDefaultScriptDialogsEnabled uintptr
	PutAreDefaultScriptDialogsEnabled uintptr
	GetIsStatusBarEnabled             uintptr
	PutIsStatusBarEnabled             uintptr
	GetAreDevToolsEnabled             uintptr
	PutAreDevToolsEnabled             uintptr
	// (remaining omitted)
}

type iCoreWebView2WebMessageReceivedEventArgsVtbl struct {
	iUnknownVtbl
	GetSource             uintptr
	GetWebMessageAsJSON   uintptr
	TryGetWebMessageAsStr uintptr
}

// Interface pointer wrappers (the vtbl pointer is the object's first field).
type iEnvironment struct{ vtbl *iCoreWebView2EnvironmentVtbl }
type iController struct{ vtbl *iCoreWebView2ControllerVtbl }
type iCoreWebView2 struct{ vtbl *iCoreWebView2Vtbl }
type iSettings struct{ vtbl *iCoreWebView2SettingsVtbl }
type iMessageArgs struct {
	vtbl *iCoreWebView2WebMessageReceivedEventArgsVtbl
}

func asEnvironment(p uintptr) *iEnvironment { return (*iEnvironment)(ptr(p)) }
func asController(p uintptr) *iController   { return (*iController)(ptr(p)) }
func asWebView2(p uintptr) *iCoreWebView2   { return (*iCoreWebView2)(ptr(p)) }
func asSettings(p uintptr) *iSettings       { return (*iSettings)(ptr(p)) }
func asMessageArgs(p uintptr) *iMessageArgs { return (*iMessageArgs)(ptr(p)) }

func (i *iEnvironment) CreateController(hwnd, handler uintptr) uintptr {
	r, _, _ := purego.SyscallN(i.vtbl.CreateCoreWebView2Controller, uintptr(unsafe.Pointer(i)), hwnd, handler)
	return r
}
func (i *iController) GetCoreWebView2(out *uintptr) uintptr {
	r, _, _ := purego.SyscallN(i.vtbl.GetCoreWebView2, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(out)))
	return r
}
func (i *iController) PutIsVisible(v bool) {
	purego.SyscallN(i.vtbl.PutIsVisible, uintptr(unsafe.Pointer(i)), boolToUintptr(v))
}

// moveFocusReasonProgrammatic is COREWEBVIEW2_MOVE_FOCUS_REASON_PROGRAMMATIC:
// focus moved by the host, not by a Tab key. The reason is a plain enum (a
// scalar), so MoveFocus is not arch-specific the way putBounds is.
const moveFocusReasonProgrammatic = 0

// MoveFocus pushes keyboard focus into the hosted WebView2 content. Without it,
// the content stays unfocused until the user clicks the page - a keyboard and
// screen-reader accessibility gap, since focus on the host HWND does not reach
// the WebView2 child HWND on its own.
func (i *iController) MoveFocus(reason uintptr) {
	purego.SyscallN(i.vtbl.MoveFocus, uintptr(unsafe.Pointer(i)), reason)
}

// AddRef/Close/Release are the controller's IUnknown/lifetime methods.
// (putBounds is arch-specific; see putbounds_amd64.go and putbounds_arm64.go.)
func (i *iController) AddRef()  { purego.SyscallN(i.vtbl.AddRef, uintptr(unsafe.Pointer(i))) }
func (i *iController) Close()   { purego.SyscallN(i.vtbl.Close, uintptr(unsafe.Pointer(i))) }
func (i *iController) Release() { purego.SyscallN(i.vtbl.Release, uintptr(unsafe.Pointer(i))) }

func (i *iCoreWebView2) GetSettings(out *uintptr) uintptr {
	r, _, _ := purego.SyscallN(i.vtbl.GetSettings, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(out)))
	return r
}
func (i *iCoreWebView2) Navigate(url *uint16) {
	purego.SyscallN(i.vtbl.Navigate, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(url)))
}
func (i *iCoreWebView2) NavigateToString(html *uint16) {
	purego.SyscallN(i.vtbl.NavigateToString, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(html)))
}
func (i *iCoreWebView2) ExecuteScript(js *uint16, handler uintptr) {
	purego.SyscallN(i.vtbl.ExecuteScript, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(js)), handler)
}
func (i *iCoreWebView2) AddScript(js *uint16, handler uintptr) {
	purego.SyscallN(i.vtbl.AddScriptToExecuteOnDocumentCreated, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(js)), handler)
}
func (i *iCoreWebView2) RemoveScript(id *uint16) {
	purego.SyscallN(i.vtbl.RemoveScriptToExecuteOnDocCreated, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(id)))
}
func (i *iCoreWebView2) Release() {
	purego.SyscallN(i.vtbl.Release, uintptr(unsafe.Pointer(i)))
}
func (i *iCoreWebView2) AddWebMessageReceived(handler uintptr, token *uint64) {
	purego.SyscallN(i.vtbl.AddWebMessageReceived, uintptr(unsafe.Pointer(i)), handler, uintptr(unsafe.Pointer(token)))
}
func (i *iCoreWebView2) AddWebResourceRequested(handler uintptr, token *uint64) {
	purego.SyscallN(i.vtbl.AddWebResourceRequested, uintptr(unsafe.Pointer(i)), handler, uintptr(unsafe.Pointer(token)))
}
func (i *iCoreWebView2) AddWebResourceRequestedFilter(uri *uint16, ctx uint32) {
	purego.SyscallN(i.vtbl.AddWebResourceRequestedFilter, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(uri)), uintptr(ctx))
}
func (i *iCoreWebView2) AddRef() { purego.SyscallN(i.vtbl.AddRef, uintptr(unsafe.Pointer(i))) }

func (i *iSettings) PutAreDevToolsEnabled(v bool) {
	purego.SyscallN(i.vtbl.PutAreDevToolsEnabled, uintptr(unsafe.Pointer(i)), boolToUintptr(v))
}
func (i *iSettings) PutIsStatusBarEnabled(v bool) {
	purego.SyscallN(i.vtbl.PutIsStatusBarEnabled, uintptr(unsafe.Pointer(i)), boolToUintptr(v))
}
func (i *iMessageArgs) TryGetWebMessageAsString(out *uintptr) uintptr {
	r, _, _ := purego.SyscallN(i.vtbl.TryGetWebMessageAsStr, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(out)))
	return r
}

func boolToUintptr(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}

// --- inbound COM handler objects we implement ------------------------------

const (
	kindEnv = iota
	kindController
	kindMessage
	kindScript
	kindWebResourceRequested
)

type comHandlerVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
	Invoke         uintptr
}

// comHandler is a COM object we hand to WebView2. Its first field MUST be the
// vtbl pointer. Instances are kept alive in handlerKeepAlive (Go's GC is
// non-moving, so the address stays valid for WebView2).
type comHandler struct {
	vtbl     *comHandlerVtbl
	iid      *guid
	engineID uintptr
	kind     int
	refCount int32
}

var (
	sharedHandlerVtbl *comHandlerVtbl
	handlerMu         sync.Mutex
	handlerKeepAlive  []*comHandler
)

func newHandler(engineID uintptr, kind int, iid *guid) *comHandler {
	h := &comHandler{vtbl: sharedHandlerVtbl, iid: iid, engineID: engineID, kind: kind, refCount: 1}
	handlerMu.Lock()
	handlerKeepAlive = append(handlerKeepAlive, h)
	handlerMu.Unlock()
	return h
}

func handlerPtr(h *comHandler) uintptr     { return uintptr(unsafe.Pointer(h)) }
func handlerFrom(this uintptr) *comHandler { return (*comHandler)(ptr(this)) }

func handlerQueryInterface(this, riid, ppv uintptr) uintptr {
	if ppv == 0 {
		return 0x80004003 // E_POINTER
	}
	h := handlerFrom(this)
	want := (*guid)(ptr(riid))
	if guidEqual(want, h.iid) || guidEqual(want, &iidIUnknown) {
		*(*uintptr)(ptr(ppv)) = this
		atomic.AddInt32(&h.refCount, 1)
		return 0 // S_OK
	}
	*(*uintptr)(ptr(ppv)) = 0
	return 0x80004002 // E_NOINTERFACE
}

func handlerAddRef(this uintptr) uintptr {
	h := handlerFrom(this)
	return uintptr(atomic.AddInt32(&h.refCount, 1))
}

func handlerRelease(this uintptr) uintptr {
	h := handlerFrom(this)
	// Never free: the object is owned by handlerKeepAlive for the app lifetime.
	n := atomic.AddInt32(&h.refCount, -1)
	if n < 1 {
		n = 1
	}
	return uintptr(n)
}

// handlerInvoke is the single Invoke for all handler kinds. The C signatures
// all reduce to (this, uintptr, uintptr) since every argument is pointer- or
// int-sized; we dispatch on the handler kind.
func handlerInvoke(this, a, b uintptr) uintptr {
	h := handlerFrom(this)
	dbg("invoke kind=%d a=0x%x b=0x%x", h.kind, a, b)
	w := lookupEngine(h.engineID)
	if w == nil {
		return 0
	}
	switch h.kind {
	case kindEnv:
		// Invoke(this, HRESULT res, ICoreWebView2Environment* env)
		if int32(a) >= 0 && b != 0 { // SUCCEEDED(res)
			w.environment = b // kept for CreateWebResourceResponse (custom schemes)
			if int32(asEnvironment(b).CreateController(w.window, handlerPtr(w.ctrlH))) < 0 {
				w.ready = true // CreateController failed synchronously; unblock embed.
			}
		} else {
			w.ready = true // environment creation failed; unblock embed (controller stays 0).
		}
	case kindController:
		// Invoke(this, HRESULT res, ICoreWebView2Controller* controller)
		if int32(a) >= 0 && b != 0 {
			ctrl := asController(b)
			var wv uintptr
			ctrl.GetCoreWebView2(&wv)
			ctrl.AddRef()
			w.controller = b
			w.webview2 = wv
			if wv != 0 {
				cw := asWebView2(wv)
				cw.AddRef()
				var token uint64
				cw.AddWebMessageReceived(handlerPtr(w.msgH), &token)
				// Custom-scheme serving: intercept the per-scheme https vhost and
				// answer from the SchemeHandler (see serveSchemeWindows).
				if len(w.schemeHandlers) > 0 {
					w.wrrH = newHandler(w.id, kindWebResourceRequested, &iidWebResourceRequested)
					var wrrTok uint64
					cw.AddWebResourceRequested(handlerPtr(w.wrrH), &wrrTok)
					for scheme := range w.schemeHandlers {
						cw.AddWebResourceRequestedFilter(utf16(schemeVHost(scheme)+"/*"), 0) // 0 = ALL
					}
				}
			}
		}
		w.ready = true
	case kindMessage:
		// Invoke(this, ICoreWebView2* sender, ICoreWebView2WebMessageReceivedEventArgs* args)
		if b != 0 {
			var pwstr uintptr
			if int32(asMessageArgs(b).TryGetWebMessageAsString(&pwstr)) >= 0 && pwstr != 0 {
				msg := wideToString(pwstr)
				coTaskMemFree(pwstr)
				w.onMessage(msg)
			}
		}
	case kindScript:
		// Invoke(this, HRESULT res, LPCWSTR id)
		if int32(a) >= 0 && b != 0 {
			w.lastScript = wideToString(b)
		}
		w.scriptDone = true
	case kindWebResourceRequested:
		// Invoke(this, ICoreWebView2* sender, ICoreWebView2WebResourceRequestedEventArgs* args)
		if b != 0 {
			w.serveSchemeWindows(b)
		}
	}
	return 0 // S_OK
}

// --- extra Win32 / COM functions (ole32, advapi32, user32 RECT) ------------

var (
	coInitializeEx func(reserved uintptr, coinit uint32) int32
	coTaskMemFree  func(p uintptr)

	regOpenKeyExW    func(key uintptr, subkey *uint16, opts, desired uint32, out *uintptr) int32
	regQueryValueExW func(key uintptr, name *uint16, reserved uintptr, typ *uint32, data *byte, dataLen *uint32) int32
	regCloseKey      func(key uintptr) int32

	getClientRect func(hwnd uintptr, r *rect) int32

	comInitOnce sync.Once
	comInitErr  error
)

type rect struct{ Left, Top, Right, Bottom int32 }

func ensureCOMInit() error {
	comInitOnce.Do(func() {
		err := ensureWinInit()
		if err != nil {
			comInitErr = err
			return
		}
		ole32, err := syscall.LoadLibrary("ole32.dll")
		if err != nil {
			comInitErr = fmt.Errorf("load ole32.dll: %w", err)
			return
		}
		advapi32, err := syscall.LoadLibrary("advapi32.dll")
		if err != nil {
			comInitErr = fmt.Errorf("load advapi32.dll: %w", err)
			return
		}
		user32, err := syscall.LoadLibrary("user32.dll")
		if err != nil {
			comInitErr = fmt.Errorf("load user32.dll: %w", err)
			return
		}
		reg := func(fn any, dll syscall.Handle, name string) {
			if comInitErr != nil {
				return
			}
			addr, e := syscall.GetProcAddress(dll, name)
			if e != nil {
				comInitErr = fmt.Errorf("resolve %s: %w", name, e)
				return
			}
			purego.RegisterFunc(fn, addr)
		}
		reg(&coInitializeEx, ole32, "CoInitializeEx")
		reg(&coTaskMemFree, ole32, "CoTaskMemFree")
		reg(&regOpenKeyExW, advapi32, "RegOpenKeyExW")
		reg(&regQueryValueExW, advapi32, "RegQueryValueExW")
		reg(&regCloseKey, advapi32, "RegCloseKey")
		reg(&getClientRect, user32, "GetClientRect")
		if comInitErr != nil {
			return
		}
		sharedHandlerVtbl = &comHandlerVtbl{
			QueryInterface: purego.NewCallback(handlerQueryInterface),
			AddRef:         purego.NewCallback(handlerAddRef),
			Release:        purego.NewCallback(handlerRelease),
			Invoke:         purego.NewCallback(handlerInvoke),
		}
	})
	return comInitErr
}

// --- WebView2 loader: registry discovery, zero bundled DLL -----------------

const (
	hkeyLocalMachine = 0x80000002
	hkeyCurrentUser  = 0x80000001
	keyRead          = 0x20019
	keyWow6432Key    = 0x0200

	edgeClientStateKey = `SOFTWARE\Microsoft\EdgeUpdate\ClientState\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`
	minAPIVersion      = 1150
)

// findEmbeddedBrowserDLL locates the installed Edge WebView2 Runtime's
// EmbeddedBrowserWebView.dll via the registry (HKLM then HKCU), reimplementing
// loader.hh's built-in discovery so no DLL is bundled.
func findEmbeddedBrowserDLL() (string, error) {
	for _, root := range []uintptr{hkeyLocalMachine, hkeyCurrentUser} {
		val, err := regReadString(root, edgeClientStateKey, "EBWebView")
		if err != nil || val == "" {
			continue
		}
		// The value's last path component is the runtime version (e.g. 120.0.2210.91).
		version := filepath.Base(val)
		if !versionBuildAtLeast(version, minAPIVersion) {
			continue
		}
		dll := filepath.Join(val, "EBWebView", arch(), "EmbeddedBrowserWebView.dll")
		_, err = os.Stat(dll)
		if err == nil {
			return dll, nil
		}
	}
	return "", errors.New("webview2: Edge WebView2 Runtime not found (install it)")
}

func arch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x64"
	case "386":
		return "x86"
	case "arm64":
		return "arm64"
	}
	return "x64"
}

// versionBuildAtLeast reports whether the build field (3rd) of a dotted version
// string is >= min.
func versionBuildAtLeast(v string, min int) bool {
	parts := splitDots(v)
	if len(parts) < 3 {
		return false
	}
	return atoiSafe(parts[2]) >= min
}

func regReadString(root uintptr, subkey, name string) (string, error) {
	var key uintptr
	if regOpenKeyExW(root, utf16(subkey), 0, keyRead|keyWow6432Key, &key) != 0 {
		return "", errors.New("regOpenKeyExW failed")
	}
	defer regCloseKey(key)
	namePtr := utf16(name)
	var size uint32
	if regQueryValueExW(key, namePtr, 0, nil, nil, &size) != 0 || size == 0 {
		return "", errors.New("regQueryValueExW size query failed")
	}
	buf := make([]byte, size)
	if regQueryValueExW(key, namePtr, 0, nil, &buf[0], &size) != 0 {
		return "", errors.New("regQueryValueExW read failed")
	}
	u16 := unsafe.Slice((*uint16)(unsafe.Pointer(&buf[0])), size/2)
	// Trim trailing NUL(s).
	for len(u16) > 0 && u16[len(u16)-1] == 0 {
		u16 = u16[:len(u16)-1]
	}
	return string(utf16Decode(u16)), nil
}

// createEnvironment loads the discovered runtime DLL and calls its
// CreateWebViewEnvironmentWithOptionsInternal export.
//
// Trade-off (deliberate): this is the internal/undocumented export that
// WebView2Loader.dll itself wraps, and calling it directly is what lets glaze
// bundle ZERO native DLLs. Microsoft documents that it may change or be removed,
// and that the stable, supported entry point is
// CreateCoreWebView2EnvironmentWithOptions -- but that one is only exported by
// WebView2Loader.dll, which would have to be shipped alongside the binary. glaze
// favors the zero-DLL design; if a future Edge runtime drops this export, the
// GetProcAddress below fails with a clear error rather than misbehaving.
func createEnvironment(userDataDir string, envHandler *comHandler) error {
	dll, err := findEmbeddedBrowserDLL()
	if err != nil {
		dbg("findEmbeddedBrowserDLL: %v", err)
		return err
	}
	dbg("runtime dll: %s", dll)
	mod, err := syscall.LoadLibrary(dll)
	if err != nil {
		return fmt.Errorf("load %s: %w", dll, err)
	}
	addr, err := syscall.GetProcAddress(mod, "CreateWebViewEnvironmentWithOptionsInternal")
	if err != nil {
		// This internal export is how glaze avoids bundling WebView2Loader.dll; an
		// incompatible/too-new Edge runtime that renamed or removed it lands here.
		return fmt.Errorf("resolve CreateWebViewEnvironmentWithOptionsInternal (internal WebView2 loader export; installed Edge runtime may be incompatible): %w", err)
	}
	// HRESULT(bool, webview2_runtime_type, PCWSTR userDataDir, IUnknown* options,
	//         ICoreWebView2CreateCoreWebView2EnvironmentCompletedHandler*)
	r, _, _ := purego.SyscallN(addr,
		1, // bool: true
		0, // runtime_type: installed
		uintptr(unsafe.Pointer(utf16(userDataDir))),
		0, // options: null
		handlerPtr(envHandler),
	)
	dbg("CreateWebViewEnvironmentWithOptionsInternal -> HRESULT 0x%08X", uint32(r))
	if int32(r) < 0 {
		return fmt.Errorf("CreateWebViewEnvironmentWithOptionsInternal: HRESULT 0x%08X", uint32(r))
	}
	return nil
}

func userDataFolder() string {
	base := os.Getenv("APPDATA")
	if base == "" {
		base = os.TempDir()
	}
	exe, _ := os.Executable()
	return filepath.Join(base, filepath.Base(exe))
}

// --- embed + the WebView2-backed WebView methods ---------------------------

const coinitApartmentThreaded = 0x2

func (w *webview) embed(debug bool) error {
	err := ensureCOMInit()
	if err != nil {
		return err
	}
	coInitializeEx(0, coinitApartmentThreaded) // tolerate S_OK/S_FALSE/RPC_E_CHANGED_MODE

	w.envH = newHandler(w.id, kindEnv, &iidEnvironmentComplete)
	w.ctrlH = newHandler(w.id, kindController, &iidControllerComplete)
	w.msgH = newHandler(w.id, kindMessage, &iidMessageReceived)
	w.scriptH = newHandler(w.id, kindScript, &iidScriptAdded)

	dbg("embed: requesting environment (userDataFolder=%s)", userDataFolder())
	err = createEnvironment(userDataFolder(), w.envH)
	if err != nil {
		return err
	}

	// Pump the message loop until the controller + webview are ready.
	dbg("embed: pumping until ready")
	var m msgStruct
	for !w.ready {
		r := getMessageW(&m, 0, 0, 0)
		if r <= 0 {
			break
		}
		if m.message == wmQuit {
			return errors.New("webview2: canceled before init")
		}
		translateMessage(&m)
		dispatchMessageW(&m)
	}
	dbg("embed: ready=%v controller=0x%x webview2=0x%x", w.ready, w.controller, w.webview2)
	if w.controller == 0 || w.webview2 == 0 {
		return errors.New("webview2: environment/controller creation failed")
	}

	var settings uintptr
	if int32(asWebView2(w.webview2).GetSettings(&settings)) >= 0 && settings != 0 {
		s := asSettings(settings)
		s.PutAreDevToolsEnabled(debug)
		s.PutIsStatusBarEnabled(false)
	}

	w.addUserScript(createInitScript(bridgePostFn))

	w.resizeWebView()
	asController(w.controller).PutIsVisible(true)
	showWindow(w.window, swShow)
	updateWindow(w.window)
	// Pull keyboard focus into the content now that the controller exists. The
	// window already took WM_SETFOCUS during creation - before the controller was
	// ready, so that path could not move focus inward - so do it once here.
	asController(w.controller).MoveFocus(moveFocusReasonProgrammatic)
	return nil
}

func (w *webview) resizeWebView() {
	if w.controller == 0 || w.window == 0 {
		return
	}
	var r rect
	if getClientRect(w.window, &r) != 0 {
		asController(w.controller).putBounds(r)
	}
}

func (w *webview) Focus() {
	if w.controller == 0 {
		return
	}
	asController(w.controller).MoveFocus(moveFocusReasonProgrammatic)
}

func (w *webview) Navigate(url string) {
	url = w.rewriteSchemeURL(url) // map a registered scheme:// to its https vhost
	if w.webview2 == 0 {
		return
	}
	if url == "" {
		url = "about:blank"
	}
	asWebView2(w.webview2).Navigate(utf16(url))
}

func (w *webview) SetHtml(html string) {
	if w.webview2 != 0 {
		asWebView2(w.webview2).NavigateToString(utf16(html))
	}
}

func (w *webview) Eval(js string) {
	if w.webview2 != 0 {
		asWebView2(w.webview2).ExecuteScript(utf16(js), 0)
	}
}

func (w *webview) Init(js string) { w.addUserScript(js) }

// installDocScript adds one document-start script, pumps the message loop until
// WebView2 returns its id (matching win32_edge.hh's synchronous wait), and
// records that id in installedScriptIDs so it can be removed on rebuild.
//
// It must NOT be called with w.mu held: it pumps the loop, which can dispatch a
// WebMessageReceived into onMessage() (which locks w.mu). installedScriptIDs and
// userScriptSrcs are only ever touched on the UI thread.
func (w *webview) installDocScript(src string) {
	if w.webview2 == 0 {
		return
	}
	w.scriptDone = false
	w.lastScript = ""
	asWebView2(w.webview2).AddScript(utf16(src), handlerPtr(w.scriptH))
	var m msgStruct
	for !w.scriptDone {
		r := getMessageW(&m, 0, 0, 0)
		if r <= 0 {
			break
		}
		if m.message == wmQuit {
			postQuitMessage(0) // re-post so Run() also terminates
			break
		}
		translateMessage(&m)
		dispatchMessageW(&m)
	}
	if w.lastScript != "" {
		w.installedScriptIDs = append(w.installedScriptIDs, w.lastScript)
	}
}

// addUserScript records a persistent document-start script (the bridge and
// Init() scripts) and installs it. Bind scripts are NOT recorded here; they are
// (re)generated by rebuildScripts from the bindings map.
func (w *webview) addUserScript(src string) {
	w.mu.Lock()
	w.userScriptSrcs = append(w.userScriptSrcs, src)
	w.mu.Unlock()
	w.installDocScript(src)
}

// removeAllDocScripts removes every installed document-start script by id.
func (w *webview) removeAllDocScripts() {
	if w.webview2 != 0 {
		cw := asWebView2(w.webview2)
		for _, id := range w.installedScriptIDs {
			cw.RemoveScript(utf16(id))
		}
	}
	w.installedScriptIDs = nil
}

// rebuildScripts re-installs the persistent scripts plus a single bind script
// for the currently-bound names, mirroring the macOS/Linux removeAllUserScripts
// + createBindScript rebuild. Like installDocScript it pumps the loop, so it
// must NOT be called with w.mu held.
func (w *webview) rebuildScripts() {
	w.removeAllDocScripts()
	w.mu.Lock()
	srcs := append([]string(nil), w.userScriptSrcs...)
	names := w.bindingNamesLocked()
	w.mu.Unlock()
	for _, src := range srcs {
		w.installDocScript(src)
	}
	w.installDocScript(createBindScript(names))
}

func (w *webview) bindingNamesLocked() []string {
	names := make([]string, 0, len(w.bindings))
	for n := range w.bindings {
		names = append(names, n)
	}
	return names
}

func (w *webview) Bind(name string, f any) error {
	wrapper, err := makeFuncWrapper(f)
	if err != nil {
		return err
	}
	w.mu.Lock()
	_, exists := w.bindings[name]
	if exists {
		w.mu.Unlock()
		return errors.New("function name already bound")
	}
	w.bindings[name] = wrapper
	w.mu.Unlock()
	// Rebuild the document-start bind script so the binding survives reloads,
	// then bind it live for the current document. Done without holding w.mu
	// (rebuildScripts pumps the message loop).
	w.rebuildScripts()
	w.Eval(fmt.Sprintf("if(window.__webview__){window.__webview__.onBind(%s)}", marshalJSON(name)))
	return nil
}

func (w *webview) Unbind(name string) error {
	w.mu.Lock()
	_, exists := w.bindings[name]
	if !exists {
		w.mu.Unlock()
		return errors.New("function name not bound")
	}
	delete(w.bindings, name)
	w.mu.Unlock()
	w.rebuildScripts()
	w.Eval(fmt.Sprintf("if(window.__webview__){window.__webview__.onUnbind(%s)}", marshalJSON(name)))
	return nil
}

func (w *webview) onMessage(body string) {
	var m struct {
		ID     string          `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	err := json.Unmarshal([]byte(body), &m)
	if err != nil {
		return
	}
	w.mu.Lock()
	fn := w.bindings[m.Method]
	w.mu.Unlock()
	if fn == nil {
		return
	}
	go func() {
		status, result := callAndMarshal(fn, m.ID, string(m.Params))
		w.resolve(m.ID, status, result)
	}()
}

func (w *webview) resolve(id string, status int, resultJSON string) {
	js := fmt.Sprintf("window.__webview__.onReply(%s, %d, %s)", marshalJSON(id), status, marshalJSON(resultJSON))
	w.Dispatch(func() { w.Eval(js) })
}

// --- wide-string + small helpers -------------------------------------------

func wideToString(p uintptr) string {
	if p == 0 {
		return ""
	}
	base := ptr(p)
	var n int
	for *(*uint16)(unsafe.Add(base, uintptr(n)*2)) != 0 {
		n++
	}
	return string(utf16Decode(unsafe.Slice((*uint16)(base), n)))
}

func utf16Decode(u []uint16) []rune {
	out := make([]rune, 0, len(u))
	for i := 0; i < len(u); i++ {
		c := u[i]
		switch {
		case c >= 0xD800 && c < 0xDC00 && i+1 < len(u):
			lo := u[i+1]
			out = append(out, (rune(c-0xD800)<<10|rune(lo-0xDC00))+0x10000)
			i++
		default:
			out = append(out, rune(c))
		}
	}
	return out
}

func splitDots(s string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func atoiSafe(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return n
		}
		n = n*10 + int(s[i]-'0')
	}
	return n
}
