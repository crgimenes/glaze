//go:build darwin || linux

package glaze

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var resFind atomic.Value // string

// findScenario searches a loaded page: a word that is there, the same word
// again (next match), one that is not, and a word searched backwards.
func findScenario() string {
	var w WebView
	var got []string
	steps := []struct {
		text      string
		backwards bool
	}{{"alpha", false}, {"alpha", false}, {"zzzz", false}, {"gamma", true}}
	var next func()
	next = func() {
		if len(steps) == 0 {
			w.Terminate()
			return
		}
		s := steps[0]
		steps = steps[1:]
		w.Find(s.text, s.backwards, func(found bool) {
			got = append(got, fmt.Sprintf("%s=%v", s.text, found))
			next()
		})
	}
	page := "<html><body><p>alpha beta</p><p>alpha gamma</p></body></html>"
	w, err := NewWithOptions(Options{
		SchemeHandlers: map[string]SchemeHandler{"find": func(*SchemeRequest) *SchemeResponse {
			return &SchemeResponse{Body: []byte(page), MIMEType: "text/html"}
		}},
		OnNavigation: func(ev NavigationEvent) { next() },
	})
	if err != nil {
		return "new error: " + err.Error()
	}
	defer w.Destroy()
	var timedOut atomic.Bool
	defer time.AfterFunc(15*time.Second, func() { timedOut.Store(true); w.Terminate() }).Stop()
	w.Navigate("find://test/")
	w.Run()
	if timedOut.Load() {
		got = append(got, "TIMEOUT")
	}
	return strings.Join(got, " ")
}

func TestFind(t *testing.T) {
	got, _ := resFind.Load().(string)
	requireGUI(t, got)
	want := "alpha=true alpha=true zzzz=false gamma=true"
	if got != want {
		t.Fatalf("find in page: got %q, want %q", got, want)
	}
}
