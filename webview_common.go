package glaze

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"unsafe"
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

// WebView is the cross-platform handle returned by New and NewWindow. Its
// methods drive the native window and the embedded web view. Unless a method's
// own documentation says otherwise, call them from the UI thread (the goroutine
// that created the first window), and use Dispatch to re-enter that thread from
// background goroutines.
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
	// Example: w.SetHtml("<h1>Hello</h1>")
	SetHtml(html string)

	// Init injects JavaScript code at the initialization of the new page. Every
	// time the webview will open a the new page - this initialization code will
	// be executed. It is guaranteed that code is executed before window.onload.
	Init(js string)

	// Eval evaluates arbitrary JavaScript code. Evaluation happens asynchronously,
	// also the result of the expression is ignored. Use RPC bindings if you want
	// to receive notifications about the results of the evaluation.
	Eval(js string)

	// Focus moves keyboard focus into the web content, so typing - and a screen
	// reader's cursor - lands inside the page without the user having to click it
	// first. Each platform already does this when its window first appears and
	// when the window is re-activated; Focus is the explicit, on-demand version
	// for pulling focus back into the page. Call it from the UI thread.
	Focus()

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

	// --- Native file dialogs (a glaze extension; upstream webview has none) ---
	//
	// These present a native, application-modal file chooser. Unlike the other
	// WebView methods, they BLOCK the calling goroutine until the user dismisses
	// the dialog and therefore must NOT be called from the UI thread (doing so
	// deadlocks). Call them from a Bind callback - which runs on a background
	// goroutine - or any other goroutine. They require the main loop to be
	// running (Run has been called).
	//
	// A cancelled dialog returns an empty result and a nil error; a non-nil
	// error means the dialog could not be presented.

	// OpenFile shows an "open file" dialog and returns the chosen path, or "" if
	// the user cancelled.
	OpenFile(opts FileDialogOptions) (string, error)

	// OpenFiles shows an "open file" dialog that allows selecting multiple files
	// and returns the chosen paths, or nil if the user cancelled.
	OpenFiles(opts FileDialogOptions) ([]string, error)

	// SaveFile shows a "save file" dialog and returns the chosen path, or "" if
	// the user cancelled.
	SaveFile(opts FileDialogOptions) (string, error)

	// OpenDirectory shows a directory chooser and returns the chosen directory
	// path, or "" if the user cancelled.
	OpenDirectory(opts FileDialogOptions) (string, error)
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
		err := json.Unmarshal([]byte(req), &rawArgs)
		if err != nil {
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
			err = json.Unmarshal(rawArgs[i], argVal.Interface())
			if err != nil {
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
				v := res[0].Interface()
				if v != nil {
					return nil, v.(error)
				}
				return nil, nil //nolint:nilnil
			}
			return res[0].Interface(), nil
		case 2:
			var err error
			v := res[1].Interface()
			if v != nil {
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
//
// A panic in a user-supplied binding is recovered and turned into a rejected
// Promise (status -1) instead of crashing the host process and leaving the JS
// caller's Promise pending forever.
func callAndMarshal(fn func(id, req string) (any, error), id, req string) (status int, result string) {
	defer func() {
		r := recover()
		if r != nil {
			status = -1
			result = marshalJSON(fmt.Sprintf("binding panicked: %v", r))
		}
	}()

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
