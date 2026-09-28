//go:build darwin || linux

package glaze

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

var resHideUntilLoaded atomic.Value // string

func hideUntilLoadedScenario() string {
	v, err := NewWithOptions(Options{HideUntilLoaded: true})
	if err != nil {
		return "new error: " + err.Error()
	}
	defer v.Destroy()
	w := v.(*webview)
	hiddenAtStart := w.contentHidden
	revealed := false

	stop := make(chan struct{})
	defer close(stop)
	go func() {
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				w.Dispatch(func() {
					if !w.contentHidden {
						revealed = true
						w.Terminate()
					}
				})
			}
		}
	}()
	time.AfterFunc(15*time.Second, w.Terminate)
	w.SetHtml(`<!DOCTYPE html><html><body>loaded</body></html>`)
	w.Run()
	return fmt.Sprintf("hidden at start=%v, revealed after load=%v", hiddenAtStart, revealed)
}

func TestHideUntilLoaded(t *testing.T) {
	got, _ := resHideUntilLoaded.Load().(string)
	requireGUI(t, got)
	want := "hidden at start=true, revealed after load=true"
	if got != want {
		t.Fatalf("HideUntilLoaded: %s, want %s", got, want)
	}
}
