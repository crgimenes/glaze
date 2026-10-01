//go:build darwin || linux

package glaze

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var resHistory atomic.Value // string

// historyScenario walks a, b, back, forward, reload, then a load that Stop
// interrupts (a cancellation: never reported) and c, recording each finished
// page by its path.
func historyScenario() string {
	quit := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		select {
		case <-quit:
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()
	defer close(quit)

	var w WebView
	var got []string
	next := []func(){
		func() { w.Navigate("hist://test/b") },
		func() { w.GoBack() },
		func() { w.GoForward() },
		func() { w.Reload() },
		func() {
			w.Navigate(slow.URL + "/")
			time.AfterFunc(300*time.Millisecond, func() { w.Dispatch(w.Stop) })
			time.AfterFunc(600*time.Millisecond, func() { w.Dispatch(func() { w.Navigate("hist://test/c") }) })
		},
		func() { w.Terminate() },
	}
	onNav := func(ev NavigationEvent) {
		if ev.Kind == NavigationFailed {
			got = append(got, "failed "+ev.URL)
			w.Terminate()
			return
		}
		got = append(got, strings.TrimPrefix(ev.URL, "hist://test/"))
		if len(next) > 0 {
			step := next[0]
			next = next[1:]
			step()
		}
	}
	page := func(r *SchemeRequest) *SchemeResponse {
		return &SchemeResponse{Body: []byte("<html><body>" + r.URL + "</body></html>"), MIMEType: "text/html"}
	}
	w, err := NewWithOptions(Options{
		OnNavigation:   onNav,
		SchemeHandlers: map[string]SchemeHandler{"hist": page},
	})
	if err != nil {
		return "new error: " + err.Error()
	}
	defer w.Destroy()
	var timedOut atomic.Bool
	defer time.AfterFunc(15*time.Second, func() { timedOut.Store(true); w.Terminate() }).Stop()
	w.Navigate("hist://test/a")
	w.Run()
	if timedOut.Load() {
		got = append(got, "TIMEOUT")
	}
	return strings.Join(got, " ")
}

func TestHistoryNavigation(t *testing.T) {
	got, _ := resHistory.Load().(string)
	requireGUI(t, got)
	want := "a b a b b c"
	if got != want {
		t.Fatalf("history walk: got %q, want %q (a, b, back, forward, reload, stopped load, c)", got, want)
	}
}
