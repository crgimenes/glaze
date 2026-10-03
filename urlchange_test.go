//go:build darwin || linux

package glaze

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var resURLChange atomic.Value // string

// urlChangeScenario loads a page that moves its URL without loading --
// pushState, replaceState, a fragment, back within the document -- and then
// loads another page, reporting OnURLChange and OnNavigation in order.
func urlChangeScenario() string {
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/a" {
			_, _ = fmt.Fprint(rw, `<!DOCTYPE html><html><body>d</body></html>`)
			return
		}
		_, _ = fmt.Fprint(rw, `<!DOCTYPE html><html><body><script>
var steps = [
  function() { history.pushState({}, "", "/b"); },
  function() { history.replaceState({}, "", "/c"); },
  function() { location.hash = "s"; },
  function() { history.back(); },
  function() { location.href = "/d"; },
];
function next() { var f = steps.shift(); if (f) { f(); setTimeout(next, 300); } }
addEventListener("load", function() { setTimeout(next, 300); });
</script></body></html>`)
	}))
	defer srv.Close()

	var got []string
	var w WebView
	w, err := NewWithOptions(Options{
		NoBridge:    true,
		OnURLChange: func(url string) { got = append(got, "url "+strings.TrimPrefix(url, srv.URL)) },
		OnNavigation: func(ev NavigationEvent) {
			path := strings.TrimPrefix(ev.URL, srv.URL)
			got = append(got, fmt.Sprintf("load %s failed=%v", path, ev.Kind == NavigationFailed))
			if path == "/d" {
				w.Terminate()
			}
		},
	})
	if err != nil {
		return "new error: " + err.Error()
	}
	defer w.Destroy()
	var timedOut atomic.Bool
	defer time.AfterFunc(15*time.Second, func() { timedOut.Store(true); w.Terminate() }).Stop()
	w.Navigate(srv.URL + "/a")
	w.Run()
	if timedOut.Load() {
		got = append(got, "TIMEOUT")
	}
	return strings.Join(got, " | ")
}

func TestOnURLChange(t *testing.T) {
	got, _ := resURLChange.Load().(string)
	requireGUI(t, got)
	want := "load /a failed=false | url /b | url /c | url /c#s | url /c | load /d failed=false"
	if got != want {
		t.Fatalf("OnURLChange:\n got %s\nwant %s", got, want)
	}
}
