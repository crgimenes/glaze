//go:build !darwin

package glaze

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// Init prepares the glaze runtime: loads the native webview library and
// resolves all required symbols. It is safe to call multiple times; only
// the first call has effect. New and NewWindow call Init automatically,
// but callers may invoke it earlier to fail fast (e.g. verify that the
// native library is available before building the rest of the UI).
func Init() error {
	initOnce.Do(func() {
		rt := &glazeRuntime{
			dispatchMap: make(map[uintptr]func()),
			bindingMap:  make(map[uintptr]bindingEntry),
			boundNames:  make(map[string]uintptr),
		}

		libHandle, err := loadLibrary(libraryPath())
		if err != nil {
			initErr = fmt.Errorf("webview: failed to load native library: %w", err)
			return
		}
		if libHandle == 0 {
			initErr = errors.New("webview: native library handle is nil")
			return
		}
		// Resolve all required symbols from the library.
		symbols := []struct {
			ptr  *uintptr
			name string
		}{
			{&rt.pCreate, "webview_create"},
			{&rt.pDestroy, "webview_destroy"},
			{&rt.pRun, "webview_run"},
			{&rt.pTerminate, "webview_terminate"},
			{&rt.pDispatch, "webview_dispatch"},
			{&rt.pGetWindow, "webview_get_window"},
			{&rt.pSetTitle, "webview_set_title"},
			{&rt.pSetSize, "webview_set_size"},
			{&rt.pNavigate, "webview_navigate"},
			{&rt.pSetHtml, "webview_set_html"},
			{&rt.pInit, "webview_init"},
			{&rt.pEval, "webview_eval"},
			{&rt.pBind, "webview_bind"},
			{&rt.pUnbind, "webview_unbind"},
			{&rt.pReturn, "webview_return"},
		}
		for _, s := range symbols {
			ptr, err := loadSymbol(libHandle, s.name)
			if err != nil {
				initErr = err
				return
			}
			*s.ptr = ptr
		}

		rt.initCallbacks()

		defaultRT = rt
	})
	return initErr
}

// New calls NewWindow to create a new window and a new webview instance. If debug
// is non-zero - developer tools will be enabled (if the platform supports them).
func New(debug bool) (WebView, error) { return NewWindow(debug, nil) }

// NewWindow creates a new webview instance. If debug is non-zero - developer
// tools will be enabled (if the platform supports them). Window parameter can be
// a pointer to the native window handle. If it's non-null - then child WebView is
// embedded into the given parent window. Otherwise a new window is created.
// Depending on the platform, a GtkWindow, NSWindow or HWND pointer can be passed
// here.
//
// The first successful call pins the calling goroutine to its current OS thread.
// Keep all direct UI calls on that goroutine; background goroutines must re-enter
// through Dispatch.
func NewWindow(debug bool, window unsafe.Pointer) (WebView, error) {
	if err := Init(); err != nil {
		return nil, err
	}
	uiThreadOnce.Do(runtime.LockOSThread)
	rt := defaultRT
	if rt == nil || rt.pCreate == 0 {
		return nil, errors.New("webview: native symbols are not initialized")
	}
	r1, _, _ := purego.SyscallN(rt.pCreate, boolToInt(debug), uintptr(window))
	if r1 == 0 {
		return nil, errors.New("webview: failed to create window")
	}
	return &webview{handle: r1, rt: rt}, nil
}

// webview is a concrete implementation of WebView using native library calls.
// Each instance holds a reference to the glazeRuntime that created it.
type webview struct {
	handle uintptr
	rt     *glazeRuntime
}

// glazeRuntime holds the loaded native library, resolved symbols, callbacks,
// and all mutable state for dispatch/binding. A single instance is created by
// Init() and stored in defaultRT.
type glazeRuntime struct {
	// Function pointers for native library functions.
	pCreate    uintptr
	pDestroy   uintptr
	pRun       uintptr
	pTerminate uintptr
	pDispatch  uintptr
	pGetWindow uintptr
	pSetTitle  uintptr
	pSetSize   uintptr
	pNavigate  uintptr
	pSetHtml   uintptr
	pInit      uintptr
	pEval      uintptr
	pBind      uintptr
	pUnbind    uintptr
	pReturn    uintptr

	// Callback function pointers registered with the native library.
	dispatchCB uintptr
	bindingCB  uintptr

	// State for managing dispatched functions.
	dispatchMu      sync.Mutex
	dispatchMap     map[uintptr]func()
	dispatchCounter uintptr

	// State for managing bound callbacks.
	bindMu         sync.Mutex
	bindingMap     map[uintptr]bindingEntry
	boundNames     map[string]uintptr
	bindingCounter uintptr
}

// bindingEntry stores a bound callback and associated webview handle.
type bindingEntry struct {
	fn func(id, req string) (any, error)
	w  uintptr
}

// Package-level state: the single runtime instance and its initialization guard.
var (
	initOnce  sync.Once
	initErr   error
	defaultRT *glazeRuntime

	uiThreadOnce sync.Once
)

func (w *webview) Run() {
	purego.SyscallN(w.rt.pRun, w.handle)
}

func (w *webview) Terminate() {
	// On Windows, we need to dispatch the terminate call to the main thread.
	// Remove once this is merged: https://github.com/webview/webview/pull/1240
	if runtime.GOOS == "windows" {
		w.Dispatch(func() { purego.SyscallN(w.rt.pTerminate, w.handle) })
		return
	}
	purego.SyscallN(w.rt.pTerminate, w.handle)
}

func (w *webview) Dispatch(f func()) {
	w.rt.dispatch(w.handle, f)
}

func (w *webview) Destroy() {
	purego.SyscallN(w.rt.pDestroy, w.handle)
}

