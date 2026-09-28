//go:build darwin || linux

package glaze

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var resNavigation atomic.Value // string

// navigationScenario loads a page that finishes, then one that fails
// (connection refused on a port nothing listens on), and reports both events.
func navigationScenario() string {
	var w WebView
	var got []string
	onNav := func(ev NavigationEvent) {
		switch ev.Kind {
		case NavigationFinished:
			got = append(got, "finished "+ev.URL)
			w.Navigate("http://127.0.0.1:1/")
		case NavigationFailed:
			got = append(got, fmt.Sprintf("failed %s err=%v", ev.URL, ev.Err != nil && ev.Err.Error() != ""))
			w.Terminate()
		}
	}
	page := func(*SchemeRequest) *SchemeResponse {
		return &SchemeResponse{Body: []byte("<html><body>ok</body></html>"), MIMEType: "text/html"}
	}
	w, err := NewWithOptions(Options{
		NoBridge:       true,
		OnNavigation:   onNav,
		SchemeHandlers: map[string]SchemeHandler{"probe": page},
	})
	if err != nil {
		return "new error: " + err.Error()
	}
	defer w.Destroy()
	time.AfterFunc(15*time.Second, w.Terminate)
	w.Navigate("probe://test/ok")
	w.Run()
	return strings.Join(got, " | ")
}

func TestOnNavigation(t *testing.T) {
	got, _ := resNavigation.Load().(string)
	requireGUI(t, got)
	want := "finished probe://test/ok | failed http://127.0.0.1:1/ err=true"
	if got != want {
		t.Fatalf("OnNavigation events:\n got %q\nwant %q", got, want)
	}
}
