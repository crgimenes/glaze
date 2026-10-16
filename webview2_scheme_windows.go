// Custom-scheme serving for the WebView2 backend.
//
// A scheme is registered with the environment as a real custom scheme
// (ICoreWebView2EnvironmentOptions4::GetCustomSchemeRegistrations), marked
// secure and with an authority, exactly as WKWebView and WebKitGTK register it.
// So "app://home/index.html" is the document's real URL: its origin is
// app://home, and absolute "app://home/..." sub-resources inside it resolve and
// load. The request never leaves the process: AddWebResourceRequestedFilter +
// the WebResourceRequested event answer it from the SchemeHandler in memory.
package glaze

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/ebitengine/purego"
)

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

// iUnknown is the prefix every COM interface starts with; releaseUnknown drops
// one reference on any interface pointer, so acquired objects (request, stream,
// response) do not leak — COM out-params and factories hand back AddRef'd
// references the caller owns.
type iUnknown struct{ vtbl *iUnknownVtbl }

func releaseUnknown(p uintptr) {
	if p == 0 {
		return
	}
	u := (*iUnknown)(ptr(p))
	purego.SyscallN(u.vtbl.Release, p)
}

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
	return callSchemeHandler(h, &SchemeRequest{URL: requestURL})
}

// serveSchemeWindows answers one WebResourceRequested event from the matching
// SchemeHandler. The request URI is already the scheme URL the page used
// ("app://home/app.js"), so it goes to the handler unchanged, as on macOS and
// Linux. A nil response is left for WebView2 to turn into its default 404 (put
// no response).
func (w *webview) serveSchemeWindows(args uintptr) {
	a := asWRRArgs(args)
	var reqPtr uintptr
	if int32(a.GetRequest(&reqPtr)) < 0 || reqPtr == 0 {
		return
	}
	defer releaseUnknown(reqPtr)
	var uriPtr uintptr
	if int32(asWRRequest(reqPtr).GetUri(&uriPtr)) < 0 || uriPtr == 0 {
		return
	}
	uri := wideToString(uriPtr)
	coTaskMemFree(uriPtr)

	scheme, _, found := strings.Cut(uri, ":")
	if !found {
		return
	}
	resp := w.serveScheme(scheme, uri)
	if resp == nil || w.environment == 0 {
		return
	}

	stream := shCreateMemStream(resp.Body)
	if stream == 0 {
		return
	}
	// The response object retains the stream, and PutResponse's WebView2 side
	// retains the response; the references acquired here are dropped once the
	// handoff is done (deferred, LIFO), so served assets stop leaking COM
	// objects.
	defer releaseUnknown(stream)
	headers := "Content-Type: " + schemeMIME(resp)
	var respObj uintptr
	if int32(asEnvironment(w.environment).CreateWebResourceResponse(stream, 200, utf16("OK"), utf16(headers), &respObj)) < 0 || respObj == 0 {
		return
	}
	defer releaseUnknown(respObj)
	a.PutResponse(respObj)
}

