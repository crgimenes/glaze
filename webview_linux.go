// Linux WebView backend in pure Go via purego's C-function bindings.
//
// This reimplements webview's GTK/WebKitGTK backend (gtk_webkitgtk.hh + the
// linux compat headers) by dlopen/dlsym-ing the system GTK and WebKitGTK shared
// objects directly, so glaze needs no cgo and no bundled libwebview.so on Linux.
// Detects the runtime stack: GTK4 + webkitgtk-6.0 when present, else GTK3 +
// webkit2gtk-4.1 (falling back to -4.0).

package glaze

import (
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

const (
	gtkWindowToplevel = 0

	gPriorityHighIdle = 100
	gSourceRemove     = 0

	injectTopFrame        = 1 // WEBKIT_USER_CONTENT_INJECT_TOP_FRAME
	injectAtDocumentStart = 0 // WEBKIT_USER_SCRIPT_INJECT_AT_DOCUMENT_START

	gdkHintMaxSize   = 1 << 2 // GDK_HINT_MAX_SIZE
	gSignalMatchData = 1 << 4 // G_SIGNAL_MATCH_DATA

	// Default window size applied when the caller never calls SetSize, matching
	// the macOS backend and webview's dispatch_size_default.
	defaultWidth  = 640
	defaultHeight = 480
)

// gdkGeometry mirrors the C GdkGeometry struct (passed by pointer for MAX hint).
type gdkGeometry struct {
	MinWidth, MinHeight   int32
	MaxWidth, MaxHeight   int32
	BaseWidth, BaseHeight int32
	WidthInc, HeightInc   int32
	MinAspect, MaxAspect  float64
	WinGravity            int32
	_                     int32
}

// --- bound C functions -----------------------------------------------------

var (
	gIdleAddFull                     func(priority int, function, data, notify uintptr) uint32
	gMainContextIteration            func(context uintptr, mayBlock bool) bool
	gFree                            func(ptr uintptr)
	gObjectRefSink                   func(obj uintptr) uintptr
	gObjectUnref                     func(obj uintptr)
	gSignalConnectData               func(instance uintptr, signal string, handler, data, destroy uintptr, flags int) uint64
	gSignalHandlersDisconnectMatched func(instance uintptr, mask int, signalID, detail uint32, closure, fn, data uintptr) uint32

	gtkInitCheck              func(argc, argv uintptr) bool
	gtkWindowNew              func(typ int) uintptr
	gtkWindowSetTitle         func(window uintptr, title string)
	gtkWindowSetResizable     func(window uintptr, resizable bool)
	gtkWindowResize           func(window uintptr, w, h int)
	gtkWidgetSetSizeRequest   func(widget uintptr, w, h int)
	gtkWindowSetGeometryHints func(window, widget uintptr, geom *gdkGeometry, mask int)
	gtkContainerAdd           func(container, widget uintptr)
	gtkContainerRemove        func(container, widget uintptr)
	gtkWidgetShow             func(widget uintptr)
	gtkWidgetGrabFocus        func(widget uintptr)
	gtkWindowClose            func(window uintptr)

	// GTK 4 variants (bound + used only when gtk4 is true).
	gtk4                    bool
	gtkInitCheck0           func() bool
	gtkWindowNew0           func() uintptr
	gtkWindowSetChild       func(window, widget uintptr)
	gtkWidgetSetVisible     func(widget uintptr, visible bool)
	gtkWindowSetDefaultSize func(window uintptr, w, h int)
	webkitRegisterHandler3  func(manager uintptr, name string, world uintptr)

	webkitWebViewNew                              func() uintptr
	webkitWebViewGetUserContentManager            func(webview uintptr) uintptr
	webkitWebViewGetSettings                      func(webview uintptr) uintptr
	webkitSettingsSetJavascriptCanAccessClipboard func(settings uintptr, enabled bool)
	webkitSettingsSetEnableWriteConsoleToStdout   func(settings uintptr, enabled bool)
	webkitSettingsSetEnableDeveloperExtras        func(settings uintptr, enabled bool)
	webkitWebViewLoadURI                          func(webview uintptr, uri string)
	webkitWebViewLoadHTML                         func(webview uintptr, html string, baseURI uintptr)
	webkitWebViewGetURI                           func(webview uintptr) uintptr
	webkitUserContentManagerRegisterHandler       func(manager uintptr, name string)
	webkitUserContentManagerAddScript             func(manager, script uintptr)
	webkitUserContentManagerRemoveAllScripts      func(manager uintptr)
	webkitUserScriptNew                           func(source string, frames, time int, allow, block uintptr) uintptr
	webkitUserScriptUnref                         func(script uintptr)
	webkitJavascriptResultGetJSValue              func(result uintptr) uintptr

	webkitWebViewEvaluateJavascript func(webview uintptr, script string, length int, world, source, cancellable, callback, userData uintptr)
	webkitWebViewRunJavascript      func(webview uintptr, script string, cancellable, callback, userData uintptr)
	haveEvaluateJavascript          bool

	jscValueToString func(value uintptr) uintptr
)

// --- one-time init ---------------------------------------------------------

var (
	initOnce     sync.Once
	initErr      error
	uiThreadOnce sync.Once

	dispatchSourceFn uintptr
	messageHandlerFn uintptr
	windowDestroyFn  uintptr

	// Library handles kept after ensureInit so other files (e.g. the file
	// dialogs in dialog_linux.go) can lazily resolve extra symbols without
	// re-dlopening or duplicating the soname-selection logic.
	gtkLib, glibLib uintptr
)

func openFirst(names ...string) (uintptr, error) {
	var lastErr error
	for _, n := range names {
		h, err := purego.Dlopen(n, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err == nil {
			return h, nil
		}
		lastErr = err
	}
	return 0, fmt.Errorf("webview: none of %v could be loaded: %w", names, lastErr)
}

// Init loads the system GTK + WebKitGTK libraries and resolves all symbols.
// Safe to call multiple times; New calls it.
func Init() error { return ensureInit() }

func ensureInit() error {
	initOnce.Do(func() {
		glib, err := openFirst("libglib-2.0.so.0")
		if err != nil {
			initErr = err
			return
		}
		gobject, err := openFirst("libgobject-2.0.so.0")
		if err != nil {
			initErr = err
			return
		}
		// Prefer the GTK4 + webkitgtk-6.0 stack; fall back to GTK3 + webkit2gtk-4.x.
		//
		// The deciding probe is the webkit library, NOT libgtk-4. A GTK3 desktop
		// commonly also has libgtk-4 installed (for newer apps), and dlopen-ing
		// both GTK3 and GTK4 into the same process corrupts the GObject type
		// system and crashes gtk_init ("cannot register existing type
		// 'GdkDisplayManager'"). So load libgtk-4 only when webkitgtk-6.0 is
		// actually present -- otherwise GTK4 never enters the process.
		var gtk, webkit, jsc uintptr
		wk6, werr := openFirst("libwebkitgtk-6.0.so.4")
		if werr == nil {
			gtk4 = true
			webkit = wk6
			gtk, err = openFirst("libgtk-4.so.1")
			if err != nil {
				initErr = err
				return
			}
			jsc, err = openFirst("libjavascriptcoregtk-6.0.so.1")
			if err != nil {
				initErr = err
				return
			}
		} else {
			gtk, err = openFirst("libgtk-3.so.0")
			if err != nil {
				initErr = err
				return
			}
			webkit, err = openFirst("libwebkit2gtk-4.1.so.0", "libwebkit2gtk-4.0.so.37")
			if err != nil {
				initErr = err
				return
			}
			jsc, err = openFirst("libjavascriptcoregtk-4.1.so.0", "libjavascriptcoregtk-4.0.so.18")
			if err != nil {
				initErr = err
				return
			}
		}

		gtkLib, glibLib = gtk, glib

		purego.RegisterLibFunc(&gIdleAddFull, glib, "g_idle_add_full")
		purego.RegisterLibFunc(&gMainContextIteration, glib, "g_main_context_iteration")
		purego.RegisterLibFunc(&gFree, glib, "g_free")
		purego.RegisterLibFunc(&gObjectRefSink, gobject, "g_object_ref_sink")
		purego.RegisterLibFunc(&gObjectUnref, gobject, "g_object_unref")
		purego.RegisterLibFunc(&gSignalConnectData, gobject, "g_signal_connect_data")
		// g_signal_handlers_disconnect_by_data is a macro, not a symbol.
		purego.RegisterLibFunc(&gSignalHandlersDisconnectMatched, gobject, "g_signal_handlers_disconnect_matched")

		if gtk4 {
			purego.RegisterLibFunc(&gtkInitCheck0, gtk, "gtk_init_check")
			purego.RegisterLibFunc(&gtkWindowNew0, gtk, "gtk_window_new")
			purego.RegisterLibFunc(&gtkWindowSetChild, gtk, "gtk_window_set_child")
			purego.RegisterLibFunc(&gtkWidgetSetVisible, gtk, "gtk_widget_set_visible")
			purego.RegisterLibFunc(&gtkWindowSetDefaultSize, gtk, "gtk_window_set_default_size")
		} else {
			purego.RegisterLibFunc(&gtkInitCheck, gtk, "gtk_init_check")
			purego.RegisterLibFunc(&gtkWindowNew, gtk, "gtk_window_new")
			purego.RegisterLibFunc(&gtkContainerAdd, gtk, "gtk_container_add")
			purego.RegisterLibFunc(&gtkContainerRemove, gtk, "gtk_container_remove")
			purego.RegisterLibFunc(&gtkWidgetShow, gtk, "gtk_widget_show")
			purego.RegisterLibFunc(&gtkWindowResize, gtk, "gtk_window_resize")
			purego.RegisterLibFunc(&gtkWindowSetGeometryHints, gtk, "gtk_window_set_geometry_hints")
		}
		purego.RegisterLibFunc(&gtkWindowSetTitle, gtk, "gtk_window_set_title")
		purego.RegisterLibFunc(&gtkWindowSetResizable, gtk, "gtk_window_set_resizable")
		purego.RegisterLibFunc(&gtkWidgetSetSizeRequest, gtk, "gtk_widget_set_size_request")
		purego.RegisterLibFunc(&gtkWidgetGrabFocus, gtk, "gtk_widget_grab_focus")
		purego.RegisterLibFunc(&gtkWindowClose, gtk, "gtk_window_close")

		purego.RegisterLibFunc(&webkitWebViewNew, webkit, "webkit_web_view_new")
		purego.RegisterLibFunc(&webkitWebViewGetUserContentManager, webkit, "webkit_web_view_get_user_content_manager")
		purego.RegisterLibFunc(&webkitWebViewGetSettings, webkit, "webkit_web_view_get_settings")
		purego.RegisterLibFunc(&webkitSettingsSetJavascriptCanAccessClipboard, webkit, "webkit_settings_set_javascript_can_access_clipboard")
		purego.RegisterLibFunc(&webkitSettingsSetEnableWriteConsoleToStdout, webkit, "webkit_settings_set_enable_write_console_messages_to_stdout")
		purego.RegisterLibFunc(&webkitSettingsSetEnableDeveloperExtras, webkit, "webkit_settings_set_enable_developer_extras")
		purego.RegisterLibFunc(&webkitWebViewLoadURI, webkit, "webkit_web_view_load_uri")
		purego.RegisterLibFunc(&webkitWebViewLoadHTML, webkit, "webkit_web_view_load_html")
		purego.RegisterLibFunc(&webkitWebViewGetURI, webkit, "webkit_web_view_get_uri")
		purego.RegisterLibFunc(&webkitUserContentManagerAddScript, webkit, "webkit_user_content_manager_add_script")
		purego.RegisterLibFunc(&webkitUserContentManagerRemoveAllScripts, webkit, "webkit_user_content_manager_remove_all_scripts")
		purego.RegisterLibFunc(&webkitUserScriptNew, webkit, "webkit_user_script_new")
		purego.RegisterLibFunc(&webkitUserScriptUnref, webkit, "webkit_user_script_unref")
		if gtk4 {
			// GTK4: the script-message callback delivers a JSCValue* directly, and
			// the handler registration takes a world-name argument.
			purego.RegisterLibFunc(&webkitRegisterHandler3, webkit, "webkit_user_content_manager_register_script_message_handler")
		} else {
			purego.RegisterLibFunc(&webkitUserContentManagerRegisterHandler, webkit, "webkit_user_content_manager_register_script_message_handler")
			purego.RegisterLibFunc(&webkitJavascriptResultGetJSValue, webkit, "webkit_javascript_result_get_js_value")
		}

		_, e := purego.Dlsym(webkit, "webkit_web_view_evaluate_javascript")
		if e == nil {
			purego.RegisterLibFunc(&webkitWebViewEvaluateJavascript, webkit, "webkit_web_view_evaluate_javascript")
			haveEvaluateJavascript = true
		} else {
			purego.RegisterLibFunc(&webkitWebViewRunJavascript, webkit, "webkit_web_view_run_javascript")
		}

		purego.RegisterLibFunc(&jscValueToString, jsc, "jsc_value_to_string")

		dispatchSourceFn = purego.NewCallback(func(data uintptr) uintptr {
			dispatchMu.Lock()
			f := dispatchMap[data]
			delete(dispatchMap, data)
			dispatchMu.Unlock()
			if f != nil {
				f()
			}
			return gSourceRemove
		})
		messageHandlerFn = purego.NewCallback(func(manager, jsResult, userData uintptr) uintptr {
			w := lookupEngine(userData)
			if w != nil {
				w.onMessage(jsResultToString(jsResult))
			}
			return 0
		})
		windowDestroyFn = purego.NewCallback(func(widget, userData uintptr) uintptr {
			w := lookupEngine(userData)
			if w != nil {
				w.onWindowDestroy()
			}
			return 0
		})
	})
	return initErr
}

// jsResultToString turns the script-message callback's second argument into a
// Go string. On GTK4 it is a JSCValue* directly; on GTK3 it is a
// WebKitJavascriptResult* that must be unwrapped first.
func jsResultToString(arg uintptr) string {
	value := arg
	if !gtk4 {
		value = webkitJavascriptResultGetJSValue(arg)
	}
	cs := jscValueToString(value)
	s := cstr(cs)
	if cs != 0 {
		gFree(cs)
	}
	return s
}

// gtkInit, gtkNewWindow and registerScriptHandler hide the GTK3/GTK4 call-arity
// differences.
func gtkInit() bool {
	if gtk4 {
		return gtkInitCheck0()
	}
	return gtkInitCheck(0, 0)
}

func gtkNewWindow() uintptr {
	if gtk4 {
		return gtkWindowNew0()
	}
	return gtkWindowNew(gtkWindowToplevel)
}

func registerScriptHandler(manager uintptr, name string) {
	if gtk4 {
		webkitRegisterHandler3(manager, name, 0) // default script world
		return
	}
	webkitUserContentManagerRegisterHandler(manager, name)
}

func cstr(p uintptr) string {
	if p == 0 {
		return ""
	}
	ptr := *(*unsafe.Pointer)(unsafe.Pointer(&p))
	var n int
	for *(*byte)(unsafe.Add(ptr, n)) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(ptr), n))
}

