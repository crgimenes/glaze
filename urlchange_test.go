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
// loads another page, reporting OnURLChange and OnNavigation in order. Each
// load's URL comes at its commit, before its finish.
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
	want := "url /a | load /a failed=false | url /b | url /c | url /c#s | url /c | url /d | load /d failed=false"
	if got != want {
		t.Fatalf("OnURLChange:\n got %s\nwant %s", got, want)
	}
}

var resURLCommit atomic.Value // string

// urlCommitScenario checks that OnURLChange brings a navigation's URL at its
// commit: the first page (reached through a redirect) holds an image until
// OnURLChange has come, so the URL must arrive before the finish or the
// scenario times out. Two navigations that fail before committing (refused,
// TLS) must not change the URL; a last one does.
func urlCommitScenario() string {
	refused, err := closedPort()
	if err != nil {
		return "closed port: " + err.Error()
	}
	release := make(chan struct{})
	var released atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(rw, r, "/page", http.StatusFound)
		case "/held.png":
			select {
			case <-release:
			case <-time.After(10 * time.Second):
			}
			http.NotFound(rw, r)
		default:
			_, _ = fmt.Fprint(rw, `<!DOCTYPE html><html><body>text<img src="/held.png"></body></html>`)
		}
	}))
	defer srv.Close()
	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(rw, "must not load")
	}))
	defer tlsSrv.Close()
	name := strings.NewReplacer(srv.URL, "SRV", tlsSrv.URL, "TLS", refused, "REFUSED").Replace

	var got []string
	finished := false
	var w WebView
	w, err = NewWithOptions(Options{
		NoBridge:          true,
		HideUntilLoaded:   true,
		OnNavigationStart: func(url string) { got = append(got, "start "+name(url)) },
		OnContentShown:    func() { got = append(got, "shown") },
		OnURLChange: func(url string) {
			got = append(got, fmt.Sprintf("url %s finished=%v", name(url), finished))
			if released.CompareAndSwap(false, true) {
				close(release)
			}
		},
		OnNavigation: func(ev NavigationEvent) {
			finished = true
			kind := "load"
			if ev.Kind == NavigationFailed {
				kind = "fail"
			}
			got = append(got, kind+" "+name(ev.URL))
			switch {
			case strings.HasSuffix(ev.URL, "/page"):
				w.Navigate(refused + "/")
			case strings.HasPrefix(ev.URL, refused):
				w.Navigate(tlsSrv.URL + "/")
			case strings.HasPrefix(ev.URL, tlsSrv.URL):
				finished = false
				w.Navigate(srv.URL + "/last")
			case strings.HasSuffix(ev.URL, "/last"):
				w.Terminate()
			}
		},
	})
	if err != nil {
		return "new error: " + err.Error()
	}
	defer w.Destroy()
	var timedOut atomic.Bool
	defer time.AfterFunc(20*time.Second, func() { timedOut.Store(true); w.Terminate() }).Stop()
	w.Navigate(srv.URL + "/start")
	w.Run()
	if timedOut.Load() {
		got = append(got, "TIMEOUT")
	}
	return strings.Join(got, " | ")
}

func TestOnURLChangeAtCommit(t *testing.T) {
	got, _ := resURLCommit.Load().(string)
	requireGUI(t, got)
	want := "start SRV/start | url SRV/page finished=false | shown | load SRV/page" +
		" | start REFUSED/ | fail REFUSED/" +
		" | start TLS/ | fail TLS/" +
		" | start SRV/last | url SRV/last finished=false | load SRV/last"
	if got != want {
		t.Fatalf("OnURLChange at commit:\n got %s\nwant %s", got, want)
	}
}
