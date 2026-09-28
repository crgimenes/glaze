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
	open := func(h SchemeHandler) string {
		var w WebView
		result := "no navigation"
		w, err := NewWithOptions(Options{
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
	first := open(handlerA)
	second := open(handlerB)
	return first + " by " + *byA + ", " + second + " by " + *byB
}

func TestSchemeReuseAcrossWebViews(t *testing.T) {
	got, _ := resSchemeReuse.Load().(string)
	requireGUI(t, got)
	want := "finished by A, finished by B"
	if got != want {
		t.Fatalf("same scheme in two web views: got %q, want %q", got, want)
	}
}
