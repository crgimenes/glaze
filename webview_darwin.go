// macOS WebView backend in pure Go via purego's Objective-C runtime.
//
// This reimplements webview's Cocoa/WKWebView backend directly against AppKit
// and WebKit, so glaze needs no cgo and no bundled libwebview.dylib on macOS.
// The exported API (New/NewWindow/Init + the WebView interface) matches the
// native-library backend used on the other platforms.

package glaze

import (
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

const (
	nsWindowStyleMaskTitled         = 1 << 0
	nsWindowStyleMaskClosable       = 1 << 1
	nsWindowStyleMaskMiniaturizable = 1 << 2
	nsWindowStyleMaskResizable      = 1 << 3

	nsBackingStoreBuffered = 2

	nsApplicationActivationPolicyRegular = 0

	nsEventTypeApplicationDefined = 15
	nsEventMaskAny                = ^uint(0)

	nsViewWidthSizable  = 1 << 1
	nsViewHeightSizable = 1 << 4

	nsModalResponseOK = 1

	wkInjectionTimeAtDocumentStart = 0

	defaultWidth  = 640
	defaultHeight = 480
)

// CGFloat is float64 on 64-bit; these mirror Cocoa geometry structs passed by
// value through objc_msgSend.
type cgPoint struct{ X, Y float64 }
type cgSize struct{ Width, Height float64 }
type cgRect struct {
	Origin cgPoint
	Size   cgSize
}

// --- objc helpers ----------------------------------------------------------

var selCache sync.Map // string -> objc.SEL

func sel(name string) objc.SEL {
	v, ok := selCache.Load(name)
	if ok {
		return v.(objc.SEL)
	}
	s := objc.RegisterName(name)
	selCache.Store(name, s)
	return s
}

func class(name string) objc.ID {
	c := objc.GetClass(name)
	if c == 0 {
		panic(fmt.Sprintf("glaze: objc class %q not found", name))
	}
	return objc.ID(c)
}

func nsstr(s string) objc.ID {
	return class("NSString").Send(sel("stringWithUTF8String:"), s)
}

// cstr reads a NUL-terminated C string returned as an objc.ID (e.g. -UTF8String).
func cstr(id objc.ID) string {
	if id == 0 {
		return ""
	}
	// Reinterpret the objc.ID's bits without a uintptr->Pointer cast (keeps go
	// vet's unsafeptr check quiet); the pointer is C string memory, not a Go
	// pointer.
	ptr := *(*unsafe.Pointer)(unsafe.Pointer(&id)) // #nosec G103
	var n int
	for *(*byte)(unsafe.Add(ptr, n)) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(ptr), n)) // #nosec G103 -- slice over the C string buffer
}

// autorelease wraps f in an NSAutoreleasePool, draining it afterward.
func autorelease(f func()) {
	pool := class("NSAutoreleasePool").Send(sel("alloc")).Send(sel("init"))
	defer pool.Send(sel("drain"))
	f()
}

// --- one-time runtime initialization ---------------------------------------

var (
	initOnce sync.Once
	initErr  error

	dispatchAsyncF func(queue, context, work uintptr)
	mainQueue      uintptr
	dispatchWork   uintptr

	appDelegateClass, scriptHandlerClass, windowDelegateClass, uiDelegateClass objc.Class
	schemeHandlerClass                                                         objc.Class
)

// Init prepares the macOS backend: loads AppKit + WebKit and registers the
// Objective-C delegate classes. Safe to call multiple times; New calls it.
func Init() error { return ensureInit() }

func ensureInit() error {
	initOnce.Do(func() {
		for _, fw := range []string{
			"/System/Library/Frameworks/Cocoa.framework/Cocoa",
			"/System/Library/Frameworks/WebKit.framework/WebKit",
		} {
			_, err := purego.Dlopen(fw, purego.RTLD_GLOBAL|purego.RTLD_LAZY)
			if err != nil {
				initErr = fmt.Errorf("webview: dlopen %s: %w", fw, err)
				return
			}
		}
		q, err := purego.Dlsym(purego.RTLD_DEFAULT, "_dispatch_main_q")
		if err != nil {
			initErr = fmt.Errorf("webview: resolve _dispatch_main_q: %w", err)
			return
		}
		mainQueue = q
		purego.RegisterLibFunc(&dispatchAsyncF, purego.RTLD_DEFAULT, "dispatch_async_f")
		dispatchWork = purego.NewCallback(func(ctx uintptr) uintptr {
			dispatchMu.Lock()
			f := dispatchMap[ctx]
			delete(dispatchMap, ctx)
			dispatchMu.Unlock()
			if f != nil {
				f()
			}
			return 0
		})
		initErr = registerClasses()
	})
	return initErr
}

