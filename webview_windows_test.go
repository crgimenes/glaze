package glaze

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"
)

// The WebView2/COM backend runs on one OS thread, so the GUI scenarios run in
// TestMain (the main goroutine) and stash their results; the TestXxx functions
// only assert. Needs the Edge WebView2 Runtime (present on windows-latest CI).

var (
	resWinBridge      atomic.Value // string
	resWinErrorUnbind atomic.Value // string
	resWinRichTypes   atomic.Value // string
	resWinEmbed       atomic.Value // string
	resWinClose       atomic.Value // string
	resWinDialogCfg   atomic.Value // string
)

// guiAvailable reports whether the Edge WebView2 Runtime is installed. Without
// it the GUI scenarios are skipped so `go test ./...` stays green instead of
// failing on a machine without the runtime; windows-latest CI ships it.
func guiAvailable() bool {
	if ensureCOMInit() != nil {
		return false
	}
	_, err := findEmbeddedBrowserDLL()
	return err == nil
}

// requireGUI skips a GUI assertion when its scenario did not run.
func requireGUI(t *testing.T, got string) {
	t.Helper()
	if got == "" {
		t.Skip("Edge WebView2 Runtime not available")
	}
}

func TestMain(m *testing.M) {
	flag.Parse()
	runtime.LockOSThread()
	if !testing.Short() && guiAvailable() {
		resWinBridge.Store(winBridgeScenario())
		resWinErrorUnbind.Store(winErrorUnbindScenario())
		resWinRichTypes.Store(winRichTypesScenario())
		resWinEmbed.Store(winEmbedScenario())
		resWinDialogCfg.Store(winDialogConfigScenario())
		resWinClose.Store(winCloseViaUIScenario()) // last: it ends with WM_QUIT
	}
	os.Exit(m.Run())
}

// winCloseViaUIScenario simulates the user closing the window (WM_CLOSE, as the
// X button / Alt+F4 send) and asserts that Run() returns. Without the WM_CLOSE
// -> PostQuitMessage path, Run would block forever, so a watchdog distinguishes
// "Run ended because of the close" from "Run had to be force-terminated".
func winCloseViaUIScenario() string {
	w, err := New(false)
	if err != nil {
		return "new error: " + err.Error()
	}
	defer w.Destroy()
	hwnd := w.(*webview).window

	var watchdogFired atomic.Bool
	time.AfterFunc(2*time.Second, func() { postMessageW(hwnd, wmClose, 0, 0) })
	time.AfterFunc(40*time.Second, func() { watchdogFired.Store(true); w.Terminate() })

	w.SetHtml(`<!DOCTYPE html><html><body>close test</body></html>`)
	w.Run() // must return once WM_CLOSE posts WM_QUIT

	if watchdogFired.Load() {
		return "hung (WM_CLOSE did not end Run)"
	}
	return "closed"
}

// winEmbedScenario embeds a web view into a caller-provided HWND and verifies
// the engine does not take ownership and Destroy leaves the host window intact.
func winEmbedScenario() string {
	err := ensureWinInit()
	if err != nil {
		return "init error: " + err.Error()
	}
	host := createWindowExW(0, utf16("STATIC"), utf16("host"), wsOverlappedWindow,
		cwUseDefault, cwUseDefault, 320, 240, 0, 0, getModuleHandleW(0), 0)
	if host == 0 {
		return "host window nil"
	}
	hostPtr := *(*unsafe.Pointer)(unsafe.Pointer(&host))
	w, err := NewWindow(false, hostPtr)
	if err != nil {
		return "new error: " + err.Error()
	}
	owns := w.(*webview).ownsWindow
	w.Destroy()
	// A valid window still returns its style; a destroyed HWND returns 0.
	alive := getWindowLongPtrW(host, gwlStyle) != 0
	destroyWindow(host)
	if owns {
		return "owns=true (BUG: should not own external window)"
	}
	if !alive {
		return "host destroyed (BUG)"
	}
	return "embed-ok"
}

func TestEmbedExternalWindow(t *testing.T) {
	got, _ := resWinEmbed.Load().(string)
	requireGUI(t, got)
	if got != "embed-ok" {
		t.Fatalf("embed external window = %q, want %q", got, "embed-ok")
	}
}

// winDialogConfigScenario verifies that the Common Item Dialog COM plumbing
// works: a FileOpenDialog is created via CoCreateInstance, its options round-trip
// through SetOptions/GetOptions, and a title + file-type filters are applied --
// all without calling Show (which is modal and needs user input). The full
// dialog is exercised manually via examples/filedialog.
func winDialogConfigScenario() string {
	err := ensureDialogInit()
	if err != nil {
		return "dialog init error: " + err.Error()
	}
	coInitializeEx(0, coinitApartmentThreaded) // ensure STA on the test thread
	var pdlg uintptr
	hr := coCreateInstance(&clsidFileOpenDialog, 0, clsctxInprocServer, &iidIFileOpenDialog, &pdlg)
	if hr < 0 || pdlg == 0 {
		return fmt.Sprintf("CoCreateInstance failed: 0x%08X", uint32(hr))
	}
	dlg := (*iFileDialog)(ptr(pdlg))
	defer dlg.Release()

	dlg.SetOptions(dlg.GetOptions() | fosForceFilesystem | fosPickFolders | fosAllowMultiSelect)
	got := dlg.GetOptions()
	if got&fosPickFolders == 0 || got&fosAllowMultiSelect == 0 {
		return fmt.Sprintf("options roundtrip lost bits: 0x%08X", got)
	}
	dlg.SetTitle(utf16("Pick"))
	specs, keep := buildFilterSpecs([]FileFilter{{Name: "Images", Extensions: []string{"png", "jpg"}}})
	if len(specs) != 1 {
		return "filter spec build failed"
	}
	dlg.SetFileTypes(uint32(len(specs)), &specs[0])
	runtime.KeepAlive(keep)
	return "dialog-config-ok"
}

