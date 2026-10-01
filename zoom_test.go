//go:build darwin || linux

package glaze

import (
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var resZoom atomic.Value // string

// zoomScenario has the page report its width in CSS pixels, then zooms to 2
// and reloads: the same window holds half the CSS width.
func zoomScenario() string {
	var w WebView
	var widths []float64
	page := `<html><body><script>new Image().src = "report?w=" + window.innerWidth</script></body></html>`
	handler := func(r *SchemeRequest) *SchemeResponse {
		_, q, ok := strings.Cut(r.URL, "report?w=")
		if !ok {
			return &SchemeResponse{Body: []byte(page), MIMEType: "text/html"}
		}
		v, _ := strconv.ParseFloat(q, 64)
		widths = append(widths, v)
		if len(widths) == 1 {
			w.Dispatch(func() { w.SetZoom(2); w.Reload() })
		} else {
			w.Dispatch(w.Terminate)
		}
		return &SchemeResponse{}
	}
	w, err := NewWithOptions(Options{SchemeHandlers: map[string]SchemeHandler{"zoom": handler}})
	if err != nil {
		return "new error: " + err.Error()
	}
	defer w.Destroy()
	w.SetSize(800, 600, HintNone)
	defer time.AfterFunc(15*time.Second, w.Terminate).Stop()
	w.Navigate("zoom://test/")
	w.Run()
	if len(widths) != 2 || widths[1] == 0 {
		return fmt.Sprintf("widths %v", widths)
	}
	return fmt.Sprintf("ratio %.1f", widths[0]/widths[1])
}

func TestSetZoom(t *testing.T) {
	got, _ := resZoom.Load().(string)
	requireGUI(t, got)
	if got != "ratio 2.0" {
		t.Fatalf("CSS width at zoom 1 over zoom 2: got %q, want %q", got, "ratio 2.0")
	}
}
