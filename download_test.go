//go:build darwin || linux

package glaze

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var resDownload atomic.Value // string

// downloadScenario saves an attachment over an existing file, saves a link
// with the download attribute, and refuses a third download: nothing may be
// written for it and no completion reported.
func downloadScenario() string {
	dir, err := os.MkdirTemp("", "glaze-download")
	if err != nil {
		return err.Error()
	}
	defer func() { _ = os.RemoveAll(dir) }()
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/attachment", "/refused":
			rw.Header().Set("Content-Disposition", `attachment; filename="note.txt"`)
			rw.Header().Set("Content-Type", "text/plain")
			_, _ = fmt.Fprint(rw, "from "+r.URL.Path)
		case "/page":
			_, _ = fmt.Fprint(rw, `<html><body><a id=a download="linked.txt" href="/raw">x</a></body></html>`)
		case "/raw":
			rw.Header().Set("Content-Type", "text/plain")
			_, _ = fmt.Fprint(rw, "from /raw")
		}
	}))
	defer srv.Close()

	var w WebView
	var got []string
	step := 0
	next := func() {
		step++
		switch step {
		case 1:
			w.Navigate(srv.URL + "/page")
		case 2:
			w.Navigate(srv.URL + "/refused")
			time.AfterFunc(1500*time.Millisecond, func() { w.Dispatch(w.Terminate) })
		}
	}
	existing := filepath.Join(dir, "note.txt")
	_ = os.WriteFile(existing, []byte("old"), 0o600)
	w, err = NewWithOptions(Options{
		OnNavigation: func(ev NavigationEvent) {
			if ev.Kind == NavigationFinished && strings.HasSuffix(ev.URL, "/page") {
				w.Eval(`document.getElementById("a").click()`)
			}
		},
		OnDownload: func(name string) string {
			got = append(got, "offer "+name)
			if step == 2 {
				return ""
			}
			return filepath.Join(dir, name)
		},
		OnDownloadDone: func(path string, err error) {
			b, _ := os.ReadFile(path) // #nosec G304 -- test temp dir
			got = append(got, fmt.Sprintf("saved %s=%q err=%v", filepath.Base(path), b, err))
			next()
		},
	})
	if err != nil {
		return "new error: " + err.Error()
	}
	defer w.Destroy()
	var timedOut atomic.Bool
	defer time.AfterFunc(20*time.Second, func() { timedOut.Store(true); w.Terminate() }).Stop()
	w.Navigate(srv.URL + "/attachment")
	w.Run()
	entries, _ := os.ReadDir(dir)
	got = append(got, fmt.Sprintf("files=%d", len(entries)))
	if timedOut.Load() {
		got = append(got, "TIMEOUT")
	}
	return strings.Join(got, " | ")
}

func TestDownloads(t *testing.T) {
	got, _ := resDownload.Load().(string)
	requireGUI(t, got)
	want := `offer note.txt | saved note.txt="from /attachment" err=<nil>` +
		` | offer linked.txt | saved linked.txt="from /raw" err=<nil>` +
		` | offer note.txt | files=2`
	if got != want {
		t.Fatalf("downloads: got %q, want %q", got, want)
	}
}
