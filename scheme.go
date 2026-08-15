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
