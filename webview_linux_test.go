package glaze

import (
	"fmt"
	"github.com/ebitengine/purego"
	"strings"

	"errors"
	"flag"
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"
)

// The GTK backend runs on one OS thread, so the GUI scenarios run in TestMain
// (the main goroutine) and stash results; the TestXxx functions only assert.
// Needs an X display — run under xvfb-run on CI.

var (
	resBridge      atomic.Value // string
	resErrorUnbind atomic.Value // string
	resRichTypes   atomic.Value // string
	resEmbed       atomic.Value // string
	resDialogCfg   atomic.Value // string
)

// hasDisplay reports whether a windowing system is available.
func hasDisplay() bool {
	return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}

// guiAvailable reports whether the GTK/WebKitGTK stack can actually run here:
// the shared libraries load AND a display is present. Without both, the GUI
// scenarios are skipped so `go test ./...` stays green on a headless box or one
// without WebKitGTK in the loader path (minimal containers, Nix, etc.) instead
// of failing. CI installs the libraries and runs under xvfb, which sets DISPLAY.
func guiAvailable() bool {
	return hasDisplay() && ensureInit() == nil
}

func TestMain(m *testing.M) {
	flag.Parse()
	runtime.LockOSThread()
	if !testing.Short() && guiAvailable() {
		resBridge.Store(bridgeScenario())
		resErrorUnbind.Store(errorUnbindScenario())
		resRichTypes.Store(richTypesScenario())
		resEmbed.Store(embedScenario())
		resDialogCfg.Store(dialogConfigScenario())
		resNoBridge.Store(noBridgeScenario())
		resHideUntilLoaded.Store(hideUntilLoadedScenario())
		resNavigation.Store(navigationScenario())
		resSchemeReuse.Store(schemeReuseScenario())
		resHistory.Store(historyScenario())
		resNewWindow.Store(newWindowScenario())
		resFind.Store(findScenario())
		resZoom.Store(zoomScenario())
		resClipboard.Store(clipboardScenario())
	}
	os.Exit(m.Run())
}

// embedScenario embeds a web view into a caller-provided GtkWindow and verifies
// the engine does not take ownership and Destroy leaves the host window intact.
func embedScenario() string {
	err := ensureInit()
	if err != nil {
		return "init error: " + err.Error()
	}
	if !gtkInit() {
		return "gtk_init failed"
	}
	host := gtkNewWindow()
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
	// Host must still be alive after Destroy (this call would fault on a freed
	// widget), then tear it down ourselves.
	gtkWindowSetTitle(host, "still alive")
	gtkWindowClose(host)
	if owns {
		return "owns=true (BUG: should not own external window)"
	}
	return "embed-ok"
}

func TestEmbedExternalWindow(t *testing.T) {
	got, _ := resEmbed.Load().(string)
	requireGUI(t, got)
	if got != "embed-ok" {
		t.Fatalf("embed external window = %q, want %q", got, "embed-ok")
	}
}

func bridgeScenario() string {
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
	time.AfterFunc(15*time.Second, w.Terminate)

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

func errorUnbindScenario() string {
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
	time.AfterFunc(15*time.Second, w.Terminate)

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

type point struct{ X, Y int }

func richTypesScenario() string {
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
	_ = w.Bind("echoPoint", func(p point) point { return point{p.X + 1, p.Y + 1} })
	_ = w.Bind("sum", func(xs []int) int {
		t := 0
		for _, x := range xs {
			t += x
		}
		return t
	})
	time.AfterFunc(15*time.Second, w.Terminate)

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

// dialogConfigScenario verifies the GtkFileChooserNative plumbing: a chooser is
// created, configured for multiple selection and given a type filter, then
// destroyed -- all without gtk_native_dialog_run (which is modal and needs user
// input). The full dialog is exercised manually via examples/filedialog.
func dialogConfigScenario() string {
	err := ensureDialogInit()
	if err != nil {
		return "dialog init error: " + err.Error()
	}
	if !gtkInit() {
		return "gtk_init failed"
	}
	dlg := gtkFileChooserNativeNew("Pick", 0, gtkFileChooserActionOpen, "_Open", "_Cancel")
	if dlg == 0 {
		return "chooser nil"
	}
	defer gObjectUnref(dlg)
	gtkFileChooserSetSelectMultiple(dlg, true)
	applyChooserFilters(dlg, []FileFilter{{Name: "Images", Extensions: []string{"png", "jpg"}}})
	return "dialog-config-ok"
}

func TestDialogConfig(t *testing.T) {
	got, _ := resDialogCfg.Load().(string)
	requireGUI(t, got)
	if got != "dialog-config-ok" {
		t.Fatalf("dialog config = %q, want %q", got, "dialog-config-ok")
	}
}

// requireGUI skips a GUI assertion when its scenario did not run (no display).
func requireGUI(t *testing.T, got string) {
	t.Helper()
	if got == "" {
		t.Skip("WebKitGTK/display not available; install libwebkit2gtk and run under xvfb-run")
	}
}

func TestBridge(t *testing.T) {
	got, _ := resBridge.Load().(string)
	requireGUI(t, got)
	if got != "42|hi x" {
		t.Fatalf("JS<->Go bridge = %q, want %q", got, "42|hi x")
	}
}

func TestErrorAndUnbind(t *testing.T) {
	const want = "temp=undefined boom=kaboom"
	got, _ := resErrorUnbind.Load().(string)
	requireGUI(t, got)
	if got != want {
		t.Fatalf("error/unbind = %q, want %q", got, want)
	}
}

func TestRichBindingTypes(t *testing.T) {
	const want = "p=2,3 s=10"
	got, _ := resRichTypes.Load().(string)
	requireGUI(t, got)
	if got != want {
		t.Fatalf("rich types = %q, want %q", got, want)
	}
}

var resClipboard atomic.Value // string

// clipboardScenario reads WebKitGTK's script clipboard setting for a web view
// with and without NoBridge: a page the app does not control gets no script
// access to the clipboard, the app's own pages keep it.
func clipboardScenario() string {
	names := []string{"libwebkit2gtk-4.1.so.0", "libwebkit2gtk-4.0.so.37"}
	if gtk4 {
		names = []string{"libwebkitgtk-6.0.so.4"}
	}
	var got []string
	for _, noBridge := range []bool{false, true} {
		v, err := NewWithOptions(Options{NoBridge: noBridge})
		if err != nil {
			return "new error: " + err.Error()
		}
		webkit, err := openFirst(names...)
		if err != nil {
			return "webkit: " + err.Error()
		}
		var allowed func(settings uintptr) bool
		purego.RegisterLibFunc(&allowed, webkit, "webkit_settings_get_javascript_can_access_clipboard")
		got = append(got, fmt.Sprintf("nobridge=%v clipboard=%v", noBridge, allowed(webkitWebViewGetSettings(v.(*webview).webview))))
		v.Destroy()
	}
	return strings.Join(got, ", ")
}

func TestNoBridgeDeniesClipboard(t *testing.T) {
	got, _ := resClipboard.Load().(string)
	requireGUI(t, got)
	want := "nobridge=false clipboard=true, nobridge=true clipboard=false"
	if got != want {
		t.Fatalf("script clipboard access: got %q, want %q", got, want)
	}
}
