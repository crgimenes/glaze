//go:build windows

package glaze

import (
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// The WebView2/COM backend runs on one OS thread, so the GUI scenario runs in
// TestMain (the main goroutine) and stashes its result; TestBridge asserts.
// Needs the Edge WebView2 Runtime (present on windows-latest CI).

var resWinBridge atomic.Value // string

func TestMain(m *testing.M) {
	runtime.LockOSThread()
	resWinBridge.Store(winBridgeScenario())
	os.Exit(m.Run())
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
	time.AfterFunc(60*time.Second, w.Terminate) // watchdog

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

func TestBridge(t *testing.T) {
	if got, _ := resWinBridge.Load().(string); got != "42|hi x" {
		t.Fatalf("JS<->Go bridge = %q, want %q", got, "42|hi x")
	}
}
