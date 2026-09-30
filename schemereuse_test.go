//go:build darwin || linux

package glaze

import (
	"sync/atomic"
	"testing"
	"time"
)

var resSchemeReuse atomic.Value // string

// schemeReuseScenario opens two web views, one after the other, that register
// the same scheme with different handlers. On Linux a scheme lives on the
// shared web context and is registered once, so the second view's requests
// must still reach the second view's handler.
func schemeReuseScenario() string {
	served := func(name string) (*string, func(*SchemeRequest) *SchemeResponse) {
		var by string
		return &by, func(*SchemeRequest) *SchemeResponse {
			by = name
			return &SchemeResponse{Body: []byte("<html><body>" + name + "</body></html>"), MIMEType: "text/html"}
		}
	}
	open := func(h SchemeHandler, ephemeral bool) string {
		var w WebView
		result := "no navigation"
		w, err := NewWithOptions(Options{
			Ephemeral:      ephemeral,
			SchemeHandlers: map[string]SchemeHandler{"dup": h},
			OnNavigation: func(ev NavigationEvent) {
				result = "finished"
				if ev.Kind == NavigationFailed {
					result = "failed: " + ev.Err.Error()
				}
				w.Terminate()
			},
		})
		if err != nil {
			return "new error: " + err.Error()
		}
		defer w.Destroy()
		time.AfterFunc(10*time.Second, w.Terminate)
		w.Navigate("dup://test/")
		w.Run()
		return result
	}

	byA, handlerA := served("A")
	byB, handlerB := served("B")
	byC, handlerC := served("C")
	first := open(handlerA, false)
	second := open(handlerB, false)
	// An ephemeral web view has a web context of its own on WebKitGTK 4.x:
	// the scheme must be registered there too.
	third := open(handlerC, true)
	return first + " by " + *byA + ", " + second + " by " + *byB + ", " + third + " by " + *byC
}

func TestSchemeReuseAcrossWebViews(t *testing.T) {
	got, _ := resSchemeReuse.Load().(string)
	requireGUI(t, got)
	want := "finished by A, finished by B, finished by C"
	if got != want {
		t.Fatalf("same scheme in two web views: got %q, want %q", got, want)
	}
}
