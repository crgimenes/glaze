package webview

import (
	_ "embed"
	"encoding/json"
	"errors"
	"reflect"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

func init() {
	// Ensure that main.main is called from the main thread.
	runtime.LockOSThread()
}

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

// New calls NewWindow to create a new window and a new webview instance. If debug
// is non-zero - developer tools will be enabled (if the platform supports them).
func New(debug bool) WebView { return NewWindow(debug, nil) }

// NewWindow creates a new webview instance. If debug is non-zero - developer
// tools will be enabled (if the platform supports them). Window parameter can be
// a pointer to the native window handle. If it's non-null - then child WebView is
// embedded into the given parent window. Otherwise a new window is created.
// Depending on the platform, a GtkWindow, NSWindow or HWND pointer can be passed
// here.
func NewWindow(debug bool, window unsafe.Pointer) WebView {
	loadOnce.Do(func() {
		libHandle, err := loadLibrary(libraryPath())
		if err != nil {
			panic("webview: failed to load native library: " + err.Error())
		}
		if libHandle == 0 {
			panic("webview: native library not loaded")
		}
		// Resolve all required symbols from the library
		pCreate = loadSymbol(libHandle, "webview_create")
		pDestroy = loadSymbol(libHandle, "webview_destroy")
		pRun = loadSymbol(libHandle, "webview_run")
		pTerminate = loadSymbol(libHandle, "webview_terminate")
		pDispatch = loadSymbol(libHandle, "webview_dispatch")
		pGetWindow = loadSymbol(libHandle, "webview_get_window")
		pSetTitle = loadSymbol(libHandle, "webview_set_title")
		pSetSize = loadSymbol(libHandle, "webview_set_size")
		pNavigate = loadSymbol(libHandle, "webview_navigate")
		pSetHtml = loadSymbol(libHandle, "webview_set_html")
		pInit = loadSymbol(libHandle, "webview_init")
		pEval = loadSymbol(libHandle, "webview_eval")
		pBind = loadSymbol(libHandle, "webview_bind")
		pUnbind = loadSymbol(libHandle, "webview_unbind")
		pReturn = loadSymbol(libHandle, "webview_return")
		dispatchCallback = purego.NewCallback(dispatchCallbackFn)
		bindingCallback = purego.NewCallback(bindingCallbackFn)
	})
	r1, _, _ := purego.SyscallN(pCreate, boolToInt(debug), uintptr(window))
	if r1 == 0 {
		panic("webview: failed to create window")
	}
	return &webview{handle: r1}
}

// webview is a concrete implementation of WebView using native library calls.
type webview struct {
	handle uintptr
}

// Global once to load native library symbols.
var loadOnce sync.Once

// Function pointers for native library functions.
var (
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
)

// Callback function pointers.
var (
	dispatchCallback uintptr
	bindingCallback  uintptr
)

// Global state for managing dispatched functions and bound callbacks.
var (
	dispatchMu      sync.Mutex
	dispatchMap     = make(map[uintptr]func())
	dispatchCounter uintptr

	bindMu         sync.Mutex
	bindingMap     = make(map[uintptr]bindingEntry)
	boundNames     = make(map[string]uintptr)
	bindingCounter uintptr
)

// bindingEntry stores a bound callback and associated webview handle.
type bindingEntry struct {
	fn func(id, req string) (any, error)
	w  uintptr
}

func (w *webview) Run() {
	purego.SyscallN(pRun, w.handle)
}

func (w *webview) Terminate() {
	// On Windows, we need to dispatch the terminate call to the main thread.
	// Remove once this is merged: https://github.com/webview/webview/pull/1240
	if runtime.GOOS == "windows" {
		w.Dispatch(func() { purego.SyscallN(pTerminate, w.handle) })
		return
	}
	purego.SyscallN(pTerminate, w.handle)
}

func (w *webview) Dispatch(f func()) {
	dispatchMu.Lock()
	idx := dispatchCounter
	dispatchCounter++
	dispatchMap[idx] = f
	dispatchMu.Unlock()
	purego.SyscallN(pDispatch, w.handle, dispatchCallback, idx)
}

func (w *webview) Destroy() {
	purego.SyscallN(pDestroy, w.handle)
}

func (w *webview) Window() unsafe.Pointer {
	r1, _, _ := purego.SyscallN(pGetWindow, w.handle)
	return unsafe.Pointer(r1)
}

func (w *webview) SetTitle(title string) {
	cs, ptr := cString(title)
	purego.SyscallN(pSetTitle, w.handle, ptr)
	runtime.KeepAlive(cs)
}

func (w *webview) SetSize(width, height int, hint Hint) {
	purego.SyscallN(pSetSize, w.handle, uintptr(width), uintptr(height), uintptr(hint))
}

func (w *webview) Navigate(url string) {
	cs, ptr := cString(url)
	purego.SyscallN(pNavigate, w.handle, ptr)
	runtime.KeepAlive(cs)
}

func (w *webview) SetHtml(html string) {
	cs, ptr := cString(html)
	purego.SyscallN(pSetHtml, w.handle, ptr)
	runtime.KeepAlive(cs)
}

func (w *webview) Init(js string) {
	cs, ptr := cString(js)
	purego.SyscallN(pInit, w.handle, ptr)
	runtime.KeepAlive(cs)
}

func (w *webview) Eval(js string) {
	cs, ptr := cString(js)
	purego.SyscallN(pEval, w.handle, ptr)
	runtime.KeepAlive(cs)
}

func (w *webview) Bind(name string, f any) error {
	fn, err := makeFuncWrapper(f)
	if err != nil {
		return err
	}

	bindMu.Lock()
	if _, exists := boundNames[name]; exists {
		bindMu.Unlock()
		return errors.New("function name already bound")
	}
	contextKey := bindingCounter
	bindingCounter++
	bindingMap[contextKey] = bindingEntry{w: w.handle, fn: fn}
	boundNames[name] = contextKey
	bindMu.Unlock()

	nameBytes, namePtr := cString(name)
	purego.SyscallN(pBind, w.handle, namePtr, bindingCallback, contextKey)
	runtime.KeepAlive(nameBytes)
	return nil
}

func (w *webview) Unbind(name string) error {
	bindMu.Lock()
	contextKey, exists := boundNames[name]
	if !exists {
		bindMu.Unlock()
		return errors.New("function name not bound")
	}
	delete(boundNames, name)
	delete(bindingMap, contextKey)
	bindMu.Unlock()
	cs, namePtr := cString(name)
	purego.SyscallN(pUnbind, w.handle, namePtr)
	runtime.KeepAlive(cs)
	return nil
}

var errorType = reflect.TypeOf((*error)(nil)).Elem()

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
	for i := 0; i < numIn; i++ {
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
				return nil, res[0].Interface().(error)
			}
			return res[0].Interface(), nil
		case 2:
			return res[0].Interface(), res[1].Interface().(error)
		default:
			panic("unreachable")
		}
	}

	return fn, nil
}