func registerClasses() error {
	var err error
	appDelegateClass, err = objc.RegisterClass(
		"GlazeAppDelegate", objc.GetClass("NSResponder"),
		[]*objc.Protocol{objc.GetProtocol("NSTouchBarProvider")}, nil,
		[]objc.MethodDef{
			{
				Cmd: sel("applicationShouldTerminateAfterLastWindowClosed:"),
				Fn:  func(self objc.ID, _cmd objc.SEL, sender objc.ID) bool { return false },
			},
			{
				Cmd: sel("applicationDidFinishLaunching:"),
				Fn: func(self objc.ID, _cmd objc.SEL, notification objc.ID) {
					w := lookupEngine(self)
					if w != nil {
						w.onApplicationDidFinishLaunching(notification.Send(sel("object")))
					}
				},
			},
		})
	if err != nil {
		return fmt.Errorf("webview: app delegate class: %w", err)
	}

	scriptHandlerClass, err = objc.RegisterClass(
		"GlazeScriptMessageHandler", objc.GetClass("NSResponder"),
		[]*objc.Protocol{objc.GetProtocol("WKScriptMessageHandler")}, nil,
		[]objc.MethodDef{{
			Cmd: sel("userContentController:didReceiveScriptMessage:"),
			Fn: func(self objc.ID, _cmd objc.SEL, ucc objc.ID, message objc.ID) {
				w := lookupEngine(self)
				if w != nil {
					w.onMessage(cstr(message.Send(sel("body")).Send(sel("UTF8String"))))
				}
			},
		}})
	if err != nil {
		return fmt.Errorf("webview: script handler class: %w", err)
	}

	windowDelegateClass, err = objc.RegisterClass(
		"GlazeWindowDelegate", objc.GetClass("NSObject"),
		[]*objc.Protocol{objc.GetProtocol("NSWindowDelegate")}, nil,
		[]objc.MethodDef{{
			Cmd: sel("windowWillClose:"),
			Fn: func(self objc.ID, _cmd objc.SEL, notification objc.ID) {
				w := lookupEngine(self)
				if w != nil {
					w.onWindowWillClose()
				}
			},
		}})
	if err != nil {
		return fmt.Errorf("webview: window delegate class: %w", err)
	}

	uiDelegateClass, err = objc.RegisterClass(
		"GlazeUIDelegate", objc.GetClass("NSObject"),
		[]*objc.Protocol{objc.GetProtocol("WKUIDelegate")}, nil,
		[]objc.MethodDef{{
			Cmd: sel("webView:runOpenPanelWithParameters:initiatedByFrame:completionHandler:"),
			Fn:  runOpenPanel,
		}})
	if err != nil {
		return fmt.Errorf("webview: ui delegate class: %w", err)
	}

	schemeHandlerClass, err = objc.RegisterClass(
		"GlazeURLSchemeHandler", objc.GetClass("NSObject"),
		[]*objc.Protocol{objc.GetProtocol("WKURLSchemeHandler")}, nil,
		[]objc.MethodDef{
			{Cmd: sel("webView:startURLSchemeTask:"), Fn: startURLSchemeTask},
			{Cmd: sel("webView:stopURLSchemeTask:"), Fn: stopURLSchemeTask},
		})
	if err != nil {
		return fmt.Errorf("webview: url scheme handler class: %w", err)
	}
	return nil
}

