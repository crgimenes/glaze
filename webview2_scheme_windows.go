// Custom-scheme serving for the WebView2 backend.
//
// WebView2 has no per-scheme secure-context flag the way WKWebView/WebKitGTK do,
// so a registered scheme is served over a per-scheme https virtual host
// (https://<scheme>.localhost) — an https origin is a secure context, and the
// request never leaves the process: AddWebResourceRequestedFilter + the
// WebResourceRequested event answer it from the SchemeHandler in memory. Navigate
// rewrites "<scheme>://.../path" to that vhost so callers use one scheme URL on
// every platform.
package glaze

import (
	"net/url"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/ebitengine/purego"
)

// schemeVHost is the secure https origin a custom scheme is served from.
func schemeVHost(scheme string) string { return "https://" + scheme + ".localhost" }

// --- COM wrappers for the WebResourceRequested event -----------------------

type iWRRArgsVtbl struct {
	iUnknownVtbl
	GetRequest  uintptr
	GetResponse uintptr
	PutResponse uintptr
}
type iWRRArgs struct{ vtbl *iWRRArgsVtbl }

func asWRRArgs(p uintptr) *iWRRArgs { return (*iWRRArgs)(ptr(p)) }

func (i *iWRRArgs) GetRequest(out *uintptr) uintptr {
	r, _, _ := purego.SyscallN(i.vtbl.GetRequest, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(out)))
	return r
}
func (i *iWRRArgs) PutResponse(resp uintptr) uintptr {
	r, _, _ := purego.SyscallN(i.vtbl.PutResponse, uintptr(unsafe.Pointer(i)), resp)
	return r
}

type iWRRequestVtbl struct {
	iUnknownVtbl
	GetUri uintptr
}
type iWRRequest struct{ vtbl *iWRRequestVtbl }

func asWRRequest(p uintptr) *iWRRequest { return (*iWRRequest)(ptr(p)) }

func (i *iWRRequest) GetUri(out *uintptr) uintptr {
	r, _, _ := purego.SyscallN(i.vtbl.GetUri, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(out)))
	return r
}

func (i *iEnvironment) CreateWebResourceResponse(stream uintptr, status int, reason, headers *uint16, out *uintptr) uintptr {
	r, _, _ := purego.SyscallN(i.vtbl.CreateWebResourceResponse,
		uintptr(unsafe.Pointer(i)), stream, uintptr(status),
		uintptr(unsafe.Pointer(reason)), uintptr(unsafe.Pointer(headers)), uintptr(unsafe.Pointer(out)))
	return r
}

// --- SHCreateMemStream (shlwapi) -------------------------------------------

var (
	memStreamOnce sync.Once
	memStreamProc uintptr
)

// shCreateMemStream wraps SHCreateMemStream, which copies the bytes into a
// COM-owned IStream — so the Go slice need not outlive the call.
func shCreateMemStream(data []byte) uintptr {
	memStreamOnce.Do(func() {
		mod, err := syscall.LoadLibrary("shlwapi.dll")
		if err != nil {
			return
		}
		addr, err := syscall.GetProcAddress(mod, "SHCreateMemStream")
		if err != nil {
			return
		}
		memStreamProc = addr
	})
	if memStreamProc == 0 {
		return 0
	}
	var p *byte
	if len(data) > 0 {
		p = &data[0]
	}
	r, _, _ := purego.SyscallN(memStreamProc, uintptr(unsafe.Pointer(p)), uintptr(uint32(len(data)))) // #nosec G103 -- SHCreateMemStream copies the buffer
	return r
}

// --- request handling ------------------------------------------------------

// serveScheme looks up the handler for a scheme and invokes it (nil if none).
func (w *webview) serveScheme(scheme, requestURL string) *SchemeResponse {
	w.mu.Lock()
	h := w.schemeHandlers[scheme]
	w.mu.Unlock()
	if h == nil {
		return nil
	}
	return h(&SchemeRequest{URL: requestURL})
}

// schemeForVHostURL returns the registered scheme whose vhost matches uri, or "".
func (w *webview) schemeForVHostURL(uri string) string {
	for scheme := range w.schemeHandlers {
		if strings.HasPrefix(uri, schemeVHost(scheme)+"/") {
			return scheme
		}
	}
	return ""
}

// serveSchemeWindows answers one WebResourceRequested event from the matching
// SchemeHandler. A nil response is left for WebView2 to turn into its default
// 404 (put no response).
func (w *webview) serveSchemeWindows(args uintptr) {
	a := asWRRArgs(args)
	var reqPtr uintptr
	if int32(a.GetRequest(&reqPtr)) < 0 || reqPtr == 0 {
		return
	}
	var uriPtr uintptr
	if int32(asWRRequest(reqPtr).GetUri(&uriPtr)) < 0 || uriPtr == 0 {
		return
	}
	uri := wideToString(uriPtr)
	coTaskMemFree(uriPtr)

	scheme := w.schemeForVHostURL(uri)
	if scheme == "" {
		return
	}
	resp := w.serveScheme(scheme, w.canonicalSchemeURL(scheme, uri))
	if resp == nil || w.environment == 0 {
		return
	}

	stream := shCreateMemStream(resp.Body)
	headers := "Content-Type: " + schemeMIME(resp)
	var respObj uintptr
	if int32(asEnvironment(w.environment).CreateWebResourceResponse(stream, 200, utf16("OK"), utf16(headers), &respObj)) < 0 || respObj == 0 {
		return
	}
	a.PutResponse(respObj)
}

// rewriteSchemeURL maps a registered "<scheme>://.../path" URL to its https
// vhost so a single scheme URL works on every platform; other URLs pass through.
func (w *webview) rewriteSchemeURL(raw string) string {
	if len(w.schemeHandlers) == 0 {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	_, ok := w.schemeHandlers[u.Scheme]
	if !ok {
		return raw
	}
	// The vhost origin has no place for the scheme's authority, so remember it
	// here; serveSchemeWindows uses it to rebuild the original URL for the
	// handler (the other backends preserve the authority natively).
	if u.Host != "" {
		w.mu.Lock()
		if w.schemeAuthority == nil {
			w.schemeAuthority = map[string]string{}
		}
		w.schemeAuthority[u.Scheme] = u.Host
		w.mu.Unlock()
	}
	out := schemeVHost(u.Scheme) + u.Path
	if u.RawQuery != "" {
		out += "?" + u.RawQuery
	}
	// Preserve the fragment: it is client-side (never sent to the handler), but
	// dropping it here would break hash/path routing on the initial Navigate.
	if u.Fragment != "" {
		out += "#" + u.EscapedFragment()
	}
	return out
}

// canonicalSchemeURL turns the internal https vhost URL WebView2 delivers back
// into the "<scheme>://<authority>/path?query" form the macOS/Linux backends
// pass to a handler, so SchemeRequest.URL has one shape on every platform. The
// authority is the one the app navigated with (recorded in rewriteSchemeURL),
// falling back to the scheme name if none was seen. Fragments are client-side
// and never reach a resource request, so none is reconstructed.
func (w *webview) canonicalSchemeURL(scheme, vhostURL string) string {
	u, err := url.Parse(vhostURL)
	if err != nil {
		return vhostURL
	}
	w.mu.Lock()
	authority := w.schemeAuthority[scheme]
	w.mu.Unlock()
	if authority == "" {
		authority = scheme
	}
	out := scheme + "://" + authority + u.Path
	if u.RawQuery != "" {
		out += "?" + u.RawQuery
	}
	return out
}
