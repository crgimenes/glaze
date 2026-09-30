//go:build darwin || linux

package glaze

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var resNavigation atomic.Value // string

// navigationScenario drives four navigations: one replaced while it waits for
// the server (its start is reported, its end is not), one that finishes, one
// refused (plain failure) and one to a self-signed https server (TLS failure).
func navigationScenario() string {
	refused, err := closedPort()
	if err != nil {
		return "closed port: " + err.Error()
	}
	quit := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		select {
		case <-quit:
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()
	defer close(quit)
	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(rw, "must not load")
	}))
	defer tlsSrv.Close()

	var w WebView
	var got, starts []string
	name := func(url string) string {
		return strings.NewReplacer(tlsSrv.URL, "TLSSERVER", refused, "REFUSED", slow.URL, "SLOW").Replace(url)
	}
	onNav := func(ev NavigationEvent) {
		kind := "finished"
		if ev.Kind == NavigationFailed {
			kind = fmt.Sprintf("failed err=%v", ev.Err != nil && ev.Err.Error() != "")
		}
		got = append(got, fmt.Sprintf("%s %s tls=%v", kind, name(ev.URL), ev.TLS))
		switch len(got) {
		case 1:
			w.Navigate(refused + "/")
		case 2:
			w.Navigate(tlsSrv.URL + "/")
		default:
			w.Terminate()
		}
	}
	page := func(*SchemeRequest) *SchemeResponse {
		return &SchemeResponse{Body: []byte("<html><body>ok</body></html>"), MIMEType: "text/html"}
	}
	w, err = NewWithOptions(Options{
		NoBridge:          true,
		OnNavigation:      onNav,
		OnNavigationStart: func(url string) { starts = append(starts, name(url)) },
		SchemeHandlers:    map[string]SchemeHandler{"probe": page},
	})
	if err != nil {
		return "new error: " + err.Error()
	}
	defer w.Destroy()
	var timedOut atomic.Bool
	time.AfterFunc(15*time.Second, func() { timedOut.Store(true); w.Terminate() })
	w.Navigate(slow.URL + "/")
	time.AfterFunc(500*time.Millisecond, func() { w.Dispatch(func() { w.Navigate("probe://test/ok") }) })
	w.Run()
	if timedOut.Load() {
		got = append(got, "TIMEOUT")
	}
	return strings.Join(got, " | ") + " || starts " + strings.Join(starts, " ")
}

// closedPort returns http://127.0.0.1:<port> for a port that was just free:
// connecting is refused, on every WebKit, unlike a port WebKit blocks by policy.
func closedPort() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	addr := l.Addr().String()
	err = l.Close()
	return "http://" + addr, err
}

func TestOnNavigation(t *testing.T) {
	got, _ := resNavigation.Load().(string)
	requireGUI(t, got)
	want := "finished probe://test/ok tls=false" +
		" | failed err=true REFUSED/ tls=false" +
		" | failed err=true TLSSERVER/ tls=true" +
		" || starts SLOW/ probe://test/ok REFUSED/ TLSSERVER/"
	if got != want {
		t.Fatalf("OnNavigation events: got %q, want %q", got, want)
	}
}