// startURLSchemeTask implements -webView:startURLSchemeTask:. It resolves the
// owning webview via the scheme-handler object's registry entry, invokes the
// registered SchemeHandler, and feeds the bytes back through the task.
func startURLSchemeTask(self objc.ID, _cmd objc.SEL, webView objc.ID, task objc.ID) {
	w := lookupEngine(self)
	if w == nil {
		return
	}
	req := task.Send(sel("request"))
	nsurl := req.Send(sel("URL"))
	urlStr := cstr(nsurl.Send(sel("absoluteString")).Send(sel("UTF8String")))
	scheme := cstr(nsurl.Send(sel("scheme")).Send(sel("UTF8String")))

	resp := w.serveScheme(scheme, urlStr)
	autorelease(func() {
		if resp == nil {
			task.Send(sel("didFailWithError:"), objc.ID(0))
			return
		}
		body := resp.Body
		var dataPtr unsafe.Pointer
		if len(body) > 0 {
			dataPtr = unsafe.Pointer(&body[0]) // #nosec G103 -- dataWithBytes:length: copies the buffer
		}
		data := class("NSData").Send(sel("dataWithBytes:length:"), dataPtr, len(body))
		// A 200 NSHTTPURLResponse with a Content-Type header — an http-style
		// response is what makes WebKit treat the custom origin as secure.
		headers := class("NSMutableDictionary").Send(sel("dictionary"))
		headers.Send(sel("setObject:forKey:"), nsstr(schemeMIME(resp)), nsstr("Content-Type"))
		urlResp := class("NSHTTPURLResponse").Send(sel("alloc")).Send(
			sel("initWithURL:statusCode:HTTPVersion:headerFields:"),
			nsurl, 200, nsstr("HTTP/1.1"), headers)
		task.Send(sel("didReceiveResponse:"), urlResp)
		task.Send(sel("didReceiveData:"), data)
		task.Send(sel("didFinish"))
	})
}

// stopURLSchemeTask implements -webView:stopURLSchemeTask: — we complete
// synchronously, so there is nothing to cancel.
func stopURLSchemeTask(self objc.ID, _cmd objc.SEL, webView objc.ID, task objc.ID) {}

// runOpenPanel implements WKUIDelegate's file chooser via NSOpenPanel, invoking
// the completion handler block (driven through NSInvocation) with the URLs.
func runOpenPanel(self objc.ID, _cmd objc.SEL, webView, parameters, frame, completionHandler objc.ID) {
	autorelease(func() {
		allowsMultiple := parameters.Send(sel("allowsMultipleSelection")) != 0
		allowsDirs := parameters.Send(sel("allowsDirectories")) != 0

		panel := class("NSOpenPanel").Send(sel("openPanel"))
		configureOpenPanel(panel, true, allowsDirs, allowsMultiple, FileDialogOptions{})

		var urls objc.ID
		if int(panel.Send(sel("runModal"))) == nsModalResponseOK { // #nosec G115 -- NSModalResponse is a small int
			urls = panel.Send(sel("URLs"))
		}
		invokeOpenPanelCompletion(completionHandler, urls)
	})
}

// invokeOpenPanelCompletion calls the WKWebView open-panel completion block with
// the selected URLs (or nil when cancelled). The handler is an opaque block, so
// it is driven through NSInvocation with the signature "v@?@": index 0 is the
// block itself, index 1 the NSArray<NSURL*>* argument.
func invokeOpenPanelCompletion(completionHandler, urls objc.ID) {
	sig := class("NSMethodSignature").Send(sel("signatureWithObjCTypes:"), "v@?@")
	inv := class("NSInvocation").Send(sel("invocationWithMethodSignature:"), sig)
	inv.Send(sel("setTarget:"), completionHandler)
	inv.Send(sel("setArgument:atIndex:"), unsafe.Pointer(&urls), 1) // #nosec G103 -- pass the arg's address to NSInvocation
	inv.Send(sel("invoke"))
}

// --- instance registry (replaces objc associated objects) ------------------

var (
	regMu    sync.Mutex
	registry = map[objc.ID]*webview{}
)

func registerInstance(id objc.ID, w *webview) {
	regMu.Lock()
	registry[id] = w
	regMu.Unlock()
}

func unregisterInstance(id objc.ID) {
	regMu.Lock()
	delete(registry, id)
	regMu.Unlock()
}

