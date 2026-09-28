//go:build darwin || linux || windows

package glaze

import (
	"errors"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var resNoBridge atomic.Value // string

// noBridgeScenario loads a page through a scheme handler (the only way back
// to Go once the bridge is gone) and has it report what it can reach. Init
// must still run, since it shares the document-start path with the bridge.
func noBridgeScenario() string {
	var w WebView
	report := make(chan string, 1)
	page := `<!DOCTYPE html><html><body><script>
var r = [
  typeof window.__webview__,
  typeof (window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.__webview__),
  typeof window.__probe__
].join(',');
new Image().src = 'report?' + encodeURIComponent(r);
</script></body></html>`
	handler := func(req *SchemeRequest) *SchemeResponse {
		_, query, found := strings.Cut(req.URL, "report?")
		if !found {
			return &SchemeResponse{Body: []byte(page), MIMEType: "text/html"}
		}
		got, err := url.QueryUnescape(query)
		if err != nil {
			got = "bad report: " + err.Error()
		}
		select {
		case report <- got:
		default:
		}
		w.Terminate()
		return &SchemeResponse{}
	}

	w, err := NewWithOptions(Options{
		NoBridge:       true,
		SchemeHandlers: map[string]SchemeHandler{"probe": handler},
	})
	if err != nil {
		return "new error: " + err.Error()
	}
	defer w.Destroy()

	err = w.Bind("f", func() {})
	if !errors.Is(err, ErrBridgeDisabled) {
		return "Bind error = " + errString(err)
	}
	err = w.Unbind("f")
	if !errors.Is(err, ErrBridgeDisabled) {
		return "Unbind error = " + errString(err)
	}

	w.Init("window.__probe__ = 1;")
	time.AfterFunc(15*time.Second, w.Terminate)
	w.Navigate("probe://test/index.html")
	w.Run()

	select {
	case r := <-report:
		return r
	default:
		return "no report"
	}
}

func errString(err error) string {
	if err == nil {
		return "nil"
	}
	return err.Error()
}

func TestNoBridge(t *testing.T) {
	got, _ := resNoBridge.Load().(string)
	requireGUI(t, got)
	want := "undefined,undefined,number"
	if got != want {
		t.Fatalf("NoBridge page sees %q, want %q (__webview__, message handler, Init probe)", got, want)
	}
}
