package glaze

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// Hints are used to configure window sizing and resizing.
type Hint int

const (
	// Width and height are default size.
	HintNone Hint = iota

	// Width and height are minimum bounds.
	HintMin

	// Width and height are maximum bounds.
	HintMax

	// Window size can not be changed by a user.
	HintFixed
)

type WebView interface {
	// Run runs the main loop until it's terminated. After this function exits -
	// you must destroy the webview.
	Run()

	// Terminate stops the main loop. It is safe to call this function from
	// a background thread.
	Terminate()

	// Dispatch posts a function to be executed on the main thread. You normally
	// do not need to call this function, unless you want to tweak the native
	// window.
	Dispatch(f func())

	// Destroy destroys a webview and closes the native window.
	Destroy()

	// Window returns a native window handle pointer. When using GTK backend the
	// pointer is GtkWindow pointer, when using Cocoa backend the pointer is
	// NSWindow pointer, when using Win32 backend the pointer is HWND pointer.
	Window() unsafe.Pointer

	// SetTitle updates the title of the native window. Must be called from the UI
	// thread.
	SetTitle(title string)

	// SetSize updates native window size. See Hint constants.
	SetSize(w, h int, hint Hint)

	// Navigate navigates webview to the given URL. URL may be a properly encoded data.
	// URI. Examples:
	// w.Navigate("https://github.com/webview/webview")
	// w.Navigate("data:text/html,%3Ch1%3EHello%3C%2Fh1%3E")
	// w.Navigate("data:text/html;base64,PGgxPkhlbGxvPC9oMT4=")
	Navigate(url string)

	// SetHtml sets the webview HTML directly.
	// Example: w.SetHtml(w, "<h1>Hello</h1>");
	SetHtml(html string)

	// Init injects JavaScript code at the initialization of the new page. Every
	// time the webview will open a the new page - this initialization code will
	// be executed. It is guaranteed that code is executed before window.onload.
	Init(js string)

	// Eval evaluates arbitrary JavaScript code. Evaluation happens asynchronously,
	// also the result of the expression is ignored. Use RPC bindings if you want
	// to receive notifications about the results of the evaluation.
	Eval(js string)

	// Bind binds a callback function so that it will appear under the given name
	// as a global JavaScript function. Internally it uses webview_init().
	// Callback receives a request string and a user-provided argument pointer.
	// Request string is a JSON array of all the arguments passed to the
	// JavaScript function.
	//
	// f must be a function
	// f must return either value and error or just error
	Bind(name string, f any) error

	// Removes a callback that was previously set by Bind.
	Unbind(name string) error
}

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

// VerifyBeforeLoad, when non-nil, is called with the resolved library path
// immediately before the native library is opened via dlopen/LoadLibrary.
// The embedded package sets this to a BLAKE2b-256 integrity check so that
// libraries replaced on disk after extraction are detected before loading.
var VerifyBeforeLoad func(path string) error

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

var errorType = reflect.TypeFor[error]()

// makeFuncWrapper inspects a user-supplied function "f" via reflection once,
// validating its signature and caching the relevant details.
// It returns a closure that, given (id, req string),
// decodes JSON args, calls the underlying function, and returns (value, error).
//
//nolint:cyclop,funlen
func makeFuncWrapper(f any) (func(id, req string) (any, error), error) {
	v := reflect.ValueOf(f)
	if v.Kind() != reflect.Func {
		return nil, errors.New("only functions can be bound")
	}

	funcType := v.Type()
	outCount := funcType.NumOut()
	if outCount > 2 {
		return nil, errors.New("function may only return a value or value+error")
	}

	numIn := funcType.NumIn()
	isVariadic := funcType.IsVariadic()
	inTypes := make([]reflect.Type, numIn)
	for i := range numIn {
		inTypes[i] = funcType.In(i)
	}

	var returnsError bool
	switch outCount {
	case 1:
		if funcType.Out(0).Implements(errorType) {
			returnsError = true
		}
	case 2:
		if !funcType.Out(1).Implements(errorType) {
			return nil, errors.New("second return value must implement error")
		}
	}

	fn := func(id, req string) (any, error) {
		var rawArgs []json.RawMessage
		if err := json.Unmarshal([]byte(req), &rawArgs); err != nil {
			return nil, err
		}
		if (!isVariadic && len(rawArgs) != numIn) || (isVariadic && len(rawArgs) < numIn-1) {
			return nil, errors.New("function arguments mismatch")
		}

		args := make([]reflect.Value, len(rawArgs))
		for i := range rawArgs {
			var argVal reflect.Value
			if isVariadic && i >= numIn-1 {
				argVal = reflect.New(inTypes[numIn-1].Elem())
			} else {
				argVal = reflect.New(inTypes[i])
			}
			if err := json.Unmarshal(rawArgs[i], argVal.Interface()); err != nil {
				return nil, err
			}
			args[i] = argVal.Elem()
		}

		res := v.Call(args)

		switch outCount {
		case 0:
			return nil, nil //nolint:nilnil
		case 1:
			if returnsError {
				if v := res[0].Interface(); v != nil {
					return nil, v.(error)
				}
				return nil, nil //nolint:nilnil
			}
			return res[0].Interface(), nil
		case 2:
			var err error
			if v := res[1].Interface(); v != nil {
				err = v.(error)
			}
			return res[0].Interface(), err
		default:
			panic("unreachable")
		}
	}

	return fn, nil
}

// callAndMarshal executes a bound function and marshals the result to JSON.
// Returns the status code (0 for success, -1 for error) and the JSON string.
func callAndMarshal(fn func(id, req string) (any, error), id, req string) (int, string) {
	resultValue, err := fn(id, req)
	if err != nil {
		return -1, marshalJSON(err.Error())
	}

	data, e := json.Marshal(resultValue)
	if e != nil {
		return -1, marshalJSON(e.Error())
	}
	return 0, string(data)
}

// marshalJSON JSON-encodes a string message for returning to JavaScript.
func marshalJSON(msg string) string {
	data, _ := json.Marshal(msg) // json.Marshal on string never fails
	return string(data)
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
