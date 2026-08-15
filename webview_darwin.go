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

	nsEventTypeKeyDown            = 10
	nsEventTypeApplicationDefined = 15
	nsEventMaskAny                = ^uint(0)

	nsViewWidthSizable  = 1 << 1
	nsViewHeightSizable = 1 << 4

	nsModalResponseOK = 1

	// nsURLErrorFileDoesNotExist is Foundation's NSURLErrorFileDoesNotExist,
	// reported to a URL-scheme task when its handler has no resource to serve.
	nsURLErrorFileDoesNotExist = -1100

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
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
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
	schemeHandlerClass, firstMouseViewClass                                    objc.Class
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

	// A WKWebView that answers YES to acceptsFirstMouse:, used only when
	// Options.AcceptsFirstMouse asks for it. NSView's default is NO, so a click
	// on an inactive window is spent activating it and never reaches the page —
	// the "I had to click twice" complaint. Subclassing is the whole mechanism:
	// AppKit asks the VIEW under the cursor, and there is no window-level or
	// runtime switch for it.
	firstMouseViewClass, err = objc.RegisterClass(
		"GlazeFirstMouseWebView", objc.GetClass("WKWebView"), nil, nil,
		[]objc.MethodDef{{
			Cmd: sel("acceptsFirstMouse:"),
			Fn:  func(self objc.ID, _cmd objc.SEL, event objc.ID) bool { return true },
		}})
	if err != nil {
		return fmt.Errorf("webview: first-mouse web view class: %w", err)
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
			// WebKit requires a non-nil NSError here; passing nil can raise. A nil
			// response means "not found", so report NSURLErrorFileDoesNotExist.
			nsErr := class("NSError").Send(sel("errorWithDomain:code:userInfo:"),
				nsstr("NSURLErrorDomain"), nsURLErrorFileDoesNotExist, objc.ID(0))
			task.Send(sel("didFailWithError:"), nsErr)
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
		// alloc/init returns a +1 object; autorelease it so it does not leak once
		// per request (didReceiveResponse: retains what it needs).
		urlResp.Send(sel("autorelease"))
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

// onMainThread reports whether the caller runs on the process main thread —
// the only thread AppKit accepts UI work from.
func onMainThread() bool {
	return class("NSThread").Send(sel("isMainThread")) != 0
}

// uiIsMain records, at first webview creation, whether the UI runs on the
// process main thread. True in both supported shapes: creation on the main
// thread (the normal contract), and creation marshaled to the main thread
// because another owner's run loop is already there (native/tray). False only
// under the legacy single-goroutine shape — everything pinned to a secondary
// thread — where the main dispatch queue is never drained, so marshaling to
// it would hang; performOnMain then runs inline, preserving that shape's
// historical behavior.
var (
	uiIsMainOnce sync.Once
	uiIsMain     bool
)

// performOnMain runs f on the UI thread and waits for it, running inline when
// the caller is already there (or when the UI thread is not the main thread —
// see uiIsMain). Off the UI thread it queues onto the main dispatch queue and
// blocks until a running run loop drains it.
func performOnMain(f func()) {
	if onMainThread() || !uiIsMain {
		f()
		return
	}
	done := make(chan struct{})
	dispatchMain(func() {
		defer close(done)
		f()
	})
	<-done
}

// --- process-wide lifecycle bookkeeping ------------------------------------

var (
	firstMu      sync.Mutex
	notFirst     bool
	windowCount  int32
	uiThreadOnce sync.Once

	// glazeRunsLoop is true while OUR Run() drives [NSApp run]. When the loop
	// belongs to someone else (native/tray started it before the first webview
	// existed), it stays false: closing the last glaze window must not stop a
	// loop we do not own, and Terminate must not stop it either.
	glazeRunsLoop atomic.Bool
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
	// firstMouse makes a click on an INACTIVE window reach the page instead of
	// only bringing the window forward. See Options.AcceptsFirstMouse.
	firstMouse bool

	isSizeSet         bool
	isInitScriptAdded bool

	// closed is closed when this window goes away (user close or Destroy);
	// Run() waits on it instead of re-running NSApp when the run loop already
	// belongs to someone else. closeOnce makes the two close paths safe.
	closed    chan struct{}
	closeOnce sync.Once

	mu             sync.Mutex
	bindings       map[string]func(id, req string) (any, error)
	userScriptSrcs []string
	schemeHandlers map[string]SchemeHandler
	// schemeHandlerObjs are the WKURLSchemeHandler delegate objects (one per
	// scheme), kept so Destroy can drop their instance-registry entries — the
	// engine would otherwise stay pinned in the registry after Destroy.
	schemeHandlerObjs []objc.ID
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
// background goroutines. Exception: when the application run loop is already
// running (started by native/tray or another owner), NewWindow may be called
// from any goroutine — creation and the UI-touching methods marshal
// themselves to the main thread.
func NewWindow(debug bool, window unsafe.Pointer) (WebView, error) {
	return NewWithOptions(Options{Debug: debug, Window: window})
}

// NewWithOptions creates a web view configured by opts, including any custom
// SchemeHandlers (which must be installed before the WKWebView is created).
//
// When called off the main thread while the application run loop is already
// running (a native/tray app whose loop was started first — issue #31), the
// creation is marshaled to the main thread and every UI-touching method of
// the returned WebView marshals itself the same way, so the webview can be
// driven from that goroutine.
func NewWithOptions(opts Options) (WebView, error) {
	err := ensureInit()
	if err != nil {
		return nil, err
	}

	app := class("NSApplication").Send(sel("sharedApplication"))
	loopRunning := app.Send(sel("isRunning")) != 0
	uiIsMainOnce.Do(func() { uiIsMain = onMainThread() || loopRunning })

	if !onMainThread() && loopRunning {
		// Someone else's run loop is draining the main queue: build the whole
		// webview over there. Doing it here would run AppKit off the main
		// thread, and the old bootstrap path would hang in a second [NSApp
		// run] waiting for an applicationDidFinishLaunching that already fired.
		var w WebView
		performOnMain(func() { w = newWebView(opts, app, loopRunning) })
		return w, nil
	}

	uiThreadOnce.Do(runtime.LockOSThread)
	return newWebView(opts, app, loopRunning), nil
}

// newWebView builds the webview on the UI thread.
func newWebView(opts Options, app objc.ID, loopRunning bool) *webview {
	w := &webview{
		ownsWindow:     true,
		debug:          opts.Debug,
		firstMouse:     opts.AcceptsFirstMouse,
		bindings:       map[string]func(id, req string) (any, error){},
		schemeHandlers: opts.SchemeHandlers,
		closed:         make(chan struct{}),
	}
	w.app = app
	w.windowInit(objc.ID(uintptr(opts.Window)))
	w.windowSettings(opts.Debug)
	if loopRunning && w.ownsWindow {
		// The loop's owner picked the activation policy (a tray app runs as
		// Accessory, no Dock icon — leave that alone); activate so the new
		// window actually fronts instead of opening behind the current app.
		w.Raise()
	}
	if w.ownsWindow && w.isInitScriptAdded {
		dispatchMain(func() {
			if !w.isSizeSet {
				w.SetSize(defaultWidth, defaultHeight, HintNone)
			}
		})
	}
	return w
}

func (w *webview) windowInit(window objc.ID) {
	autorelease(func() {
		if window != 0 {
			w.window = window
			w.ownsWindow = false
			return
		}
		// The bootstrap below exists to finish launching the app: it installs
		// an app delegate and spins a temporary [NSApp run] that the delegate
		// stops from applicationDidFinishLaunching. If the run loop is already
		// running (native/tray started it), the app finished launching long
		// ago — that notification will never fire again and the temporary run
		// would block forever. Skip straight to window creation.
		if w.app.Send(sel("isRunning")) != 0 || !getAndSetIsFirstInstance() {
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
			// Autorelease the +1 from -new; the configuration retains it (matching
			// scriptHandler). Track it so Destroy can drop its registry entry.
			sh.Send(sel("autorelease"))
			registerInstance(sh, w)
			w.schemeHandlerObjs = append(w.schemeHandlerObjs, sh)
			config.Send(sel("setURLSchemeHandler:forURLScheme:"), sh, nsstr(scheme))
		}

		// The first-mouse variant is a WKWebView subclass, so everything below
		// treats it as one; only the class allocated differs.
		viewClass := objc.Class(class("WKWebView"))
		if w.firstMouse {
			viewClass = firstMouseViewClass
		}
		wv := objc.ID(viewClass).Send(sel("alloc"))
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
			// The content view is a plain NSView container, and a plain NSView
			// REFUSES first-responder status — so when the window becomes key,
			// AppKit's offer stops at the window itself and every keystroke is
			// an unhandled key: the system beep. A click fixed it only because
			// hit-testing hands the WKWebView the responder role. Hand it over
			// at birth instead, so a freshly opened window types.
			w.window.Send(sel("makeFirstResponder:"), w.webView)
		}
	})
}

func (w *webview) stopRunLoop() {
	autorelease(func() {
		w.app.Send(sel("stop:"), objc.ID(0))
		postWakeEvent(w.app)
	})
}

// postWakeEvent posts a no-op application-defined event so a thread blocked in
// nextEventMatchingMask wakes up (stop: alone only takes effect after an event).
func postWakeEvent(app objc.ID) {
	event := class("NSEvent").Send(
		sel("otherEventWithType:location:modifierFlags:timestamp:windowNumber:context:subtype:data1:data2:"),
		nsEventTypeApplicationDefined, cgPoint{0, 0}, uint(0), float64(0), 0, objc.ID(0), int16(0), 0, 0)
	app.Send(sel("postEvent:atStart:"), event, true)
}

func (w *webview) onWindowWillClose() {
	w.widget = 0
	w.webView = 0
	w.window = 0
	w.closeOnce.Do(func() { close(w.closed) })
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
	// Last owned window gone: stop the loop — but only when Run() drives it.
	// An external owner's loop (native/tray) outlives every glaze window.
	if decWindowCount() <= 0 && !skipTermination && glazeRunsLoop.Load() {
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

func (w *webview) Run() {
	if w.app.Send(sel("isRunning")) != 0 {
		// A run loop is already active — an external owner's (native/tray) or
		// our own driving another window. Re-running NSApp would fight it, so
		// Run means "until THIS window closes". Off the UI thread, waiting on
		// the channel is enough; on it (a Run inside a tray OnClick or another
		// run-loop callout), block-waiting would starve the loop that must
		// deliver the close, so pump events until the window goes away.
		if onMainThread() {
			w.pumpUntilClosed()
			return
		}
		<-w.closed
		return
	}
	glazeRunsLoop.Store(true)
	w.app.Send(sel("run"))
	glazeRunsLoop.Store(false)
}

// pumpUntilClosed services the event queue on the UI thread until this window
// closes — a nested, modal-style loop for a Run() issued from inside a run-loop
// callout (e.g. a tray menu handler).
func (w *webview) pumpUntilClosed() {
	for {
		select {
		case <-w.closed:
			return
		default:
		}
		autorelease(func() {
			// A short wait instead of distantFuture: the close can arrive from
			// a plain goroutine (Terminate), and a nested pump cannot count on
			// the main dispatch queue for a wake-up — when the pump itself runs
			// inside a main-queue callout, libdispatch will not drain that
			// queue again until the callout returns. Polling the channel every
			// 50ms is boring and deterministic.
			deadline := class("NSDate").Send(sel("dateWithTimeIntervalSinceNow:"), 0.05)
			ev := w.app.Send(sel("nextEventMatchingMask:untilDate:inMode:dequeue:"),
				nsEventMaskAny, deadline, nsstr("kCFRunLoopDefaultMode"), true)
			if ev != 0 {
				w.app.Send(sel("sendEvent:"), ev)
			}
		})
	}
}

// Terminate stops the run loop. Per the WebView contract it is safe to call from
// a background thread, so the AppKit calls in stopRunLoop are routed to the main
// thread (bindings run on goroutines), matching the Linux/Windows backends.
//
// When the run loop belongs to someone else (native/tray), stopping it would
// kill the owner's app; Terminate then only ends this webview's Run wait, and
// the caller's Destroy closes the window.
func (w *webview) Terminate() {
	if w.app.Send(sel("isRunning")) != 0 && !glazeRunsLoop.Load() {
		// Closing the channel is enough for both Run shapes: the channel wait
		// returns at once, and the pump polls it (see pumpUntilClosed for why
		// a queued wake-up could not be trusted here).
		w.closeOnce.Do(func() { close(w.closed) })
		return
	}
	dispatchMain(w.stopRunLoop)
}

func (w *webview) Dispatch(f func()) { dispatchMain(f) }

func (w *webview) Window() unsafe.Pointer {
	id := w.window
	return *(*unsafe.Pointer)(unsafe.Pointer(&id)) // #nosec G103 -- reinterpret the objc.ID's bits as the window pointer
}

func (w *webview) SetTitle(title string) {
	performOnMain(func() {
		autorelease(func() { w.window.Send(sel("setTitle:"), nsstr(title)) })
	})
}

func (w *webview) Focus() {
	if w.window == 0 || w.webView == 0 {
		return
	}
	// Largely redundant: an NSWindow makes its content view the first responder
	// when it becomes key, and restores it on re-activation. Kept as the explicit,
	// on-demand path and to mirror the other backends.
	performOnMain(func() {
		autorelease(func() { w.window.Send(sel("makeFirstResponder:"), w.webView) })
	})
}

func (w *webview) Raise() {
	if w.window == 0 {
		return
	}
	performOnMain(func() {
		autorelease(func() {
			// Both halves are needed and neither substitutes for the other:
			// activateIgnoringOtherApps brings the APPLICATION forward (without it
			// the window rises inside an app that is still in the background, and
			// the click that follows is still spent activating), and
			// makeKeyAndOrderFront brings THIS window forward within the app.
			w.app.Send(sel("activateIgnoringOtherApps:"), true)
			w.window.Send(sel("makeKeyAndOrderFront:"), objc.ID(0))
		})
	})
}

func (w *webview) SetSize(width, height int, hint Hint) {
	performOnMain(func() {
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
	})
	w.isSizeSet = true
}

func (w *webview) Navigate(url string) {
	if url == "" {
		url = "about:blank"
	}
	performOnMain(func() {
		autorelease(func() {
			nsurl := class("NSURL").Send(sel("URLWithString:"), nsstr(url))
			req := class("NSURLRequest").Send(sel("requestWithURL:"), nsurl)
			w.webView.Send(sel("loadRequest:"), req)
		})
	})
}

func (w *webview) SetHtml(html string) {
	performOnMain(func() {
		autorelease(func() {
			w.webView.Send(sel("loadHTMLString:baseURL:"), nsstr(html), objc.ID(0))
		})
	})
}

func (w *webview) Init(js string) {
	performOnMain(func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.pushUserScript(js)
	})
}

func (w *webview) Eval(js string) {
	if w.webView == 0 {
		return // web view destroyed (e.g. a late reply dispatched after Destroy).
	}
	// Unlike the Linux backend, there is no "URL is nil" guard here: SetHtml uses
	// loadHTMLString with a nil baseURL, which leaves WKWebView.URL nil, so such a
	// guard would block every Eval on SetHtml pages. Evaluating before load is
	// harmless on WKWebView (the completion handler, which we ignore, just errors).
	performOnMain(func() {
		autorelease(func() {
			w.webView.Send(sel("evaluateJavaScript:completionHandler:"), nsstr(js), objc.ID(0))
		})
	})
}

func (w *webview) Bind(name string, f any) error {
	wrapper, err := makeFuncWrapper(f)
	if err != nil {
		return err
	}
	// The script rebuild touches the WKUserContentController, so it runs on the
	// UI thread; taking mu inside the marshaled closure keeps every mu+AppKit
	// section on one thread (no lock held across a thread hop).
	var bindErr error
	performOnMain(func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		_, exists := w.bindings[name]
		if exists {
			bindErr = errors.New("function name already bound")
			return
		}
		w.bindings[name] = wrapper
		w.rebuildScriptsLocked()
	})
	if bindErr != nil {
		return bindErr
	}
	w.Eval(fmt.Sprintf("if(window.__webview__){window.__webview__.onBind(%s)}", marshalJSON(name)))
	return nil
}

func (w *webview) Unbind(name string) error {
	var unbindErr error
	performOnMain(func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		_, exists := w.bindings[name]
		if !exists {
			unbindErr = errors.New("function name not bound")
			return
		}
		delete(w.bindings, name)
		w.rebuildScriptsLocked()
	})
	if unbindErr != nil {
		return unbindErr
	}
	w.Eval(fmt.Sprintf("if(window.__webview__){window.__webview__.onUnbind(%s)}", marshalJSON(name)))
	return nil
}

// Destroy releases the web view and closes the native window, mirroring
// webview's cocoa destructor (release order matters for AppKit/WebKit).
func (w *webview) Destroy() {
	performOnMain(func() { w.destroyOnUI() })
}

func (w *webview) destroyOnUI() {
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
		// Scheme-handler delegates are owned by the (now-released) configuration;
		// like scriptHandler, only their registry entries need reclaiming.
		for _, sh := range w.schemeHandlerObjs {
			unregisterInstance(sh)
		}
		w.schemeHandlerObjs = nil
	})
	// Unblock a Run() waiting on this window (the pump polls the channel).
	w.closeOnce.Do(func() { close(w.closed) })
	if w.ownsWindow && !glazeRunsLoop.Load() && w.app.Send(sel("isRunning")) == 0 {
		// No run loop is active (the normal teardown, after Run returned):
		// flush the events queued during destruction ourselves. When a loop IS
		// running — ours or an external owner's — it drains them, and pumping
		// nested from inside one of its callouts is exactly the kind of
		// re-entrancy to avoid.
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
