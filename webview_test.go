//go:build integration

package webview_test

import (
	"runtime"
	"testing"
	"time"

	"github.com/crgimenes/go-webview"
	_ "github.com/crgimenes/go-webview/embedded"
)

// init unlocks the OS thread that the webview package locks during its own
// init(). Tests manage thread locking explicitly per test function.
// This is a justified exception to the "no init() side effects" guideline.
func init() {
	runtime.UnlockOSThread()
}

func TestWebview(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	run := make(chan bool, 1)

	w, err := webview.New(true)
	if err != nil {
		t.Fatal(err)
	}

	w.SetTitle("Hello")
	w.SetSize(800, 600, webview.HintNone)

	err = w.Bind("run", func(b bool) {
		run <- b
		w.Terminate()
	})
	if err != nil {
		t.Fatal(err)
	}

	w.SetHtml(`<!doctype html>
		<html>
			<script>
				window.onload = function() { run(true); };
			</script>
		</html>`)

	w.Run()
	w.Destroy()

	select {
	case ok := <-run:
		if !ok {
			t.Fatal("run failed")
		}
	case <-time.After(time.Minute):
		t.Fatal("timeout")
	}
}
