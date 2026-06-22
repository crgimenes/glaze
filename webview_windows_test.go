//go:build windows

package glaze

import (
	"errors"
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
)

func TestMain(m *testing.M) {
	runtime.LockOSThread()
	resWinBridge.Store(winBridgeScenario())
	resWinErrorUnbind.Store(winErrorUnbindScenario())
	resWinRichTypes.Store(winRichTypesScenario())
	resWinEmbed.Store(winEmbedScenario())
	os.Exit(m.Run())
}

// winEmbedScenario embeds a web view into a caller-provided HWND and verifies
// the engine does not take ownership and Destroy leaves the host window intact.
func winEmbedScenario() string {
	if err := ensureWinInit(); err != nil {
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
	if got, _ := resWinEmbed.Load().(string); got != "embed-ok" {
		t.Fatalf("embed external window = %q, want %q", got, "embed-ok")
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
	if got, _ := resWinBridge.Load().(string); got != "42|hi x" {
		t.Fatalf("JS<->Go bridge = %q, want %q", got, "42|hi x")
	}
}

func TestErrorAndUnbind(t *testing.T) {
	const want = "temp=undefined boom=kaboom"
	if got, _ := resWinErrorUnbind.Load().(string); got != want {
		t.Fatalf("error/unbind = %q, want %q", got, want)
	}
}

func TestRichBindingTypes(t *testing.T) {
	const want = "p=2,3 s=10"
	if got, _ := resWinRichTypes.Load().(string); got != want {
		t.Fatalf("rich types = %q, want %q", got, want)
	}
}
