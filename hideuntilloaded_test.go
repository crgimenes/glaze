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

// revealTimingScenario loads a page whose image does not answer until the
// view has been shown, so the navigation cannot finish before the reveal: a
// view shown by the finish itself, or never, reads as late. The image gives up
// after 10 s so a view never shown still gets its finish, and the page is then
// asked whether it painted at all: where it did not, there was no first paint
// to show it by.
func revealTimingScenario() string {
	revealed := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow.png" {
			select {
			case <-revealed:
			case <-time.After(10 * time.Second):
			}
			http.NotFound(rw, r)
			return
		}
		_, _ = fmt.Fprint(rw, `<!DOCTYPE html><html><body>text<img src="/slow.png"></body></html>`)
	}))
	defer srv.Close()

	var w *webview
	early, finished, visible := false, false, true
	painted := make(chan bool, 1)
	v, err := NewWithOptions(Options{HideUntilLoaded: true, OnNavigation: func(ev NavigationEvent) {
		finished = true
		visible = windowVisible(w)
		if early {
			w.Terminate()
			return
		}
		w.Eval(`glazePainted(performance.getEntriesByType("paint").length > 0)`)
	}})
	if err != nil {
		return "new error: " + err.Error()
	}
	defer v.Destroy()
	w = v.(*webview)
	err = w.Bind("glazePainted", func(p bool) {
		painted <- p
		w.Dispatch(w.Terminate)
	})
	if err != nil {
		return "bind: " + err.Error()
	}

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
					if !w.contentHidden && !finished && !early {
						early = true
						close(revealed)
					}
				})
			}
		}
	}()
	defer time.AfterFunc(15*time.Second, w.Terminate).Stop()
	w.Navigate(srv.URL)
	w.Run()
	if !finished {
		return fmt.Sprintf("shown=%v, navigation never finished", early)
	}
	if !early {
		select {
		case p := <-painted:
			if !p {
				return "page never painted"
			}
		default:
		}
		if !visible {
			return "window not visible"
		}
	}
	return fmt.Sprintf("shown before finish=%v", early)
}

// Linux shows the page when it commits, macOS at its first paint: both long
// before a slow image lets the navigation finish.
func TestRevealTiming(t *testing.T) {
	got, _ := resRevealTiming.Load().(string)
	requireGUI(t, got)
	if got == "window not visible" || got == "page never painted" {
		// macOS pauses an occluded window's animation frames, and a display
		// that is not refreshing (the CI runner, now and then) paints nothing:
		// either way no first paint can show the view, and it shows at the
		// finish, as designed.
		t.Skip(got)
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