// --- instance + dispatch registries ----------------------------------------

var (
	regMu     sync.Mutex
	registry  = map[uintptr]*webview{}
	engineSeq uintptr

	dispatchMu  sync.Mutex
	dispatchMap = map[uintptr]func(){}
	dispatchSeq uintptr
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

func dispatchMain(f func()) {
	dispatchMu.Lock()
	dispatchSeq++
	id := dispatchSeq
	dispatchMap[id] = f
	dispatchMu.Unlock()
	gIdleAddFull(gPriorityHighIdle, dispatchSourceFn, id, 0)
}

// --- webview ---------------------------------------------------------------

// webview is the Linux implementation of the WebView interface.
type webview struct {
	id         uintptr
	window     uintptr
	webview    uintptr
	manager    uintptr
	ownsWindow bool

	stopRunLoop   bool
	isWindowShown bool
	isSizeSet     bool

	mu             sync.Mutex
	bindings       map[string]func(id, req string) (any, error)
	userScriptSrcs []string
	schemeHandlers map[string]SchemeHandler
	schemeCB       uintptr // retained purego trampoline
}

// serveScheme looks up the handler for a scheme and invokes it (nil if none).
func (w *webview) serveScheme(scheme, url string) *SchemeResponse {
	w.mu.Lock()
	h := w.schemeHandlers[scheme]
	w.mu.Unlock()
	if h == nil {
		return nil
	}
	return h(&SchemeRequest{URL: url})
}

// registerSchemes wires each SchemeHandler onto the web view's WebKitWebContext
// and marks the scheme as a secure context. Called before the first Navigate.
// It returns an error when a requested scheme cannot be registered (a missing
// library, handle, or symbol), rather than silently leaving the scheme
// unregistered so app:// pages fail to load with no diagnostic anywhere.
func (w *webview) registerSchemes() error {
	if len(w.schemeHandlers) == 0 {
		return nil
	}
	if w.webview == 0 {
		return errors.New("webview: register schemes: web view not created")
	}
	webkitSonames := []string{"libwebkit2gtk-4.1.so.0", "libwebkit2gtk-4.0.so.37"}
	if gtk4 {
		webkitSonames = []string{"libwebkitgtk-6.0.so.4"}
	}
	webkit, err := openFirst(webkitSonames...)
	if err != nil {
		return fmt.Errorf("webview: register schemes: load webkit: %w", err)
	}
	gio, err := openFirst("libgio-2.0.so.0")
	if err != nil {
		return fmt.Errorf("webview: register schemes: load gio: %w", err)
	}
	gobject, err := openFirst("libgobject-2.0.so.0")
	if err != nil {
		return fmt.Errorf("webview: register schemes: load gobject: %w", err)
	}
	gFreeAddr, err := purego.Dlsym(glibLib, "g_free")
	if err != nil {
		return fmt.Errorf("webview: register schemes: resolve g_free: %w", err)
	}
	// g_memdup2 (gsize length) only exists on GLib >= 2.68; on older GLib
	// (Debian 11, Ubuntu 20.04) fall back to g_memdup (guint length). Resolve
	// with Dlsym, not RegisterLibFunc, which panics when a symbol is absent.
	memdup, err := resolveMemdup(glibLib)
	if err != nil {
		return fmt.Errorf("webview: register schemes: %w", err)
	}

	var (
		getContext          func(uintptr) uintptr
		registerScheme      func(ctx uintptr, scheme string, cb, data, notify uintptr)
		getSecurityManager  func(uintptr) uintptr
		registerAsSecure    func(sm uintptr, scheme string)
		requestGetURI       func(uintptr) uintptr
		requestGetScheme    func(uintptr) uintptr
		schemeRequestFinish func(req, stream uintptr, streamLen int64, contentType string)
		memInputStreamNew   func(data unsafe.Pointer, length int, destroy uintptr) uintptr
		gObjectUnref        func(uintptr)
	)
	purego.RegisterLibFunc(&getContext, webkit, "webkit_web_view_get_context")
	purego.RegisterLibFunc(&registerScheme, webkit, "webkit_web_context_register_uri_scheme")
	purego.RegisterLibFunc(&getSecurityManager, webkit, "webkit_web_context_get_security_manager")
	purego.RegisterLibFunc(&registerAsSecure, webkit, "webkit_security_manager_register_uri_scheme_as_secure")
	purego.RegisterLibFunc(&requestGetURI, webkit, "webkit_uri_scheme_request_get_uri")
	purego.RegisterLibFunc(&requestGetScheme, webkit, "webkit_uri_scheme_request_get_scheme")
	purego.RegisterLibFunc(&schemeRequestFinish, webkit, "webkit_uri_scheme_request_finish")
	purego.RegisterLibFunc(&memInputStreamNew, gio, "g_memory_input_stream_new_from_data")
	purego.RegisterLibFunc(&gObjectUnref, gobject, "g_object_unref")

	ctx := getContext(w.webview)
	if ctx == 0 {
		return errors.New("webview: register schemes: web context is nil")
	}
	sm := getSecurityManager(ctx)
	if sm == 0 {
		return errors.New("webview: register schemes: security manager is nil")
	}

	// void (*WebKitURISchemeRequestCallback)(WebKitURISchemeRequest*, gpointer).
	// user_data is the engine id, so this resolves back to the right webview.
	w.schemeCB = purego.NewCallback(func(request uintptr, data uintptr) uintptr {
		eng := lookupEngine(data)
		if eng == nil {
			return 0
		}
		url := cstr(requestGetURI(request))
		scheme := cstr(requestGetScheme(request))
		resp := eng.serveScheme(scheme, url)
		var body []byte
		mime := "application/octet-stream"
		if resp != nil {
			body, mime = resp.Body, schemeMIME(resp)
		}
		// Copy into glib-owned memory freed by g_free once the stream is done, so
		// the bytes outlive this callback (the stream is read asynchronously).
		var dataPtr unsafe.Pointer
		if len(body) > 0 {
			dataPtr = memdup(unsafe.Pointer(&body[0]), len(body)) // #nosec G103 -- copied into glib memory, freed by g_free
		}
		stream := memInputStreamNew(dataPtr, len(body), uintptr(gFreeAddr))
		schemeRequestFinish(request, stream, int64(len(body)), mime)
		gObjectUnref(stream)
		return 0
	})
	for scheme := range w.schemeHandlers {
		registerScheme(ctx, scheme, w.schemeCB, w.id, 0)
		registerAsSecure(sm, scheme)
	}
	return nil
}

// resolveMemdup returns a "copy into glib-owned memory" function, preferring
// g_memdup2 (GLib >= 2.68, gsize length) and falling back to g_memdup (older
// GLib, guint length) so custom schemes still work on Debian 11 / Ubuntu 20.04.
// The copy is freed with g_free once the input stream has been read. Dlsym is
// used instead of RegisterLibFunc because the latter panics on a missing symbol
// and g_memdup2 legitimately is absent on older systems.
func resolveMemdup(glib uintptr) (func(mem unsafe.Pointer, size int) unsafe.Pointer, error) {
	if addr, err := purego.Dlsym(glib, "g_memdup2"); err == nil && addr != 0 {
		var f func(mem unsafe.Pointer, size uint64) unsafe.Pointer
		purego.RegisterFunc(&f, addr)
		return func(mem unsafe.Pointer, size int) unsafe.Pointer { return f(mem, uint64(size)) }, nil
	}
	if addr, err := purego.Dlsym(glib, "g_memdup"); err == nil && addr != 0 {
		var f func(mem unsafe.Pointer, size uint32) unsafe.Pointer
		purego.RegisterFunc(&f, addr)
		return func(mem unsafe.Pointer, size int) unsafe.Pointer { return f(mem, uint32(size)) }, nil
	}
	return nil, errors.New("neither g_memdup2 nor g_memdup is available")
}

// New creates a new window and a web view.
func New(debug bool) (WebView, error) { return NewWindow(debug, nil) }

// NewWindow creates a web view. If window is non-nil it must point to an
// existing GtkWindow to embed into; otherwise a new window is created.
//
// The first successful call pins the calling goroutine to its OS thread.
func NewWindow(debug bool, window unsafe.Pointer) (WebView, error) {
	return NewWithOptions(Options{Debug: debug, Window: window})
}

// NewWithOptions creates a web view configured by opts, including any custom
// SchemeHandlers (registered on the web view's context and marked as secure).
func NewWithOptions(opts Options) (WebView, error) {
	err := ensureInit()
	if err != nil {
		return nil, err
	}
	uiThreadOnce.Do(runtime.LockOSThread)

	w := &webview{
		ownsWindow:     true,
		bindings:       map[string]func(id, req string) (any, error){},
		schemeHandlers: opts.SchemeHandlers,
	}
	w.id = registerEngine(w)
	err = w.windowInit(uintptr(opts.Window))
	if err != nil {
		unregisterEngine(w.id)
		return nil, err
	}
	if err := w.registerSchemes(); err != nil {
		w.Destroy()
		return nil, err
	}
	w.windowSettings(opts.Debug)
	if w.ownsWindow {
		// Apply a default size (which also shows the window) unless the caller
		// sets one first, mirroring the macOS backend and webview's
		// dispatch_size_default. Without this, New()->Navigate()->Run() with no
		// SetSize would leave the window unrealized.
		dispatchMain(func() {
			if !w.isSizeSet {
				w.SetSize(defaultWidth, defaultHeight, HintNone)
			}
		})
	}
	return w, nil
}

func (w *webview) windowInit(window uintptr) error {
	if window != 0 {
		w.window = window
		w.ownsWindow = false
	} else {
		// gtk_init_check returns false (rather than aborting) when the windowing
		// system cannot be initialized, e.g. no display. Surface that as an error
		// from NewWindow instead of panicking.
		if !gtkInit() {
			return errors.New("webview: gtk_init_check failed (no display?)")
		}
		w.window = gtkNewWindow()
		gSignalConnectData(w.window, "destroy", windowDestroyFn, w.id, 0, 0)
	}

	w.webview = webkitWebViewNew()
	gObjectRefSink(w.webview)
	w.manager = webkitWebViewGetUserContentManager(w.webview)

	gSignalConnectData(w.manager, "script-message-received::__webview__",
		messageHandlerFn, w.id, 0, 0)
	registerScriptHandler(w.manager, "__webview__")

	w.pushUserScript(createInitScript(bridgePostFn))
	return nil
}

func (w *webview) windowSettings(debug bool) {
	settings := webkitWebViewGetSettings(w.webview)
	webkitSettingsSetJavascriptCanAccessClipboard(settings, true)
	if debug {
		webkitSettingsSetEnableWriteConsoleToStdout(settings, true)
		webkitSettingsSetEnableDeveloperExtras(settings, true)
	}
}

func (w *webview) onWindowDestroy() {
	// Closed via the OS: reclaim the engine registry entry so the webview is not
	// pinned when Destroy() is never called. unregisterEngine is idempotent, so a
	// later Destroy() is fine; signal callbacks resolve to nil and no-op.
	unregisterEngine(w.id)
	w.window = 0
	dispatchMain(func() { w.stopRunLoop = true })
}

func (w *webview) Run() {
	w.stopRunLoop = false
	for !w.stopRunLoop {
		gMainContextIteration(0, true)
	}
}

func (w *webview) Terminate() {
	dispatchMain(func() { w.stopRunLoop = true })
}

func (w *webview) Dispatch(f func()) { dispatchMain(f) }

func (w *webview) Window() unsafe.Pointer {
	p := w.window
	return *(*unsafe.Pointer)(unsafe.Pointer(&p))
}

func (w *webview) Destroy() {
	if w.window != 0 && w.ownsWindow {
		// g_signal_handlers_disconnect_by_data is a macro -> _disconnect_matched.
		gSignalHandlersDisconnectMatched(w.window, gSignalMatchData, 0, 0, 0, 0, w.id)
		gtkWindowClose(w.window)
		w.window = 0
	}
	if w.webview != 0 {
		// Disconnect the manager's script-message handler (matched by data = w.id)
		// before the manager is freed with the web view.
		if w.manager != 0 {
			gSignalHandlersDisconnectMatched(w.manager, gSignalMatchData, 0, 0, 0, 0, w.id)
			w.manager = 0
		}
		gObjectUnref(w.webview)
		w.webview = 0
	}
	unregisterEngine(w.id)
	if w.ownsWindow {
		done := false
		dispatchMain(func() { done = true })
		for i := 0; i < 10000 && !done; i++ {
			gMainContextIteration(0, true)
		}
	}
}

func (w *webview) SetTitle(title string) { gtkWindowSetTitle(w.window, title) }

func (w *webview) SetSize(width, height int, hint Hint) {
	gtkWindowSetResizable(w.window, hint != HintFixed)
	switch hint {
	case HintMin:
		gtkWidgetSetSizeRequest(w.window, width, height)
	case HintMax:
		if !gtk4 { // gtk_window_set_geometry_hints is GTK3/X11-only
			g := gdkGeometry{MaxWidth: int32(width), MaxHeight: int32(height)}
			gtkWindowSetGeometryHints(w.window, 0, &g, gdkHintMaxSize)
		}
	default: // HintNone, HintFixed
		if gtk4 {
			gtkWindowSetDefaultSize(w.window, width, height)
		} else {
			gtkWindowResize(w.window, width, height)
		}
	}
	w.isSizeSet = true
	w.windowShow()
}

func (w *webview) Navigate(url string) {
	if url == "" {
		url = "about:blank"
	}
	webkitWebViewLoadURI(w.webview, url)
}

func (w *webview) SetHtml(html string) {
	webkitWebViewLoadHTML(w.webview, html, 0)
}

func (w *webview) Init(js string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pushUserScript(js)
}

func (w *webview) Eval(js string) {
	if w.webview == 0 {
		return // web view destroyed (e.g. a late reply dispatched after Destroy).
	}
	if webkitWebViewGetURI(w.webview) == 0 {
		return // URI is null before content has begun loading.
	}
	if haveEvaluateJavascript {
		webkitWebViewEvaluateJavascript(w.webview, js, len(js), 0, 0, 0, 0, 0)
	} else {
		webkitWebViewRunJavascript(w.webview, js, 0, 0, 0)
	}
}

func (w *webview) windowShow() {
	if w.isWindowShown {
		return
	}
	if gtk4 {
		gtkWindowSetChild(w.window, w.webview)
		gtkWidgetSetVisible(w.webview, true)
	} else {
		gtkContainerAdd(w.window, w.webview)
		gtkWidgetShow(w.webview)
	}
	if w.ownsWindow {
		gtkWidgetGrabFocus(w.webview)
		if gtk4 {
			gtkWidgetSetVisible(w.window, true)
		} else {
			gtkWidgetShow(w.window)
		}
	}
	w.isWindowShown = true
}

func (w *webview) Focus() {
	if w.webview == 0 {
		return
	}
	// GtkWindow remembers its focus widget and restores it on re-activation, so
	// the first-show grab in windowShow already covers Alt-Tab; this is the
	// explicit, on-demand version.
	gtkWidgetGrabFocus(w.webview)
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
	w.rebuildScriptsLocked()
	w.mu.Unlock()
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
	w.rebuildScriptsLocked()
	w.mu.Unlock()
	w.Eval(fmt.Sprintf("if(window.__webview__){window.__webview__.onUnbind(%s)}", marshalJSON(name)))
	return nil
}

// --- user scripts + message routing ----------------------------------------

func (w *webview) pushUserScript(src string) {
	w.userScriptSrcs = append(w.userScriptSrcs, src)
	w.rebuildScriptsLocked()
}

func (w *webview) rebuildScriptsLocked() {
	if w.manager == 0 {
		return
	}
	webkitUserContentManagerRemoveAllScripts(w.manager)
	for _, src := range w.userScriptSrcs {
		addUserScript(w.manager, src)
	}
	addUserScript(w.manager, createBindScript(w.bindingNamesLocked()))
}

func (w *webview) bindingNamesLocked() []string {
	names := make([]string, 0, len(w.bindings))
	for n := range w.bindings {
		names = append(names, n)
	}
	return names
}

func addUserScript(manager uintptr, src string) {
	script := webkitUserScriptNew(src, injectTopFrame, injectAtDocumentStart, 0, 0)
	webkitUserContentManagerAddScript(manager, script)
	webkitUserScriptUnref(script)
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
	js := fmt.Sprintf("window.__webview__.onReply(%s, %d, %s)",
		marshalJSON(id), status, marshalJSON(resultJSON))
	dispatchMain(func() { w.Eval(js) })
}
