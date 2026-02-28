//go:build integration

package glaze_test

import (
	"runtime"
	"testing"
	"time"

	"github.com/crgimenes/glaze"
	_ "github.com/crgimenes/glaze/embedded"
)

func TestWebview(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	run := make(chan bool, 1)

	w, err := glaze.New(true)
	if err != nil {
		t.Fatal(err)
	}

	w.SetTitle("Hello")
	w.SetSize(800, 600, glaze.HintNone)

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
