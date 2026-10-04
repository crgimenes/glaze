package glaze

import "unsafe"

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

	// OnURLChange is called on the UI thread when the page on screen changes
	// its URL without loading a new document: history.pushState or
	// replaceState, a fragment link, back or forward between entries of the
	// same document. A load reports its URL through OnNavigation instead.
	// macOS and Linux; never called on Windows.
	OnURLChange func(url string)

	// OnNewWindow receives the URL of a page's request for a new window (a
	// target=_blank link, window.open). glaze never opens the window itself,
	// with or without a handler: the request is dropped and the page's
	// window.open returns null. Popups without a user gesture are blocked by
	// the engine before they get here. Called on the UI thread. macOS and
	// Linux; never called on Windows.
	OnNewWindow func(url string)

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

	// Ephemeral keeps the web view's website data -- cookies, local storage,
	// cache -- in memory only: it starts with nothing from earlier runs and
	// leaves nothing behind. Each ephemeral web view gets its own store.
	// macOS and Linux; ignored on Windows.
	Ephemeral bool
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