// dispatchCallbackFn executes a function posted with Dispatch on the main thread.
func dispatchCallbackFn(_, arg uintptr) uintptr {
	dispatchMu.Lock()
	fn := dispatchMap[arg]
	delete(dispatchMap, arg)
	dispatchMu.Unlock()
	if fn != nil {
		fn()
	}
	return 0
}

// bindingCallbackFn is invoked by the native webview when a bound JS function is called.
func bindingCallbackFn(idPtr, reqPtr, arg uintptr) uintptr {
	bindMu.Lock()
	entry, ok := bindingMap[arg]
	bindMu.Unlock()
	if !ok {
		return 0
	}

	id := goString(idPtr)
	req := goString(reqPtr)

	go func() {
		resultValue, err := entry.fn(id, req)
		status := 0
		var resultJSON string
		if err != nil { //nolint:nestif
			status = -1
			errMsg := err.Error()
			// Attempt to JSON-encode the error message
			if data, e := json.Marshal(errMsg); e == nil {
				resultJSON = string(data)
			} else {
				resultJSON = "\"" + errMsg + "\""
			}
		} else {
			// Attempt to JSON-encode the result value
			if data, e := json.Marshal(resultValue); e == nil {
				resultJSON = string(data)
			} else {
				status = -1
				msg := e.Error()
				// Fallback JSON-encode for the error
				if data2, e2 := json.Marshal(msg); e2 == nil {
					resultJSON = string(data2)
				} else {
					resultJSON = "\"" + msg + "\""
				}
			}
		}

		// Create new C string for the ID as the original one is no longer valid.
		idBytes, newIDPtr := cString(id)
		resBytes, newResPtr := cString(resultJSON)
		purego.SyscallN(pReturn, entry.w, newIDPtr, uintptr(status), newResPtr)
		runtime.KeepAlive(idBytes)
		runtime.KeepAlive(resBytes)
	}()

	return 0
}

func boolToInt(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}

func cString(s string) ([]byte, uintptr) {
	b := append([]byte(s), 0)
	return b, uintptr(unsafe.Pointer(&b[0]))
}

func goString(c uintptr) string {
	// We take the address and then dereference it to trick go vet from creating a possible misuse of unsafe.Pointer
	ptr := *(*unsafe.Pointer)(unsafe.Pointer(&c))
	if ptr == nil {
		return ""
	}
	var length int
	for {
		if *(*byte)(unsafe.Add(ptr, uintptr(length))) == '\x00' {
			break
		}
		length++
	}
	return string(unsafe.Slice((*byte)(ptr), length))
}
