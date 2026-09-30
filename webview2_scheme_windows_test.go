package glaze

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

// A window without schemes keeps the folder it always had; a window with
// schemes gets one named for exactly that set, in a stable order, because
// WebView2 refuses to share a browser process between different registrations.
func TestUserDataFolder(t *testing.T) {
	exe, _ := os.Executable()
	base := filepath.Base(exe)

	got := filepath.Base(userDataFolder(nil))
	if got != base {
		t.Errorf("no schemes: folder %q, want %q", got, base)
	}
	schemes := sortedSchemes(map[string]SchemeHandler{"probe": nil, "app": nil})
	got = filepath.Base(userDataFolder(schemes))
	want := base + "-schemes-app-probe"
	if got != want {
		t.Errorf("schemes %v: folder %q, want %q", schemes, got, want)
	}
}

func TestRuntimeVersion(t *testing.T) {
	dll := filepath.Join(`C:\Program Files (x86)\Microsoft\EdgeWebView\Application`, "154.0.4258.37", "EBWebView", "arm64", "EmbeddedBrowserWebView.dll")
	got := runtimeVersion(dll)
	if got != "154.0.4258.37" {
		t.Errorf("runtimeVersion = %q, want 154.0.4258.37", got)
	}
}

// comCall invokes a COM method through its vtable slot, as the WebView2
// runtime does, so this exercises the callbacks' real calling convention.
func comCall(fn uintptr, args ...uintptr) uintptr {
	r, _, _ := purego.SyscallN(fn, args...)
	return r
}

func comString(t *testing.T, p uintptr) string {
	t.Helper()
	if p == 0 {
		t.Fatal("null string")
	}
	defer coTaskMemFree(p)
	return wideToString(p)
}

// The options object must answer exactly what the runtime reads from the
// SDK's own: Options4 through QueryInterface, one registration per scheme,
// each secure, with an authority, open to its own scheme's origins. Headless:
// no window, only COM calls into the Go objects.
func TestEnvOptionsRegistrations(t *testing.T) {
	if ensureCOMInit() != nil {
		t.Skip("COM not available")
	}
	opts := newEnvOptions([]string{"app"})
	o := (*envOptions)(ptr(opts))

	var o4 uintptr
	hr := comCall(o.vtbl.QueryInterface, opts, uintptr(unsafe.Pointer(&iidEnvironmentOptions4)), uintptr(unsafe.Pointer(&o4)))
	if hr != hrOK || o4 == 0 {
		t.Fatalf("QueryInterface(Options4) = 0x%08X, %#x", hr, o4)
	}
	var unk uintptr
	comCall((*envOptions4)(ptr(o4)).vtbl.QueryInterface, o4, uintptr(unsafe.Pointer(&iidIUnknown)), uintptr(unsafe.Pointer(&unk)))
	if unk != opts {
		t.Errorf("IUnknown from Options4 = %#x, want the options object %#x (COM identity)", unk, opts)
	}

	var version uintptr
	comCall(o.vtbl.GetTargetCompatibleBrowserVersion, opts, uintptr(unsafe.Pointer(&version)))
	got := comString(t, version)
	if got != targetBrowserVersion {
		t.Errorf("TargetCompatibleBrowserVersion = %q, want %q", got, targetBrowserVersion)
	}

	var count uint32
	var arr uintptr
	hr = comCall((*envOptions4)(ptr(o4)).vtbl.GetCustomSchemeRegistrations, o4, uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&arr)))
	if hr != hrOK || count != 1 || arr == 0 {
		t.Fatalf("GetCustomSchemeRegistrations = 0x%08X, count %d", hr, count)
	}
	reg := unsafe.Slice((*uintptr)(ptr(arr)), count)[0]
	coTaskMemFree(arr)
	rv := (*schemeReg)(ptr(reg)).vtbl

	var name uintptr
	comCall(rv.GetSchemeName, reg, uintptr(unsafe.Pointer(&name)))
	got = comString(t, name)
	if got != "app" {
		t.Errorf("SchemeName = %q, want app", got)
	}
	var secure, authority int32
	comCall(rv.GetTreatAsSecure, reg, uintptr(unsafe.Pointer(&secure)))
	comCall(rv.GetHasAuthorityComponent, reg, uintptr(unsafe.Pointer(&authority)))
	if secure != 1 || authority != 1 {
		t.Errorf("TreatAsSecure=%d HasAuthorityComponent=%d, want 1 and 1", secure, authority)
	}
	var nOrigins uint32
	var origins uintptr
	comCall(rv.GetAllowedOrigins, reg, uintptr(unsafe.Pointer(&nOrigins)), uintptr(unsafe.Pointer(&origins)))
	if nOrigins != 1 || origins == 0 {
		t.Fatalf("AllowedOrigins count = %d", nOrigins)
	}
	origin := unsafe.Slice((*uintptr)(ptr(origins)), nOrigins)[0]
	coTaskMemFree(origins)
	got = comString(t, origin)
	if got != "app://*" {
		t.Errorf("AllowedOrigins = %q, want app://*", got)
	}

	if newEnvOptions([]string{"app"}) != opts {
		t.Error("the same scheme set must reuse one options object (identical registrations)")
	}
}

var resWinScheme atomic.Value // string

// winSchemeScenario loads a page from app://home whose only script is named
// by an ABSOLUTE app:// URL, the way the scheme is documented. The script
// reports the page's origin back through another absolute URL. When the scheme
// was emulated over https://app.localhost, the document loaded but an absolute
// app:// URL named a scheme WebView2 did not know: the script was never
// requested and nothing was reported.
func winSchemeScenario() string {
	var w WebView
	report := make(chan string, 1)
	var scriptURL atomic.Value // string
	page := `<!DOCTYPE html><html><head><script src="app://home/app.js"></script></head><body></body></html>`
	script := `new Image().src = 'app://home/report?' + encodeURIComponent([location.origin, window.isSecureContext].join(','));`
	handler := func(req *SchemeRequest) *SchemeResponse {
		switch {
		case strings.HasSuffix(req.URL, "/app.js"):
			scriptURL.Store(req.URL)
			return &SchemeResponse{Body: []byte(script), MIMEType: "text/javascript"}
		case strings.Contains(req.URL, "/report?"):
			_, query, _ := strings.Cut(req.URL, "report?")
			got, err := url.QueryUnescape(query)
			if err != nil {
				got = "bad report: " + err.Error()
			}
			select {
			case report <- got:
			default:
			}
			w.Terminate()
			return &SchemeResponse{}
		}
		return &SchemeResponse{Body: []byte(page), MIMEType: "text/html"}
	}

	w, err := NewWithOptions(Options{SchemeHandlers: map[string]SchemeHandler{"app": handler}})
	if err != nil {
		return "new error: " + err.Error()
	}
	defer w.Destroy()
	time.AfterFunc(20*time.Second, w.Terminate)
	w.Navigate("app://home/index.html")
	w.Run()

	select {
	case got := <-report:
		js, _ := scriptURL.Load().(string)
		return got + " " + js
	default:
		return "no report: the absolute app:// script never loaded"
	}
}

func TestSchemeAbsoluteURLs(t *testing.T) {
	got, _ := resWinScheme.Load().(string)
	requireGUI(t, got)
	want := "app://home,true app://home/app.js"
	if got != want {
		t.Fatalf("absolute app:// sub-resource: got %q, want %q (origin, secure context, the URL the handler saw)", got, want)
	}
}
