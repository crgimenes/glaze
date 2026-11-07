//go:build darwin || linux || windows

package glaze

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"
)

var resHidden atomic.Value // string

// hiddenScenario runs a Hidden web view through creation, SetSize, Raise,
// Focus and a page load, and reports whether the window ever showed and the
// viewport the page saw.
func hiddenScenario() string {
	var w WebView
	report := make(chan string, 1)
	page := `<!DOCTYPE html><html><body><script>
new Image().src = 'report?' + innerWidth + 'x' + innerHeight;
</script></body></html>`
	handler := func(req *SchemeRequest) *SchemeResponse {
		_, query, found := strings.Cut(req.URL, "report?")
		if !found {
			return &SchemeResponse{Body: []byte(page), MIMEType: "text/html"}
		}
		select {
		case report <- query:
		default:
		}
		w.Terminate()
		return &SchemeResponse{}
	}

	w, err := NewWithOptions(Options{
		Hidden:          true,
		HideUntilLoaded: true, // ignored when hidden
		SchemeHandlers:  map[string]SchemeHandler{"app": handler},
	})
	if err != nil {
		return "new error: " + err.Error()
	}
	defer w.Destroy()

	visible := false
	check := func() { visible = visible || windowShown(w) }
	w.SetSize(640, 480, HintNone)
	w.Raise()
	w.Focus()
	check()
	defer time.AfterFunc(15*time.Second, w.Terminate).Stop()
	w.Dispatch(check)
	w.Navigate("app://test/index.html")
	w.Run()
	check()

	select {
	case r := <-report:
		return fmt.Sprintf("visible=%v viewport=%s", visible, r)
	default:
		return fmt.Sprintf("visible=%v no report", visible)
	}
}

func TestHidden(t *testing.T) {
	got, _ := resHidden.Load().(string)
	requireGUI(t, got)
	want := "visible=false viewport=640x480"
	if got != want {
		t.Fatalf("Hidden web view: %q, want %q", got, want)
	}
}

func TestHiddenRejectsWindow(t *testing.T) {
	var x int
	_, err := NewWithOptions(Options{Hidden: true, Window: unsafe.Pointer(&x)})
	if !errors.Is(err, ErrHiddenWindow) {
		t.Fatalf("NewWithOptions(Hidden, Window) error = %v, want ErrHiddenWindow", err)
	}
}
