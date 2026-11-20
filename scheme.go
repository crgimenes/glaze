package glaze

import (
	"errors"
	"unsafe"
)

// This file adds a custom-URL-scheme handler API to glaze so a host app can serve
// its assets from a portless, custom origin (e.g. "app://") that WebKit/WebView2
// still treat as a SECURE CONTEXT — the property a loopback http://127.0.0.1
// server provides today. Registering a scheme handler must happen before the web
// view is created (on macOS the WKWebViewConfiguration is copied at init), so it
// is an Option passed to NewWithOptions, not a method on the running WebView.

// SchemeRequest describes an incoming request for a registered custom scheme.
type SchemeRequest struct {
	// URL is the full request URL, e.g. "app://host/index.html".
	URL string
}

// SchemeResponse is what a SchemeHandler returns for a request. A nil response
// is treated as "not found".
type SchemeResponse struct {
	// Body is the response payload. The backend copies or streams it before the
	// handler returns, so it need not outlive the call.
	Body []byte
	// MIMEType defaults to "application/octet-stream" when empty.
	MIMEType string
}

// SchemeHandler serves responses for one registered scheme. It runs on the UI
// thread, so keep it fast (serve from an in-memory FS).
type SchemeHandler func(*SchemeRequest) *SchemeResponse

// Options configures a web view created with NewWithOptions.
type Options struct {
	// Debug enables the platform web inspector / dev tools.
	Debug bool
	// Window, if non-nil, is an existing native window to embed into (a
	// GtkWindow* / NSWindow* / HWND), mirroring NewWindow.
	Window unsafe.Pointer
	// SchemeHandlers maps a scheme name (without "://", e.g. "app") to its
	// handler, registered as a secure context. Handlers must be installed before
	// the web view is created, so they cannot be added later.
	SchemeHandlers map[string]SchemeHandler

	// AcceptsFirstMouse makes a click on an INACTIVE window reach the page
	// instead of only bringing the window forward.
	//
	// macOS only; ignored elsewhere, where a click on an inactive window already
	// reaches the content. AppKit's default is the opposite of what most web UIs
	// want: the first click is swallowed as activation, so a user who clicks a
	// button in a window that lost focus has to click twice — and the first
	// click looks broken. Turn this on for control panels, dashboards, players
	// and anything else the user clicks in passing.
	//
	// It is OPT-IN because the default protects destructive interfaces: in a
	// drawing tool, an editor, or any window with a delete button, a click that
	// merely raises the window must NOT also press what happens to be under the
	// cursor. Leave it off when a stray first click could destroy something.
	AcceptsFirstMouse bool

	// NoBridge leaves out the JS<->Go bridge: no window.__webview__ and no
	// native message handler, so the page has no channel back to the process.
	// Bind and Unbind then return ErrBridgeDisabled; Init and Eval still work.
	// Use it when the web view loads content the app does not control: on
	// Linux it also denies the page script access to the clipboard (macOS
	// denies it by default).
	NoBridge bool

	// HideUntilLoaded keeps the web view hidden until the first page can be
	// drawn, so the window shows its native background instead of flashing an
	// empty white page. On Linux that is when the navigation commits: WebKitGTK
	// paints nothing until the page does, so the page shows as it paints. On
	// macOS it is the page's first contentful paint, because WKWebView paints
	// white between the commit and then; without that signal (before macOS
	// 11), when the navigation ends. Clicks and keys wait for the page to
	// show. Ignored on Windows.
	HideUntilLoaded bool

	// OnNavigation is called on the UI thread when a main-frame navigation
	// finishes or fails. macOS and Linux; never called on Windows.
	OnNavigation func(NavigationEvent)

	// OnNavigationStart is called on the UI thread when a main-frame
	// navigation starts, with the URL requested; a redirect is not a new
	// start. The navigation then ends in OnNavigation, unless it is cancelled
	// (replaced by another, stopped, or turned into a download), which is not
	// reported. macOS and Linux; never called on Windows.
	OnNavigationStart func(url string)

	// OnURLChange is called on the UI thread with the URL of the page on
	// screen each time it changes: when a navigation commits a new document
	// (after any redirect, long before the load finishes), and when the page
	// moves its URL without loading -- history.pushState or replaceState, a
	// fragment link, back or forward within the same document. A navigation
	// that fails or is cancelled before committing leaves the page, and its
	// URL, as they were. macOS and Linux; never called on Windows.
	OnURLChange func(url string)

	// OnScriptDialog shows a page's alert(), confirm() or prompt() and returns
	// the answer: ok for OK (confirm, prompt) and the text typed (prompt). The
	// page waits for it. Without it the engines differ, as they always have:
	// macOS answers as if dismissed (the alert vanishes, confirm is false,
	// prompt null) and Linux shows WebKitGTK's own dialog. With it, a page
	// leaving with an onbeforeunload handler on Linux just leaves, as on macOS.
	// Called on the UI thread; may run a modal dialog. macOS and Linux; never
	// called on Windows.
	OnScriptDialog func(d ScriptDialog) (ok bool, text string)

	// OnContentShown is called on the UI thread when HideUntilLoaded shows
	// the web view (see there for when that is). Only with HideUntilLoaded.
	// macOS and Linux; never called on Windows.
	OnContentShown func()

	// OnNewWindow receives the URL of a page's request for a new window (a
	// target=_blank link, window.open). glaze never opens the window itself,
	// with or without a handler: the request is dropped and the page's
	// window.open returns null. Popups without a user gesture are blocked by
	// the engine before they get here. Called on the UI thread. macOS and
	// Linux; never called on Windows.
	OnNewWindow func(url string)

	// OnClose is called on the UI thread when the person closes the window
	// (its close button, Alt+F4, Cmd-W), once the window is gone; never after
	// Destroy. With several windows it is how the app learns that one of
	// them closed: Run returns for the last window only on Windows, and for
	// the window it was called on only on Linux. Call Destroy afterwards to
	// release the web view.
	OnClose func()

	// OnDownload decides where a download the page starts (an attachment, a
	// file the view cannot show, a link with the download attribute) is
	// saved: it gets the suggested file name and returns the destination
	// path, or "" to cancel. An existing file there is replaced -- the app is
	// expected to have asked (a save panel does). Without OnDownload every
	// download is cancelled; nothing is ever saved on the engine's own
	// initiative. Called on the UI thread; may run a modal dialog. macOS and
	// Linux; downloads are left to the engine on Windows.
	OnDownload func(suggestedName string) (path string)

	// OnDownloadDone reports a download OnDownload accepted: saved at path,
	// or failed with err.
	OnDownloadDone func(path string, err error)

	// OnOpenURLs receives the URLs the system asks the app to open: a link
	// clicked in another app while this one is the chosen handler for its
	// scheme (the app declares the scheme in its Info.plist). When the
	// system launches the app to open a URL, the call comes while
	// NewWithOptions is still starting the application, before it returns.
	// It is an application event, not a window's: the handler of the last
	// NewWithOptions that set one gets every URL. It reaches glaze only when
	// glaze started the application (not under another run loop's owner).
	// Called on the UI thread. macOS only: elsewhere the URL comes as a
	// command-line argument.
	OnOpenURLs func(urls []string)

	// OnMediaCapture decides whether the page from origin (scheme://host[:port])
	// may use the camera, the microphone or both. origin is always the
	// top-level page's: a frame from another origin reaches here only when
	// that page delegates the device to it (<iframe allow="camera">), and the
	// engine denies it otherwise, so the page answers for its frames, as in
	// the major browsers' permission delegation. Without it every request is
	// denied, as are other permissions (geolocation, notifications). A grant
	// also lets that page learn the devices' names, until it navigates away. On Linux, setting it also
	// turns on media streams and WebRTC, which WebKitGTK leaves off. macOS
	// asks the user once more on its own and needs NSCameraUsageDescription
	// and NSMicrophoneUsageDescription in the app's Info.plist (and, when
	// sandboxed, the device.camera and device.audio-input entitlements), or
	// the system ends the app. Called on the UI thread; may run a modal
	// dialog. macOS 12+ and Linux; never called on Windows.
	OnMediaCapture func(origin string, camera, microphone bool) bool

	// SandboxWebContent runs page content in WebKitGTK's bubblewrap sandbox
	// on Linux with WebKitGTK 4.1 (GTK3), where it is off unless asked for:
	// for an app that shows pages it does not control. It needs bwrap
	// installed, and stays off without it, as WebKit could not start the
	// page's process otherwise. WebKitGTK 6.0 (GTK4) always sandboxes, and so
	// does macOS; ignored there and on Windows. The first web view decides for
	// the process's shared context.
	SandboxWebContent bool

	// NavigationGestures turns on the trackpad's swipe between pages: two
	// fingers sideways go back and forward, as in Safari. Off by default: in
	// an app with routes of its own a swipe would move between them. macOS and
	// Linux (WebKitGTK 2.24 and later); ignored on Windows.
	NavigationGestures bool

	// FrameAutosaveName, on macOS, makes the window remember its size and
	// position under this name: the first SetSize gives the size only when
	// nothing is saved yet. A window opened while other instances of the same
	// app are running is moved down and right of where it would go, so it does
	// not hide theirs, and leaves the saved frame alone. Ignored elsewhere.
	FrameAutosaveName string

	// Ephemeral keeps the web view's website data -- cookies, local storage,
	// cache -- in memory only: it starts with nothing from earlier runs and
	// leaves nothing behind. Each ephemeral web view gets its own store.
	// macOS and Linux; ignored on Windows.
	Ephemeral bool

	// Hidden makes a headless web view: it keeps its native window and event
	// loop, but the window is never shown -- not at creation, while loading,
	// on SetSize, or in Run -- and the app is not activated for it. Focus and
	// Raise do nothing. SetSize still sizes the page's viewport (on Linux,
	// only with DisableThrottling; see below). Content loads and runs
	// normally, so HideUntilLoaded is ignored. It cannot be combined with
	// Window: an existing window's visibility belongs to its owner. On macOS a
	// process whose only web views are hidden runs without a Dock icon.
	// WebKit throttles a page whose window is not on screen, on macOS and
	// Linux alike; see DisableThrottling. On Windows a hidden page is not
	// throttled: it reports itself visible and runs at full speed, animation
	// frames included, with or without DisableThrottling. Linux still needs a
	// display session (X11 or Wayland; Xvfb or a headless compositor such as
	// weston --backend=headless will do), and without DisableThrottling a
	// hidden page there sees a 0x0 viewport, since its window is never mapped.
	Hidden bool

	// DisableThrottling keeps the page running at full speed while the
	// window is inactive, covered or hidden. For pages doing continuous work
	// in the background; it costs battery.
	//
	// On macOS it opts the process out of App Nap (for the rest of its life,
	// once any web view asks) and turns off WebKit's occlusion detection,
	// hidden-page timer throttling and hidden-page process suppression. The
	// WebKit settings are private API, each applied only if this macOS has
	// it. With Hidden, the window is put on screen fully transparent and
	// click-through instead of left off it.
	//
	// On Linux it is complete only with Hidden. It turns off WebKitGTK's
	// hidden-page timer throttling and CSS animation suspension (WebKitGTK
	// 2.42 or later), which alone keeps timers at full speed but not
	// requestAnimationFrame. With Hidden, the window is also mapped without
	// appearing on screen, so the page is visible to WebKit: an offscreen
	// window on GTK3, which the first such web view in a process therefore
	// prefers when both stacks are installed, and otherwise (GTK4, or
	// WebKitGTK 2.48.0 to 2.48.3, which crash in an offscreen window) a fully
	// transparent, undecorated, click-through window that is never presented
	// or focused. On GTK4 that window can still take focus and appear in the
	// task switcher on some desktops; under Xvfb or a headless compositor
	// neither matters. Without Hidden it is best effort: a minimized or
	// covered window keeps its timers, but not animation frames.
	//
	// On Windows only a minimized window is throttled, because its web view is
	// sized to nothing. With DisableThrottling the web view keeps its last
	// size instead, so the page keeps running at full speed and reports
	// itself visible. A Hidden web view is never throttled.
	DisableThrottling bool
}

// ErrHiddenWindow is returned by NewWithOptions when Options.Hidden is
// combined with Options.Window.
var ErrHiddenWindow = errors.New("webview: Options.Hidden cannot be combined with Options.Window")

// checkHidden validates Options.Hidden and clears what it overrides.
func checkHidden(opts *Options) error {
	if !opts.Hidden {
		return nil
	}
	if opts.Window != nil {
		return ErrHiddenWindow
	}
	opts.HideUntilLoaded = false
	return nil
}

// schemeMIME returns a response's MIME type or the octet-stream default.
func schemeMIME(r *SchemeResponse) string {
	if r.MIMEType != "" {
		return r.MIMEType
	}
	return "application/octet-stream"
}

// callSchemeHandler invokes h with panic containment: handlers run inside
// native UI callbacks, where an unwound panic kills the process. A panicking
// handler answers nil — the platform's "not found" — the same way a panicking
// binding answers status -1 (see callAndMarshal).
func callSchemeHandler(h SchemeHandler, req *SchemeRequest) (resp *SchemeResponse) {
	defer func() {
		if recover() != nil {
			resp = nil
		}
	}()
	return h(req)
}
