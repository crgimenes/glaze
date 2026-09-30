//go:build darwin || linux

package glaze

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

var (
	resHideUntilLoaded atomic.Value // string
	resRevealTiming    atomic.Value // string
)

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

// revealTimingScenario loads a page whose image takes 2 s, so the navigation
// commits and paints long before it finishes, and reports whether the view
// was shown more than a second before the finish.
func revealTimingScenario() string {
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow.png" {
			time.Sleep(2 * time.Second)
			http.NotFound(rw, r)
			return
		}
		_, _ = fmt.Fprint(rw, `<!DOCTYPE html><html><body>text<img src="/slow.png"></body></html>`)
	}))
	defer srv.Close()

	var w *webview
	var revealedAt, finishedAt time.Time
	v, err := NewWithOptions(Options{HideUntilLoaded: true, OnNavigation: func(ev NavigationEvent) {
		finishedAt = time.Now()
		if !w.contentHidden && revealedAt.IsZero() {
			revealedAt = finishedAt // shown by the finish itself
		}
		w.Terminate()
	}})
	if err != nil {
		return "new error: " + err.Error()
	}
	defer v.Destroy()
	w = v.(*webview)

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
					if !w.contentHidden && revealedAt.IsZero() {
						revealedAt = time.Now()
					}
				})
			}
		}
	}()
	time.AfterFunc(15*time.Second, w.Terminate)
	w.Navigate(srv.URL)
	w.Run()
	if revealedAt.IsZero() || finishedAt.IsZero() {
		return fmt.Sprintf("revealed=%v finished=%v", !revealedAt.IsZero(), !finishedAt.IsZero())
	}
	return fmt.Sprintf("shown before finish=%v", finishedAt.Sub(revealedAt) > time.Second)
}

// Linux shows the page when it commits; macOS, where WKWebView paints white
// until the first paint, waits for the finish.
func TestRevealTiming(t *testing.T) {
	got, _ := resRevealTiming.Load().(string)
	requireGUI(t, got)
	want := fmt.Sprintf("shown before finish=%v", runtime.GOOS == "linux")
	if got != want {
		t.Fatalf("reveal timing: %s, want %s", got, want)
	}
}
