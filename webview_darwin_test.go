package glaze

import (
	"errors"
	"os"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/ebitengine/purego/objc"
)

// AppKit runs on one OS thread, so the GUI scenarios run in TestMain (the main
// goroutine) and stash results; the TestXxx functions only assert. The macOS CI
// runner has a window server, so no virtual display is needed.

var (
	resBridge      atomic.Value // string
	resErrorUnbind atomic.Value // string
	resRichTypes   atomic.Value // string
	resMultiWindow atomic.Value // string
	resEmbed       atomic.Value // string
	resOpenPanel   atomic.Value // string
)

func TestMain(m *testing.M) {
	runtime.LockOSThread()
	resBridge.Store(bridgeScenario())
	resErrorUnbind.Store(errorUnbindScenario())
	resRichTypes.Store(richTypesScenario())
	resMultiWindow.Store(multiWindowScenario())
	resEmbed.Store(embedScenario())
	resOpenPanel.Store(openPanelCompletionScenario())
	os.Exit(m.Run())
}

// openPanelCompletionScenario exercises the WKUIDelegate file-chooser
// completion path (invokeOpenPanelCompletion / NSInvocation "v@?@") without
// presenting the modal panel, by invoking it with a Go block and a cancelled
// (nil) selection. The full panel UI is exercised manually via
// examples/filepicker.
func openPanelCompletionScenario() string {
	done := make(chan objc.ID, 1)
	block := objc.NewBlock(func(_ objc.Block, urls objc.ID) {
		select {
		case done <- urls:
		default:
		}
	})
	invokeOpenPanelCompletion(objc.ID(uintptr(block)), 0)
	select {
	case urls := <-done:
		if urls != 0 {
			return "urls=nonnil (want nil for cancel)"
		}
		return "panel-ok"
	case <-time.After(2 * time.Second):
		return "completion handler not invoked"
	}
}

// multiWindowScenario verifies window ref-count bookkeeping across two engines
// and that full Destroy returns the count to its baseline (no run loop needed).
func multiWindowScenario() string {
	start := atomic.LoadInt32(&windowCount)
	w1, err := New(false)
	if err != nil {
		return "w1 error: " + err.Error()
	}
	w2, err := NewWindow(false, nil)
	if err != nil {
		return "w2 error: " + err.Error()
	}
	peak := atomic.LoadInt32(&windowCount)
	w1.Destroy()
	w2.Destroy()
	end := atomic.LoadInt32(&windowCount)
	return strconv.Itoa(int(start)) + "->" + strconv.Itoa(int(peak)) + "->" + strconv.Itoa(int(end))
}

// embedScenario embeds a web view into a caller-provided NSWindow and verifies
// the engine does not take ownership and Destroy leaves the host window intact.
func embedScenario() string {
	host := class("NSWindow").Send(sel("alloc"))
	host = host.Send(sel("initWithContentRect:styleMask:backing:defer:"),
		cgRect{cgPoint{0, 0}, cgSize{400, 300}},
		uint(nsWindowStyleMaskTitled), nsBackingStoreBuffered, false)
	host = host.Send(sel("retain"))

	hostPtr := *(*unsafe.Pointer)(unsafe.Pointer(&host)) // objc.ID -> unsafe.Pointer
	w, err := NewWindow(false, hostPtr)
	if err != nil {
		return "new error: " + err.Error()
	}
	owns := w.(*webview).ownsWindow // concrete type (same package)
	w.Destroy()

	// Host must still be alive after Destroy (this would crash on a released
	// object), then tear it down ourselves.
	host.Send(sel("setTitle:"), nsstr("still alive"))
	host.Send(sel("close"))
	host.Send(sel("release"))

	if owns {
		return "owns=true (BUG: should not own external window)"
	}
	return "embed-ok"
}

func TestMultiWindowRefCount(t *testing.T) {
	const want = "0->2->0"
	if got, _ := resMultiWindow.Load().(string); got != want {
		t.Fatalf("window ref-count = %q, want %q", got, want)
	}
}

func TestEmbedExternalWindow(t *testing.T) {
	if got, _ := resEmbed.Load().(string); got != "embed-ok" {
		t.Fatalf("embed external window = %q, want %q", got, "embed-ok")
	}
}

func TestOpenPanelCompletion(t *testing.T) {
	if got, _ := resOpenPanel.Load().(string); got != "panel-ok" {
		t.Fatalf("open-panel completion = %q, want %q", got, "panel-ok")
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

func TestBridge(t *testing.T) {
	if got, _ := resBridge.Load().(string); got != "42|hi x" {
		t.Fatalf("JS<->Go bridge = %q, want %q", got, "42|hi x")
	}
}

func TestErrorAndUnbind(t *testing.T) {
	const want = "temp=undefined boom=kaboom"
	if got, _ := resErrorUnbind.Load().(string); got != want {
		t.Fatalf("error/unbind = %q, want %q", got, want)
	}
}

func TestRichBindingTypes(t *testing.T) {
	const want = "p=2,3 s=10"
	if got, _ := resRichTypes.Load().(string); got != want {
		t.Fatalf("rich types = %q, want %q", got, want)
	}
}