// sortedSchemes returns the registered scheme names in a stable order, so the
// same handlers always produce the same registrations and the same folder.
func sortedSchemes(handlers map[string]SchemeHandler) []string {
	names := make([]string, 0, len(handlers))
	for name := range handlers {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// userDataFolder is where WebView2 keeps the profile. Every environment that
// shares a browser process must be created with identical custom-scheme
// registrations, or creation fails; environments share the browser process
// when they share this folder. So the windows registering schemes get a
// folder of their own, and a window without any keeps the original folder
// unchanged. The suffix names no scheme on purpose: a folder named for the
// set would move, and so reset every user's storage, the day an app adds a
// scheme. The price is one set per process, which newEnvOptions enforces.
func userDataFolder(schemes []string) string {
	base := os.Getenv("APPDATA")
	if base == "" {
		base = os.TempDir()
	}
	exe, _ := os.Executable()
	name := filepath.Base(exe)
	if len(schemes) > 0 {
		name += "-schemes"
	}
	return filepath.Join(base, name)
}

// --- environment options: real custom-scheme registration ------------------
//
// WebView2 reads custom schemes from the options object passed when the
// environment is created. The SDK's options class is C++ in a header
// (WebView2EnvironmentOptions.h), so glaze implements the three interfaces the
// runtime reads -- ICoreWebView2EnvironmentOptions, ...Options4 and
// ICoreWebView2CustomSchemeRegistration -- as read-only COM objects over Go
// vtables. They live for the life of the process (kept in envOptionsCache),
// so AddRef/Release do not count.

var (
	iidEnvironmentOptions  = guid{0x2FDE08A8, 0x1E9A, 0x4766, [8]byte{0x8C, 0x05, 0x95, 0xA9, 0xCE, 0xB9, 0xD1, 0xC5}}
	iidEnvironmentOptions4 = guid{0xAC52D13F, 0x0D38, 0x475A, [8]byte{0x9D, 0xCA, 0x87, 0x65, 0x80, 0xD6, 0x79, 0x3E}}
	iidCustomSchemeReg     = guid{0xD60AC92C, 0x37A6, 0x4B26, [8]byte{0xA3, 0x9E, 0x95, 0xCF, 0xE5, 0x90, 0x47, 0xBB}}
)

const (
	// targetBrowserVersion is CORE_WEBVIEW_TARGET_PRODUCT_VERSION of WebView2
	// SDK 1.0.1587.40, the first stable SDK with custom-scheme registration;
	// the SDK's own options object reports its version here.
	targetBrowserVersion = "110.0.1587.40"
	// minSchemeAPIVersion is that runtime's build number. An older runtime
	// would ignore the registrations and the scheme would silently not load,
	// so NewWithOptions refuses it instead.
	minSchemeAPIVersion = 1587

	hrOK          = 0
	hrNotImpl     = 0x80004001 // E_NOTIMPL
	hrNoInterface = 0x80004002 // E_NOINTERFACE
	hrPointer     = 0x80004003 // E_POINTER
	hrOutOfMemory = 0x8007000E // E_OUTOFMEMORY
)

type envOptionsVtbl struct {
	iUnknownVtbl
	GetAdditionalBrowserArguments             uintptr
	PutAdditionalBrowserArguments             uintptr
	GetLanguage                               uintptr
	PutLanguage                               uintptr
	GetTargetCompatibleBrowserVersion         uintptr
	PutTargetCompatibleBrowserVersion         uintptr
	GetAllowSingleSignOnUsingOSPrimaryAccount uintptr
	PutAllowSingleSignOnUsingOSPrimaryAccount uintptr
}

type envOptions4Vtbl struct {
	iUnknownVtbl
	GetCustomSchemeRegistrations uintptr
	SetCustomSchemeRegistrations uintptr
}

type schemeRegVtbl struct {
	iUnknownVtbl
	GetSchemeName            uintptr
	GetTreatAsSecure         uintptr
	PutTreatAsSecure         uintptr
	GetAllowedOrigins        uintptr
	SetAllowedOrigins        uintptr
	GetHasAuthorityComponent uintptr
	PutHasAuthorityComponent uintptr
}

// envOptions is ICoreWebView2EnvironmentOptions; o4 is the same object's
// ICoreWebView2EnvironmentOptions4. Both answer QueryInterface for either, and
// both return envOptions for IUnknown, so COM identity holds. The vtbl
// pointer must stay the first field of each.
type envOptions struct {
	vtbl    *envOptionsVtbl
	o4      *envOptions4
	regs    []*schemeReg
	schemes []string // sorted; the set this process is registered for
}

type envOptions4 struct {
	vtbl *envOptions4Vtbl
	base *envOptions
}

// schemeReg is one ICoreWebView2CustomSchemeRegistration.
type schemeReg struct {
	vtbl   *schemeRegVtbl
	scheme string
}

var (
	envOptionsVtblOnce sync.Once
	envOptionsVtblPtr  *envOptionsVtbl
	envOptions4VtblPtr *envOptions4Vtbl
	schemeRegVtblPtr   *schemeRegVtbl

	envOptionsMu  sync.Mutex
	envOptionsObj *envOptions // the one registration of this process
)

// newEnvOptions returns the options object registering schemes (sorted):
// secure, with an authority (so app://home is an origin, like http://home),
// and reachable from any origin of the same scheme (so a same-origin POST,
// which always carries an Origin header, is allowed; https pages are not).
// There is one object per process: every window with schemes shares the
// profile folder (see userDataFolder), where WebView2 requires identical
// registrations, so a different set is refused rather than left to fail
// inside WebView2 with a message of its own.
func newEnvOptions(schemes []string) (uintptr, error) {
	envOptionsVtblOnce.Do(func() {
		addRef := purego.NewCallback(staticAddRef)
		release := purego.NewCallback(staticRelease)
		notImpl1 := purego.NewCallback(comNotImpl1)
		notImpl2 := purego.NewCallback(comNotImpl2)
		getFalse := purego.NewCallback(comGetFalse)
		getTrue := purego.NewCallback(comGetTrue)
		getNull := purego.NewCallback(comGetNullString)
		envOptionsVtblPtr = &envOptionsVtbl{
			iUnknownVtbl:                              iUnknownVtbl{purego.NewCallback(envOptionsQI), addRef, release},
			GetAdditionalBrowserArguments:             getNull,
			PutAdditionalBrowserArguments:             notImpl1,
			GetLanguage:                               getNull,
			PutLanguage:                               notImpl1,
			GetTargetCompatibleBrowserVersion:         purego.NewCallback(envOptionsTargetVersion),
			PutTargetCompatibleBrowserVersion:         notImpl1,
			GetAllowSingleSignOnUsingOSPrimaryAccount: getFalse,
			PutAllowSingleSignOnUsingOSPrimaryAccount: notImpl1,
		}
		envOptions4VtblPtr = &envOptions4Vtbl{
			iUnknownVtbl:                 iUnknownVtbl{purego.NewCallback(envOptions4QI), addRef, release},
			GetCustomSchemeRegistrations: purego.NewCallback(envOptionsRegistrations),
			SetCustomSchemeRegistrations: notImpl2,
		}
		schemeRegVtblPtr = &schemeRegVtbl{
			iUnknownVtbl:             iUnknownVtbl{purego.NewCallback(schemeRegQI), addRef, release},
			GetSchemeName:            purego.NewCallback(schemeRegName),
			GetTreatAsSecure:         getTrue,
			PutTreatAsSecure:         notImpl1,
			GetAllowedOrigins:        purego.NewCallback(schemeRegAllowedOrigins),
			SetAllowedOrigins:        notImpl2,
			GetHasAuthorityComponent: getTrue,
			PutHasAuthorityComponent: notImpl1,
		}
	})

	envOptionsMu.Lock()
	defer envOptionsMu.Unlock()
	o := envOptionsObj
	if o != nil && !slices.Equal(o.schemes, schemes) {
		return 0, fmt.Errorf("webview2: this process registered the custom schemes %v already; every window in it must register that same set, not %v", o.schemes, schemes)
	}
	if o == nil {
		o = &envOptions{vtbl: envOptionsVtblPtr, schemes: slices.Clone(schemes)}
		o.o4 = &envOptions4{vtbl: envOptions4VtblPtr, base: o}
		for _, s := range schemes {
			o.regs = append(o.regs, &schemeReg{vtbl: schemeRegVtblPtr, scheme: s})
		}
		envOptionsObj = o
	}
	return uintptr(unsafe.Pointer(o)), nil // #nosec G103 -- process-lifetime COM object (envOptionsObj)
}

// setOut writes a pointer-sized value through a COM out-parameter.
func setOut(out, v uintptr) { *(*uintptr)(ptr(out)) = v }

// coTaskMemUTF16 copies s into CoTaskMemAlloc'd UTF-16: COM out-strings are
// freed by the caller with CoTaskMemFree. 0 on failure.
func coTaskMemUTF16(s string) uintptr {
	u, err := syscall.UTF16FromString(s)
	if err != nil {
		return 0
	}
	p := coTaskMemAlloc(uintptr(len(u)) * 2)
	if p == 0 {
		return 0
	}
	copy(unsafe.Slice((*uint16)(ptr(p)), len(u)), u) // #nosec G103 -- fills the CoTaskMemAlloc buffer sized above
	return p
}

// coTaskMemPtrs copies ptrs into a CoTaskMemAlloc'd array the caller frees.
func coTaskMemPtrs(ptrs []uintptr) uintptr {
	p := coTaskMemAlloc(uintptr(len(ptrs)) * unsafe.Sizeof(uintptr(0)))
	if p == 0 {
		return 0
	}
	copy(unsafe.Slice((*uintptr)(ptr(p)), len(ptrs)), ptrs) // #nosec G103 -- fills the CoTaskMemAlloc buffer sized above
	return p
}

func envOptionsQI(this, riid, ppv uintptr) uintptr {
	return optionsQueryInterface((*envOptions)(ptr(this)), riid, ppv)
}

func envOptions4QI(this, riid, ppv uintptr) uintptr {
	return optionsQueryInterface((*envOptions4)(ptr(this)).base, riid, ppv)
}

func optionsQueryInterface(o *envOptions, riid, ppv uintptr) uintptr {
	if ppv == 0 {
		return hrPointer
	}
	want := (*guid)(ptr(riid))
	switch {
	case guidEqual(want, &iidIUnknown), guidEqual(want, &iidEnvironmentOptions):
		setOut(ppv, uintptr(unsafe.Pointer(o))) // #nosec G103 -- process-lifetime COM object
	case guidEqual(want, &iidEnvironmentOptions4):
		setOut(ppv, uintptr(unsafe.Pointer(o.o4))) // #nosec G103 -- process-lifetime COM object
	default:
		// Options2/3/5+ are not implemented: the runtime then uses its
		// defaults, as it does for any option glaze does not set.
		setOut(ppv, 0)
		return hrNoInterface
	}
	return hrOK
}

func schemeRegQI(this, riid, ppv uintptr) uintptr {
	if ppv == 0 {
		return hrPointer
	}
	want := (*guid)(ptr(riid))
	if guidEqual(want, &iidIUnknown) || guidEqual(want, &iidCustomSchemeReg) {
		setOut(ppv, this)
		return hrOK
	}
	setOut(ppv, 0)
	return hrNoInterface
}

func staticAddRef(uintptr) uintptr  { return 2 }
func staticRelease(uintptr) uintptr { return 1 }

// The objects are read-only: glaze builds them, the runtime only reads them.
func comNotImpl1(_, _ uintptr) uintptr    { return hrNotImpl }
func comNotImpl2(_, _, _ uintptr) uintptr { return hrNotImpl }

func comGetBool(out uintptr, v int32) uintptr {
	if out == 0 {
		return hrPointer
	}
	*(*int32)(ptr(out)) = v // BOOL
	return hrOK
}

func comGetFalse(_, out uintptr) uintptr { return comGetBool(out, 0) }
func comGetTrue(_, out uintptr) uintptr  { return comGetBool(out, 1) }

// comGetNullString reports an unset string option, as the SDK's options
// object does for one never put.
func comGetNullString(_, out uintptr) uintptr {
	if out == 0 {
		return hrPointer
	}
	setOut(out, 0)
	return hrOK
}

func comGetString(out uintptr, s string) uintptr {
	if out == 0 {
		return hrPointer
	}
	p := coTaskMemUTF16(s)
	if p == 0 {
		return hrOutOfMemory
	}
	setOut(out, p)
	return hrOK
}

func envOptionsTargetVersion(_, out uintptr) uintptr {
	return comGetString(out, targetBrowserVersion)
}

// envOptionsRegistrations is GetCustomSchemeRegistrations: an allocated array
// of registration pointers, which the caller releases (a no-op here) and frees.
func envOptionsRegistrations(this, count, out uintptr) uintptr {
	if count == 0 || out == 0 {
		return hrPointer
	}
	o := (*envOptions4)(ptr(this)).base
	ptrs := make([]uintptr, len(o.regs))
	for i, r := range o.regs {
		ptrs[i] = uintptr(unsafe.Pointer(r)) // #nosec G103 -- process-lifetime COM object
	}
	arr := coTaskMemPtrs(ptrs)
	if arr == 0 {
		return hrOutOfMemory
	}
	*(*uint32)(ptr(count)) = uint32(len(ptrs)) // #nosec G115 -- one per registered scheme
	setOut(out, arr)
	return hrOK
}

func schemeRegName(this, out uintptr) uintptr {
	return comGetString(out, (*schemeReg)(ptr(this)).scheme)
}

// schemeRegAllowedOrigins is GetAllowedOrigins: "<scheme>://*", every origin
// of this scheme. Requests without an Origin header (navigations, <script>,
// <img>, <link>) are always allowed; this admits the ones with one, such as a
// same-origin POST, which macOS and Linux allow too.
func schemeRegAllowedOrigins(this, count, out uintptr) uintptr {
	if count == 0 || out == 0 {
		return hrPointer
	}
	origin := coTaskMemUTF16((*schemeReg)(ptr(this)).scheme + "://*")
	if origin == 0 {
		return hrOutOfMemory
	}
	arr := coTaskMemPtrs([]uintptr{origin})
	if arr == 0 {
		coTaskMemFree(origin)
		return hrOutOfMemory
	}
	*(*uint32)(ptr(count)) = 1
	setOut(out, arr)
	return hrOK
}