func lookupEngine(id objc.ID) *webview {
	regMu.Lock()
	defer regMu.Unlock()
	return registry[id]
}

// --- libdispatch -----------------------------------------------------------

var (
	dispatchMu  sync.Mutex
	dispatchMap = map[uintptr]func(){}
	dispatchSeq uintptr
)

func dispatchMain(f func()) {
	dispatchMu.Lock()
	dispatchSeq++
	id := dispatchSeq
	dispatchMap[id] = f
	dispatchMu.Unlock()
	dispatchAsyncF(mainQueue, id, dispatchWork)
}

// --- process-wide lifecycle bookkeeping ------------------------------------

var (
	firstMu      sync.Mutex
	notFirst     bool
	windowCount  int32
	uiThreadOnce sync.Once
)

func getAndSetIsFirstInstance() bool {
	firstMu.Lock()
	defer firstMu.Unlock()
	if notFirst {
		return false
	}
	notFirst = true
	return true
}

func incWindowCount()       { atomic.AddInt32(&windowCount, 1) }
func decWindowCount() int32 { return atomic.AddInt32(&windowCount, -1) }

// --- webview ---------------------------------------------------------------

// webview is the macOS implementation of the WebView interface.
type webview struct {
	app            objc.ID
	appDelegate    objc.ID
	windowDelegate objc.ID
	uiDelegate     objc.ID
	window         objc.ID
	widget         objc.ID
	webView        objc.ID
	manager        objc.ID
	scriptHandler  objc.ID

	ownsWindow bool
	debug      bool

	isSizeSet         bool
	isInitScriptAdded bool

	mu             sync.Mutex
	bindings       map[string]func(id, req string) (any, error)
	userScriptSrcs []string
	schemeHandlers map[string]SchemeHandler
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

// New creates a new window and a web view.
func New(debug bool) (WebView, error) { return NewWindow(debug, nil) }

// NewWindow creates a web view. If window is non-nil it must point to an
// existing NSWindow to embed into; otherwise a new window is created and owned.
//
// The first successful call pins the calling goroutine to its OS thread; keep
// all direct UI calls on that goroutine and re-enter through Dispatch from
// background goroutines.
func NewWindow(debug bool, window unsafe.Pointer) (WebView, error) {
	return NewWithOptions(Options{Debug: debug, Window: window})
}

// NewWithOptions creates a web view configured by opts, including any custom
// SchemeHandlers (which must be installed before the WKWebView is created).
func NewWithOptions(opts Options) (WebView, error) {
	err := ensureInit()
	if err != nil {
		return nil, err
	}
	uiThreadOnce.Do(runtime.LockOSThread)

	w := &webview{
		ownsWindow:     true,
		debug:          opts.Debug,
		bindings:       map[string]func(id, req string) (any, error){},
		schemeHandlers: opts.SchemeHandlers,
	}
	w.app = class("NSApplication").Send(sel("sharedApplication"))
	w.windowInit(objc.ID(uintptr(opts.Window)))
	w.windowSettings(opts.Debug)
	if w.ownsWindow && w.isInitScriptAdded {
		dispatchMain(func() {
			if !w.isSizeSet {
				w.SetSize(defaultWidth, defaultHeight, HintNone)
			}
		})
	}
	return w, nil
}

func (w *webview) windowInit(window objc.ID) {
	autorelease(func() {
		if window != 0 {
			w.window = window
			w.ownsWindow = false
			return
		}
		if !getAndSetIsFirstInstance() {
			w.windowInitProceed()
			return
		}
		w.appDelegate = objc.ID(appDelegateClass).Send(sel("new"))
		registerInstance(w.appDelegate, w)
		w.app.Send(sel("setDelegate:"), w.appDelegate)
		// Temporary run loop: returns once applicationDidFinishLaunching stops it.
		w.app.Send(sel("run"))
	})
}

func (w *webview) onApplicationDidFinishLaunching(app objc.ID) {
	if w.ownsWindow {
		w.stopRunLoop()
	}
	if !isAppBundled() {
		app.Send(sel("setActivationPolicy:"), nsApplicationActivationPolicyRegular)
		app.Send(sel("activateIgnoringOtherApps:"), true)
	}
	w.windowInitProceed()
}

func (w *webview) windowInitProceed() {
	autorelease(func() {
		win := class("NSWindow").Send(sel("alloc"))
		win = win.Send(sel("initWithContentRect:styleMask:backing:defer:"),
			cgRect{cgPoint{0, 0}, cgSize{defaultWidth, defaultHeight}},
			uint(nsWindowStyleMaskTitled), nsBackingStoreBuffered, false)
		w.window = win.Send(sel("retain"))
		w.windowDelegate = objc.ID(windowDelegateClass).Send(sel("new"))
		registerInstance(w.windowDelegate, w)
		w.window.Send(sel("setDelegate:"), w.windowDelegate)
		incWindowCount()
	})
}

func (w *webview) windowSettings(debug bool) {
	autorelease(func() {
		rect := cgRect{cgPoint{0, 0}, cgSize{defaultWidth, defaultHeight}}

		config := class("WKWebViewConfiguration").Send(sel("new"))
		config.Send(sel("autorelease"))
		w.manager = config.Send(sel("userContentController"))

		prefs := config.Send(sel("preferences"))
		yes := class("NSNumber").Send(sel("numberWithBool:"), true)
		if debug {
			prefs.Send(sel("setValue:forKey:"), yes, nsstr("developerExtrasEnabled"))
		}
		prefs.Send(sel("setValue:forKey:"), yes, nsstr("fullScreenEnabled"))

		// Register custom scheme handlers on the configuration BEFORE the
		// WKWebView is created — WKWebView copies its configuration at init, so
		// this cannot be done afterward. One handler object per scheme; each is
		// mapped back to this webview via the instance registry.
		for scheme := range w.schemeHandlers {
			sh := objc.ID(schemeHandlerClass).Send(sel("new"))
			registerInstance(sh, w)
			config.Send(sel("setURLSchemeHandler:forURLScheme:"), sh, nsstr(scheme))
		}

		wv := class("WKWebView").Send(sel("alloc"))
		wv = wv.Send(sel("initWithFrame:configuration:"), rect, config)
		w.webView = wv.Send(sel("retain"))
		w.webView.Send(sel("setAutoresizingMask:"), uint(nsViewWidthSizable|nsViewHeightSizable))
		if debug {
			w.webView.Send(sel("setInspectable:"), true)
		}

		// UIDelegate is a weak reference; keep our own strong ref in w.uiDelegate.
		w.uiDelegate = objc.ID(uiDelegateClass).Send(sel("new"))
		w.webView.Send(sel("setUIDelegate:"), w.uiDelegate)

		handler := objc.ID(scriptHandlerClass).Send(sel("new"))
		registerInstance(handler, w)
		handler.Send(sel("autorelease"))
		w.scriptHandler = handler // kept so Destroy can drop its registry entry
		w.manager.Send(sel("addScriptMessageHandler:name:"), handler, nsstr("__webview__"))

		w.pushUserScript(createInitScript(bridgePostFn))
		w.isInitScriptAdded = true

		widget := class("NSView").Send(sel("alloc")).Send(sel("initWithFrame:"), rect)
		w.widget = widget.Send(sel("retain"))
		w.widget.Send(sel("setAutoresizesSubviews:"), true)
		w.widget.Send(sel("addSubview:"), w.webView)

		w.window.Send(sel("setContentView:"), w.widget)
		if w.ownsWindow {
			w.window.Send(sel("makeKeyAndOrderFront:"), objc.ID(0))
		}
	})
}

func (w *webview) stopRunLoop() {
	autorelease(func() {
		w.app.Send(sel("stop:"), objc.ID(0))
		event := class("NSEvent").Send(
			sel("otherEventWithType:location:modifierFlags:timestamp:windowNumber:context:subtype:data1:data2:"),
			nsEventTypeApplicationDefined, cgPoint{0, 0}, uint(0), float64(0), 0, objc.ID(0), int16(0), 0, 0)
		w.app.Send(sel("postEvent:atStart:"), event, true)
	})
}

func (w *webview) onWindowWillClose() {
	w.widget = 0
	w.webView = 0
	w.window = 0
	dispatchMain(func() { w.onWindowDestroyed(false) })
}

func (w *webview) onWindowDestroyed(skipTermination bool) {
	if !skipTermination && w.windowDelegate != 0 {
		// Closed via the OS, not Destroy(): drop the delegate->engine mapping so
		// the webview is not pinned in the registry when Destroy() is never
		// called. The objc object is still released by a later Destroy() if any
		// (the map delete is idempotent); a stray delegate callback resolves to
		// nil and no-ops.
		unregisterInstance(w.windowDelegate)
	}
	if decWindowCount() <= 0 && !skipTermination {
		w.Terminate()
	}
}

func isAppBundled() bool {
	bundle := class("NSBundle").Send(sel("mainBundle"))
	if bundle == 0 {
		return false
	}
	path := bundle.Send(sel("bundlePath"))
	return path.Send(sel("hasSuffix:"), nsstr(".app")) != 0
}

// --- public API (WebView interface) ----------------------------------------

func (w *webview) Run() { w.app.Send(sel("run")) }

// Terminate stops the run loop. Per the WebView contract it is safe to call from
// a background thread, so the AppKit calls in stopRunLoop are routed to the main
// thread (bindings run on goroutines), matching the Linux/Windows backends.
func (w *webview) Terminate() { dispatchMain(w.stopRunLoop) }

func (w *webview) Dispatch(f func()) { dispatchMain(f) }

func (w *webview) Window() unsafe.Pointer {
	id := w.window
	return *(*unsafe.Pointer)(unsafe.Pointer(&id)) // #nosec G103 -- reinterpret the objc.ID's bits as the window pointer
}

func (w *webview) SetTitle(title string) {
	autorelease(func() { w.window.Send(sel("setTitle:"), nsstr(title)) })
}

func (w *webview) Focus() {
	if w.window == 0 || w.webView == 0 {
		return
	}
	// Largely redundant: an NSWindow makes its content view the first responder
	// when it becomes key, and restores it on re-activation. Kept as the explicit,
	// on-demand path and to mirror the other backends.
	autorelease(func() { w.window.Send(sel("makeFirstResponder:"), w.webView) })
}

func (w *webview) SetSize(width, height int, hint Hint) {
	autorelease(func() {
		style := uint(nsWindowStyleMaskTitled | nsWindowStyleMaskClosable | nsWindowStyleMaskMiniaturizable)
		if hint != HintFixed {
			style |= nsWindowStyleMaskResizable
		}
		w.window.Send(sel("setStyleMask:"), style)
		size := cgSize{float64(width), float64(height)}
		switch hint {
		case HintMin:
			w.window.Send(sel("setContentMinSize:"), size)
		case HintMax:
			w.window.Send(sel("setContentMaxSize:"), size)
		default:
			// setContentSize keeps the top-left corner fixed, avoiding a
			// struct-return read of the current frame.
			w.window.Send(sel("setContentSize:"), size)
		}
		w.window.Send(sel("center"))
	})
	w.isSizeSet = true
}

func (w *webview) Navigate(url string) {
	if url == "" {
		url = "about:blank"
	}
	autorelease(func() {
		nsurl := class("NSURL").Send(sel("URLWithString:"), nsstr(url))
		req := class("NSURLRequest").Send(sel("requestWithURL:"), nsurl)
		w.webView.Send(sel("loadRequest:"), req)
	})
}

func (w *webview) SetHtml(html string) {
	autorelease(func() {
		w.webView.Send(sel("loadHTMLString:baseURL:"), nsstr(html), objc.ID(0))
	})
}

func (w *webview) Init(js string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pushUserScript(js)
}

func (w *webview) Eval(js string) {
	if w.webView == 0 {
		return // web view destroyed (e.g. a late reply dispatched after Destroy).
	}
	// Unlike the Linux backend, there is no "URL is nil" guard here: SetHtml uses
	// loadHTMLString with a nil baseURL, which leaves WKWebView.URL nil, so such a
	// guard would block every Eval on SetHtml pages. Evaluating before load is
	// harmless on WKWebView (the completion handler, which we ignore, just errors).
	autorelease(func() {
		w.webView.Send(sel("evaluateJavaScript:completionHandler:"), nsstr(js), objc.ID(0))
	})
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

// Destroy releases the web view and closes the native window, mirroring
// webview's cocoa destructor (release order matters for AppKit/WebKit).
func (w *webview) Destroy() {
	autorelease(func() {
		if w.window != 0 {
			if w.webView != 0 {
				if w.uiDelegate != 0 {
					w.webView.Send(sel("setUIDelegate:"), objc.ID(0))
					w.uiDelegate.Send(sel("release"))
					w.uiDelegate = 0
				}
				w.webView.Send(sel("release"))
				w.webView = 0
			}
			if w.widget != 0 {
				if w.widget == w.window.Send(sel("contentView")) {
					w.window.Send(sel("setContentView:"), objc.ID(0))
				}
				w.widget.Send(sel("release"))
				w.widget = 0
			}
			if w.ownsWindow {
				w.window.Send(sel("setDelegate:"), objc.ID(0))
				w.window.Send(sel("close"))
				w.onWindowDestroyed(true)
			}
			w.window = 0
		}
		if w.windowDelegate != 0 {
			unregisterInstance(w.windowDelegate)
			w.windowDelegate.Send(sel("release"))
			w.windowDelegate = 0
		}
		if w.appDelegate != 0 {
			w.app.Send(sel("setDelegate:"), objc.ID(0))
			unregisterInstance(w.appDelegate)
			w.appDelegate.Send(sel("release"))
			w.appDelegate = 0
		}
		if w.scriptHandler != 0 {
			// The handler object is owned by the (now-released) content manager;
			// only its registry entry needs reclaiming (a map delete).
			unregisterInstance(w.scriptHandler)
			w.scriptHandler = 0
		}
	})
	if w.ownsWindow {
		w.depleteRunLoopEventQueue()
	}
}

// runEventLoopWhile pumps queued AppKit events while cond holds, bounded so it
// can never hang even when the application run loop is not active.
func (w *webview) runEventLoopWhile(cond func() bool) {
	for i := 0; i < 10000 && cond(); i++ {
		autorelease(func() {
			ev := w.app.Send(sel("nextEventMatchingMask:untilDate:inMode:dequeue:"),
				nsEventMaskAny, objc.ID(0), nsstr("kCFRunLoopDefaultMode"), true)
			if ev != 0 {
				w.app.Send(sel("sendEvent:"), ev)
			}
		})
	}
}

// depleteRunLoopEventQueue runs the event loop until the currently queued
// events have been processed.
func (w *webview) depleteRunLoopEventQueue() {
	var done atomic.Bool
	dispatchMain(func() { done.Store(true) })
	w.runEventLoopWhile(func() bool { return !done.Load() })
}

// --- user scripts + message routing ----------------------------------------

func (w *webview) pushUserScript(src string) {
	w.userScriptSrcs = append(w.userScriptSrcs, src)
	w.rebuildScriptsLocked()
}

// rebuildScriptsLocked re-injects the bridge, Init() scripts and the current
// bind script in order. Assumes w.mu is held (or single-threaded setup).
func (w *webview) rebuildScriptsLocked() {
	if w.manager == 0 {
		return
	}
	autorelease(func() {
		w.manager.Send(sel("removeAllUserScripts"))
		for _, src := range w.userScriptSrcs {
			addWKUserScript(w.manager, src)
		}
		addWKUserScript(w.manager, createBindScript(w.bindingNamesLocked()))
	})
}

func (w *webview) bindingNamesLocked() []string {
	names := make([]string, 0, len(w.bindings))
	for n := range w.bindings {
		names = append(names, n)
	}
	return names
}

func addWKUserScript(manager objc.ID, src string) {
	s := class("WKUserScript").Send(sel("alloc"))
	s = s.Send(sel("initWithSource:injectionTime:forMainFrameOnly:"),
		nsstr(src), wkInjectionTimeAtDocumentStart, true)
	manager.Send(sel("addUserScript:"), s)
	s.Send(sel("release"))
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
	dispatchMain(func() { autorelease(func() { w.Eval(js) }) })
}