func (w *webview) Window() unsafe.Pointer {
	r1, _, _ := purego.SyscallN(w.rt.pGetWindow, w.handle)
	// We take the address and then dereference it to avoid go vet reporting
	// a possible misuse of unsafe.Pointer on direct uintptr conversion.
	return *(*unsafe.Pointer)(unsafe.Pointer(&r1))
}

func (w *webview) SetTitle(title string) {
	cs, ptr := cString(title)
	purego.SyscallN(w.rt.pSetTitle, w.handle, uintptr(ptr))
	runtime.KeepAlive(cs)
}

func (w *webview) SetSize(width, height int, hint Hint) {
	purego.SyscallN(w.rt.pSetSize, w.handle, uintptr(width), uintptr(height), uintptr(hint))
}

func (w *webview) Navigate(url string) {
	cs, ptr := cString(url)
	purego.SyscallN(w.rt.pNavigate, w.handle, uintptr(ptr))
	runtime.KeepAlive(cs)
}

func (w *webview) SetHtml(html string) {
	cs, ptr := cString(html)
	purego.SyscallN(w.rt.pSetHtml, w.handle, uintptr(ptr))
	runtime.KeepAlive(cs)
}

func (w *webview) Init(js string) {
	cs, ptr := cString(js)
	purego.SyscallN(w.rt.pInit, w.handle, uintptr(ptr))
	runtime.KeepAlive(cs)
}

func (w *webview) Eval(js string) {
	cs, ptr := cString(js)
	purego.SyscallN(w.rt.pEval, w.handle, uintptr(ptr))
	runtime.KeepAlive(cs)
}

func (w *webview) Bind(name string, f any) error {
	fn, err := makeFuncWrapper(f)
	if err != nil {
		return err
	}

	w.rt.bindMu.Lock()
	if _, exists := w.rt.boundNames[name]; exists {
		w.rt.bindMu.Unlock()
		return errors.New("function name already bound")
	}
	contextKey := w.rt.bindingCounter
	w.rt.bindingCounter++
	w.rt.bindingMap[contextKey] = bindingEntry{w: w.handle, fn: fn}
	w.rt.boundNames[name] = contextKey
	w.rt.bindMu.Unlock()

	nameBytes, namePtr := cString(name)
	purego.SyscallN(w.rt.pBind, w.handle, uintptr(namePtr), w.rt.bindingCB, contextKey)
	runtime.KeepAlive(nameBytes)
	return nil
}

func (w *webview) Unbind(name string) error {
	w.rt.bindMu.Lock()
	contextKey, exists := w.rt.boundNames[name]
	if !exists {
		w.rt.bindMu.Unlock()
		return errors.New("function name not bound")
	}
	delete(w.rt.boundNames, name)
	delete(w.rt.bindingMap, contextKey)
	w.rt.bindMu.Unlock()
	cs, namePtr := cString(name)
	purego.SyscallN(w.rt.pUnbind, w.handle, uintptr(namePtr))
	runtime.KeepAlive(cs)
	return nil
}

func boolToInt(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}

func cString(s string) ([]byte, unsafe.Pointer) {
	b := append([]byte(s), 0)
	return b, unsafe.Pointer(&b[0])
}

// maxCStringLen is the upper bound for C string reads to prevent unbounded
// memory scanning if the native library returns a non-null-terminated pointer.
const maxCStringLen = 10 << 20 // 10 MiB

func goString(c uintptr) string {
	// We take the address and then dereference it to trick go vet from creating a possible misuse of unsafe.Pointer
	ptr := *(*unsafe.Pointer)(unsafe.Pointer(&c))
	if ptr == nil {
		return ""
	}
	var length int
	for length < maxCStringLen {
		if *(*byte)(unsafe.Add(ptr, uintptr(length))) == '\x00' {
			break
		}
		length++
	}
	return string(unsafe.Slice((*byte)(ptr), length))
}

func (rt *glazeRuntime) initCallbacks() {
	rt.dispatchCB = purego.NewCallback(func(_, arg uintptr) uintptr {
		rt.dispatchMu.Lock()
		fn := rt.dispatchMap[arg]
		delete(rt.dispatchMap, arg)
		rt.dispatchMu.Unlock()
		if fn != nil {
			fn()
		}
		return 0
	})

	rt.bindingCB = purego.NewCallback(func(idPtr, reqPtr, arg uintptr) uintptr {
		rt.bindMu.Lock()
		entry, ok := rt.bindingMap[arg]
		rt.bindMu.Unlock()
		if !ok {
			return 0
		}
		id := goString(idPtr)
		req := goString(reqPtr)
		go func() {
			status, resultJSON := callAndMarshal(entry.fn, id, req)
			rt.returnToUI(entry.w, id, status, resultJSON)
		}()
		return 0
	})
}

func (rt *glazeRuntime) dispatch(handle uintptr, f func()) {
	rt.dispatchMu.Lock()
	idx := rt.dispatchCounter
	rt.dispatchCounter++
	rt.dispatchMap[idx] = f
	rt.dispatchMu.Unlock()
	purego.SyscallN(rt.pDispatch, handle, rt.dispatchCB, idx)
}

func (rt *glazeRuntime) returnToUI(handle uintptr, id string, status int, resultJSON string) {
	idBytes, idPtr := cString(id)
	resultBytes, resultPtr := cString(resultJSON)
	rt.dispatch(handle, func() {
		purego.SyscallN(rt.pReturn, handle, uintptr(idPtr), uintptr(status), uintptr(resultPtr))
		runtime.KeepAlive(idBytes)
		runtime.KeepAlive(resultBytes)
	})
}
