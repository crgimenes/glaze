package webview_test

import (
	"runtime"
	"testing"
	"time"

	"github.com/abemedia/go-webview"
	_ "github.com/abemedia/go-webview/embedded"
)

// Needed to ensure that the tests run on the main thread.
func init() {
	runtime.UnlockOSThread()
}

func TestWebview(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	run := make(chan bool, 1)

	w := webview.New(true)
	w.SetTitle("Hello")
	w.SetSize(800, 600, webview.HintNone)

	err := w.Bind("run", func(b bool) {
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
