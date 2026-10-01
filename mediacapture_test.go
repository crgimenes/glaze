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

var resMediaCapture atomic.Value // string

// mediaCaptureScenario loads a page that asks for the camera and microphone,
// with simulated devices, once refused and once allowed, and reports what
// OnMediaCapture saw and what the page got.
func mediaCaptureScenario() string {
	results := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/r" {
			results <- r.URL.Query().Get("v")
			return
		}
		_, _ = fmt.Fprint(rw, `<!DOCTYPE html><html><body><script>
function report(v) { new Image().src = "/r?v=" + encodeURIComponent(v); }
if (!navigator.mediaDevices) report("no-mediadevices");
else navigator.mediaDevices.getUserMedia({audio: true, video: true}).then(
  function(s) { report("tracks=" + s.getTracks().length); },
  function(e) { report(e.name); });
</script></body></html>`)
	}))
	defer srv.Close()

	var out []string
	for _, allow := range []bool{false, true} {
		var asked []string
		var w WebView
		w, err := NewWithOptions(Options{OnMediaCapture: func(origin string, camera, microphone bool) bool {
			asked = append(asked, fmt.Sprintf("origin=%v camera=%v mic=%v", origin == srv.URL, camera, microphone))
			return allow
		}})
		if err != nil {
			return "new error: " + err.Error()
		}
		enableMockCapture(w.(*webview))
		page := make(chan string, 1)
		go func() {
			v := "TIMEOUT"
			select {
			case v = <-results:
			case <-time.After(10 * time.Second):
			}
			page <- v
			w.Dispatch(w.Terminate)
		}()
		w.Navigate(srv.URL)
		w.Run()
		w.Destroy()
		got := <-page
		out = append(out, fmt.Sprintf("allow=%v asked=[%s] page=%s", allow, strings.Join(asked, ";"), got))
	}
	return strings.Join(out, " | ")
}

func TestMediaCapture(t *testing.T) {
	got, _ := resMediaCapture.Load().(string)
	requireGUI(t, got)
	if strings.Contains(got, "page=no-mediadevices") {
		// Recent macOS WebKit hides the capture API from an app without
		// NSCameraUsageDescription/NSMicrophoneUsageDescription, which a test
		// binary has no Info.plist to carry.
		t.Skip("navigator.mediaDevices absent: the test binary has no camera/microphone usage descriptions")
	}
	want := "allow=false asked=[origin=true camera=true mic=true] page=NotAllowedError" +
		" | allow=true asked=[origin=true camera=true mic=true] page=tracks=2"
	if got != want {
		t.Fatalf("media capture:\n got %s\nwant %s", got, want)
	}
}