func TestDialogConfig(t *testing.T) {
	got, _ := resWinDialogCfg.Load().(string)
	requireGUI(t, got)
	if got != "dialog-config-ok" {
		t.Fatalf("dialog config = %q, want %q", got, "dialog-config-ok")
	}
}

func TestCloseViaUI(t *testing.T) {
	got, _ := resWinClose.Load().(string)
	requireGUI(t, got)
	if got != "closed" {
		t.Fatalf("close via UI = %q, want %q", got, "closed")
	}
}

func winBridgeScenario() string {
	w, err := New(true)
	if err != nil {
		return "new error: " + err.Error()
	}
	defer w.Destroy()
	w.SetSize(700, 500, HintNone)

	done := make(chan string, 1)
	_ = w.Bind("add", func(a, b float64) float64 { return a + b })
	_ = w.Bind("hello", func(s string) string { return "hi " + s })
	_ = w.Bind("done", func(s string) {
		select {
		case done <- s:
		default:
		}
		w.Terminate()
	})
	time.AfterFunc(40*time.Second, w.Terminate) // watchdog

	w.SetHtml(`<!DOCTYPE html><html><body><script>
window.addEventListener('load', async function(){
  try {
    var s = await window.add(20, 22);
    var h = await window.hello("x");
    window.done(s + "|" + h);
  } catch(e) { window.done("ERR:" + e); }
});
</script></body></html>`)
	w.Run()

	select {
	case r := <-done:
		return r
	default:
		return "no report"
	}
}

// winErrorUnbindScenario covers a rejected-promise binding and, crucially, that
// an Unbound name does NOT reappear when a new document loads — the regression
// test for the doc-start onBind script leak (a buggy Unbind that left the
// document-start bind script installed would make window.temp a function again
// at load, so "temp=undefined" would fail).
func winErrorUnbindScenario() string {
	w, err := New(false)
	if err != nil {
		return "new error: " + err.Error()
	}
	defer w.Destroy()

	done := make(chan string, 1)
	_ = w.Bind("report", func(s string) {
		select {
		case done <- s:
		default:
		}
		w.Terminate()
	})
	_ = w.Bind("boom", func() (string, error) { return "", errors.New("kaboom") })
	_ = w.Bind("temp", func() string { return "x" })
	_ = w.Unbind("temp")
	time.AfterFunc(40*time.Second, w.Terminate)

	w.SetHtml(`<!DOCTYPE html><html><body><script>
window.addEventListener('load', async function(){
  var msg = 'temp=' + (typeof window.temp);
  try { await window.boom(); msg += ' boom=nope'; }
  catch(e){ msg += ' boom=' + e; }
  window.report(msg);
});
</script></body></html>`)
	w.Run()

	select {
	case r := <-done:
		return r
	default:
		return "no report"
	}
}

// xy mirrors the linux/darwin test's point struct (named differently here to
// avoid clashing with the Windows backend's own point type).
type xy struct{ X, Y int }

func winRichTypesScenario() string {
	w, err := New(false)
	if err != nil {
		return "new error: " + err.Error()
	}
	defer w.Destroy()

	done := make(chan string, 1)
	_ = w.Bind("report", func(s string) {
		select {
		case done <- s:
		default:
		}
		w.Terminate()
	})
	_ = w.Bind("echoPoint", func(p xy) xy { return xy{p.X + 1, p.Y + 1} })
	_ = w.Bind("sum", func(xs []int) int {
		t := 0
		for _, x := range xs {
			t += x
		}
		return t
	})
	time.AfterFunc(40*time.Second, w.Terminate)

	w.SetHtml(`<!DOCTYPE html><html><body><script>
window.addEventListener('load', async function(){
  try {
    var p = await window.echoPoint({X:1, Y:2});
    var s = await window.sum([1,2,3,4]);
    window.report('p=' + p.X + ',' + p.Y + ' s=' + s);
  } catch(e) { window.report('ERR:' + e); }
});
</script></body></html>`)
	w.Run()

	select {
	case r := <-done:
		return r
	default:
		return "no report"
	}
}

func TestBridge(t *testing.T) {
	got, _ := resWinBridge.Load().(string)
	requireGUI(t, got)
	if got != "42|hi x" {
		t.Fatalf("JS<->Go bridge = %q, want %q", got, "42|hi x")
	}
}

func TestErrorAndUnbind(t *testing.T) {
	const want = "temp=undefined boom=kaboom"
	got, _ := resWinErrorUnbind.Load().(string)
	requireGUI(t, got)
	if got != want {
		t.Fatalf("error/unbind = %q, want %q", got, want)
	}
}

func TestRichBindingTypes(t *testing.T) {
	const want = "p=2,3 s=10"
	got, _ := resWinRichTypes.Load().(string)
	requireGUI(t, got)
	if got != want {
		t.Fatalf("rich types = %q, want %q", got, want)
	}
}
