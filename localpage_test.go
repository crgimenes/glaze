//go:build darwin || linux

package glaze

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var resLocalPage atomic.Value // string

// localPageScenario opens a file: page with one stylesheet beside it and one
// in another directory, has it read a file outside its directory, then has
// an http page try to move to the local one; the pages report through an
// http beacon.
func localPageScenario() string {
	reports := make(chan string, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/r" {
			reports <- r.URL.Query().Get("v")
			return
		}
		_, _ = fmt.Fprint(rw, `<!DOCTYPE html><html><body><script>
location.href = "`+r.URL.Query().Get("to")+`";
setTimeout(function() { new Image().src = "/r?v=after%3D" + location.protocol; }, 1000);
</script></body></html>`)
	}))
	defer srv.Close()

	dir, err := os.MkdirTemp("", "glaze_local")
	if err != nil {
		return "tempdir: " + err.Error()
	}
	defer func() { _ = os.RemoveAll(dir) }()
	outside, err := os.MkdirTemp("", "glaze_outside")
	if err != nil {
		return "tempdir: " + err.Error()
	}
	defer func() { _ = os.RemoveAll(outside) }()
	err = os.WriteFile(filepath.Join(outside, "outside.css"), []byte("body { color: rgb(4, 5, 6) }"), 0o600)
	if err != nil {
		return "write: " + err.Error()
	}
	page := filepath.Join(dir, "index.html")
	html := `<!DOCTYPE html><html><head><link rel="stylesheet" href="style.css">
<link rel="stylesheet" href="file://` + filepath.Join(outside, "outside.css") + `"></head><body><script>
function report(v) { new Image().src = "` + srv.URL + `/r?v=" + encodeURIComponent(v); }
window.onload = function() {
	var cs = getComputedStyle(document.body);
	report("bg=" + cs.backgroundColor + " fg=" + cs.color);
	fetch("file:///etc/hosts").then(function(r) { return r.text(); }).then(
		function(t) { report("outside=read"); }, function(e) { report("outside=blocked"); });
};
</script></body></html>`
	err = os.WriteFile(page, []byte(html), 0o600)
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, "style.css"), []byte("body { background: rgb(1, 2, 3) }"), 0o600)
	}
	if err != nil {
		return "write: " + err.Error()
	}
	fileURL := "file://" + page

	var w WebView
	w, err = NewWithOptions(Options{NoBridge: true})
	if err != nil {
		return "new error: " + err.Error()
	}
	defer w.Destroy()
	var got []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		timeout := time.After(15 * time.Second)
		for len(got) < 3 {
			select {
			case v := <-reports:
				got = append(got, v)
				if len(got) == 2 {
					w.Dispatch(func() { w.Navigate(srv.URL + "/remote?to=" + fileURL) })
				}
			case <-timeout:
				got = append(got, "TIMEOUT")
				w.Dispatch(w.Terminate)
				return
			}
		}
		w.Dispatch(w.Terminate)
	}()
	w.Navigate(fileURL)
	w.Run()
	<-done
	// The first two reports are separate requests, and the network process
	// sends them in the order it likes; only the third is caused by the second.
	if len(got) >= 2 {
		slices.Sort(got[:2])
	}
	return strings.Join(got, " | ")
}

// A local page may use what sits beside it; a web page cannot reach a local
// one. WKWebView keeps the page to its own directory (an app sandboxed or
// not); WebKitGTK has no such scope, so there the other stylesheet loads.
func TestLocalPage(t *testing.T) {
	got, _ := resLocalPage.Load().(string)
	requireGUI(t, got)
	fg := "rgb(0, 0, 0)"
	if runtime.GOOS == "linux" {
		fg = "rgb(4, 5, 6)"
	}
	want := "bg=rgb(1, 2, 3) fg=" + fg + " | outside=blocked | after=http:"
	if got != want {
		t.Fatalf("local page: got %q, want %q", got, want)
	}
}
