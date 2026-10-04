//go:build darwin || linux

package glaze

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

var (
	resHideUntilLoaded atomic.Value // string
	resRevealTiming    atomic.Value // string
	resSpinner         atomic.Value // string
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
	defer time.AfterFunc(15*time.Second, w.Terminate).Stop()
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
	visible := true
	v, err := NewWithOptions(Options{HideUntilLoaded: true, OnNavigation: func(ev NavigationEvent) {
		finishedAt = time.Now()
		visible = windowVisible(w)
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
	defer time.AfterFunc(15*time.Second, w.Terminate).Stop()
	w.Navigate(srv.URL)
	w.Run()
	if revealedAt.IsZero() || finishedAt.IsZero() {
		return fmt.Sprintf("revealed=%v finished=%v", !revealedAt.IsZero(), !finishedAt.IsZero())
	}
	early := finishedAt.Sub(revealedAt) > time.Second
	if !early && !visible {
		return "window not visible"
	}
	return fmt.Sprintf("shown before finish=%v", early)
}

// Linux shows the page when it commits, macOS at its first paint: both long
// before a slow image lets the navigation finish.
func TestRevealTiming(t *testing.T) {
	got, _ := resRevealTiming.Load().(string)
	requireGUI(t, got)
	if got == "window not visible" {
		// macOS pauses an occluded window's animation frames, which the
		// first-paint reveal waits on; a CI runner without an active display
		// can leave the window so.
		t.Skip("the window was not visible on screen")
	}
	want := "shown before finish=true"
	if got != want {
		t.Fatalf("reveal timing: %s, want %s", got, want)
	}
}

// spinnerScenario loads a page that takes 1.2 s to answer and reports whether
// the spinner showed while the view was held back and was gone once shown.
func spinnerScenario() string {
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		time.Sleep(1200 * time.Millisecond)
		_, _ = fmt.Fprint(rw, `<!DOCTYPE html><html><body>slow</body></html>`)
	}))
	defer srv.Close()

	var w *webview
	shown, gone := false, false
	v, err := NewWithOptions(Options{HideUntilLoaded: true, OnNavigation: func(ev NavigationEvent) {
		gone = w.spinner == 0
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
					if w.contentHidden && spinnerVisible(w) {
						shown = true
					}
				})
			}
		}
	}()
	defer time.AfterFunc(15*time.Second, w.Terminate).Stop()
	w.Navigate(srv.URL)
	w.Run()
	return fmt.Sprintf("spinner shown=%v, gone after=%v", shown, gone)
}

func TestSpinnerWhileHeldBack(t *testing.T) {
	got, _ := resSpinner.Load().(string)
	requireGUI(t, got)
	want := "spinner shown=true, gone after=true"
	if got != want {
		t.Fatalf("HideUntilLoaded spinner: %s, want %s", got, want)
	}
}
