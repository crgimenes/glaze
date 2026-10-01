//go:build darwin || linux

package glaze

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var resEphemeral atomic.Value // string

// ephemeralScenario loads a page that sets a cookie twice in each of three
// web views -- persistent, ephemeral, ephemeral -- and records whether each
// load arrived with the cookie. Within a view the cookie comes back; an
// ephemeral view sees nothing from the persistent one or from another
// ephemeral one.
func ephemeralScenario() string {
	name := fmt.Sprintf("glaze_ephemeral_%d", time.Now().UnixNano())
	var mu sync.Mutex
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		_, err := r.Cookie(name)
		mu.Lock()
		got = append(got, fmt.Sprintf("%s=%v", strings.TrimPrefix(r.URL.Path, "/"), err == nil))
		mu.Unlock()
		http.SetCookie(rw, &http.Cookie{Name: name, Value: "kept", MaxAge: 600, Path: "/"})
		_, _ = fmt.Fprint(rw, "<html><body>cookie</body></html>")
	}))
	defer srv.Close()

	visit := func(label string, ephemeral bool) string {
		var w WebView
		loads := 0
		w, err := NewWithOptions(Options{Ephemeral: ephemeral, OnNavigation: func(ev NavigationEvent) {
			loads++
			if loads == 1 {
				w.Navigate(srv.URL + "/" + label + "-again")
				return
			}
			w.Terminate()
		}})
		if err != nil {
			return err.Error()
		}
		defer w.Destroy()
		defer time.AfterFunc(10*time.Second, w.Terminate).Stop()
		w.Navigate(srv.URL + "/" + label)
		w.Run()
		return ""
	}
	for _, v := range []struct {
		label     string
		ephemeral bool
	}{{"persistent", false}, {"ephemeral1", true}, {"ephemeral2", true}} {
		if msg := visit(v.label, v.ephemeral); msg != "" {
			return "new error: " + msg
		}
	}
	mu.Lock()
	defer mu.Unlock()
	return strings.Join(got, " ")
}

func TestEphemeral(t *testing.T) {
	got, _ := resEphemeral.Load().(string)
	requireGUI(t, got)
	want := "persistent=false persistent-again=true" +
		" ephemeral1=false ephemeral1-again=true" +
		" ephemeral2=false ephemeral2-again=true"
	if got != want {
		t.Fatalf("cookies seen: got %q, want %q", got, want)
	}
}
