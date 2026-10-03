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

var resMediaOrigin atomic.Value // string

// mediaOriginScenario checks whose origin a capture request carries and who
// learns the devices' names. Top (127.0.0.1) embeds a frame from another
// origin (localhost) that asks for the camera and microphone, once delegated
// with allow= and once not; then top and the other origin each list the
// devices, after OnMediaCapture granted only top.
func mediaOriginScenario() string {
	results := make(chan string, 8)
	var top, other *httptest.Server
	handler := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		report := `function report(v) { new Image().src = "/r?v=" + encodeURIComponent(v); }`
		switch r.URL.Path {
		case "/r":
			results <- r.URL.Query().Get("v")
		case "/deleg", "/nodeleg":
			allow := ""
			if r.URL.Path == "/deleg" {
				allow = ` allow="camera; microphone"`
			}
			_, _ = fmt.Fprintf(rw, `<!DOCTYPE html><iframe%s src="%s/gum"></iframe>`, allow, other.URL)
		case "/gum":
			_, _ = fmt.Fprintf(rw, `<!DOCTYPE html><script>%s
if (!navigator.mediaDevices) report("no-mediadevices");
else navigator.mediaDevices.getUserMedia({audio: true, video: true}).then(
  function(s) { report("tracks=" + s.getTracks().length); },
  function(e) { report(e.name); });
</script>`, report)
		case "/devices":
			_, _ = fmt.Fprintf(rw, `<!DOCTYPE html><script>%s
navigator.mediaDevices.enumerateDevices().then(function(ds) {
  report("named=" + ds.filter(function(d) { return d.label !== ""; }).length);
}, function(e) { report(e.name); });
</script>`, report)
		}
	})
	top = httptest.NewServer(handler)
	defer top.Close()
	other = httptest.NewUnstartedServer(handler)
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return "listen: " + err.Error()
	}
	other.Listener = l
	other.Start()
	defer other.Close()
	_, port, _ := net.SplitHostPort(l.Addr().String())
	otherURL := "http://localhost:" + port
	other.URL = otherURL

	var asked []string
	name := strings.NewReplacer(top.URL, "TOP", otherURL, "OTHER").Replace
	w, err := NewWithOptions(Options{NoBridge: true, OnMediaCapture: func(origin string, camera, microphone bool) bool {
		asked = append(asked, name(origin))
		return true
	}})
	if err != nil {
		return "new error: " + err.Error()
	}
	defer w.Destroy()
	enableMockCapture(w.(*webview))

	var out []string
	steps := []string{top.URL + "/deleg", top.URL + "/nodeleg", top.URL + "/devices", otherURL + "/devices"}
	go func() {
		defer w.Dispatch(w.Terminate)
		for _, u := range steps {
			w.Dispatch(func() { w.Navigate(u) })
			v := "TIMEOUT"
			select {
			case v = <-results:
			case <-time.After(10 * time.Second):
			}
			out = append(out, v)
			if v == "TIMEOUT" || v == "no-mediadevices" {
				return
			}
		}
	}()
	w.Run()
	return fmt.Sprintf("asked=[%s] page=[%s]", strings.Join(asked, " "), strings.Join(out, " "))
}

func TestMediaOrigin(t *testing.T) {
	got, _ := resMediaOrigin.Load().(string)
	requireGUI(t, got)
	if strings.Contains(got, "no-mediadevices") {
		t.Skip("navigator.mediaDevices absent: the test binary has no camera/microphone usage descriptions")
	}
	// The delegated frame is asked for under top's origin; the undelegated
	// one is denied by the engine before OnMediaCapture; the other origin
	// learns no device name from top's grant.
	want := "asked=[TOP] page=[tracks=2 NotAllowedError named="
	if !strings.HasPrefix(got, want) || !strings.HasSuffix(got, " named=0]") {
		t.Fatalf("media origin:\n got %s\nwant %s<n> named=0]", got, want)
	}
}
