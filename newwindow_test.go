//go:build darwin || linux

package glaze

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var resNewWindow atomic.Value // string

// newWindowScenario loads a page that calls window.open on its own (a popup
// without a user gesture: the engine blocks it) and then has a target=_blank
// link clicked from Eval; only the link must reach OnNewWindow.
func newWindowScenario() string {
	var w WebView
	var got []string
	page := `<html><body>
<a id=a target=_blank href="https://example.test/linked">x</a>
<script>window.open("https://example.test/popup")</script>
</body></html>`
	w, err := NewWithOptions(Options{
		NoBridge: true,
		SchemeHandlers: map[string]SchemeHandler{"nw": func(*SchemeRequest) *SchemeResponse {
			return &SchemeResponse{Body: []byte(page), MIMEType: "text/html"}
		}},
		OnNewWindow: func(url string) { got = append(got, url) },
		OnNavigation: func(ev NavigationEvent) {
			if ev.Kind != NavigationFinished {
				got = append(got, "failed "+ev.URL)
				w.Terminate()
				return
			}
			w.Eval(`document.getElementById("a").click()`)
			time.AfterFunc(time.Second, func() { w.Dispatch(w.Terminate) })
		},
	})
	if err != nil {
		return "new error: " + err.Error()
	}
	defer w.Destroy()
	time.AfterFunc(15*time.Second, w.Terminate)
	w.Navigate("nw://test/")
	w.Run()
	return strings.Join(got, " ")
}

func TestOnNewWindow(t *testing.T) {
	got, _ := resNewWindow.Load().(string)
	requireGUI(t, got)
	want := "https://example.test/linked"
	if got != want {
		t.Fatalf("new-window requests: got %q, want %q (the popup without a gesture must be blocked)", got, want)
	}
}
