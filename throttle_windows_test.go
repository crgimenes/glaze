package glaze

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var (
	resWinHiddenThrottle    atomic.Value // string
	resWinMinimized         atomic.Value // string
	resWinMinimizedNoThrott atomic.Value // string
)

const swShowMinNoActive = 7

// winThrottleReport is what winThrottlePage measures over a fixed window.
type winThrottleReport struct {
	Timeout0   int    `json:"timeout0"` // setTimeout(0) chain, per second
	RAF        int    `json:"raf"`      // requestAnimationFrame, per second
	Visibility string `json:"visibility"`
	Viewport   string `json:"viewport"`
	Restored   *struct {
		RAF        int    `json:"raf"`
		Visibility string `json:"visibility"`
	} `json:"restored,omitempty"`
}

// winThrottlePage measures for 1.5 s once the page has settled. If the host
// defines restore(), it then calls it and measures rAF again for 1 s.
const winThrottlePage = `<!DOCTYPE html><html><body><script>
function measure(d, done) {
  let ticks = 0, rafs = 0; const t0 = performance.now();
  (function loop() { ticks++; if (performance.now() - t0 < d) setTimeout(loop, 0); })();
  (function raf() { rafs++; if (performance.now() - t0 < d) requestAnimationFrame(raf); })();
  setTimeout(() => {
    const s = (performance.now() - t0) / 1000;
    done({timeout0: Math.round(ticks / s), raf: Math.round(rafs / s),
      visibility: document.visibilityState, viewport: innerWidth + 'x' + innerHeight});
  }, d);
}
setTimeout(() => measure(1500, r => {
  if (!window.restore) { report(r); return; }
  restore().then(() => setTimeout(() => measure(1000, r2 => { r.restored = r2; report(r); }), 300));
}), 500);
</script></body></html>`

// winThrottleScenario runs winThrottlePage in a web view made with opts.
// minimize minimizes the window (without activating it) before the page
// loads, and restores it for a second measurement.
func winThrottleScenario(opts Options, minimize bool) string {
	w, err := NewWithOptions(opts)
	if err != nil {
		return "new error: " + err.Error()
	}
	defer w.Destroy()
	hwnd := w.(*webview).window
	var got winThrottleReport
	err = w.Bind("report", func(r winThrottleReport) {
		got = r
		w.Terminate()
	})
	if err != nil {
		return "bind error: " + err.Error()
	}
	if minimize {
		err = w.Bind("restore", func() {
			w.Dispatch(func() { showWindow(hwnd, swRestore) })
		})
		if err != nil {
			return "bind error: " + err.Error()
		}
	}
	w.SetSize(640, 480, HintNone)
	if minimize {
		showWindow(hwnd, swShowMinNoActive)
	}
	defer time.AfterFunc(20*time.Second, w.Terminate).Stop()
	w.SetHtml(winThrottlePage)
	w.Run()
	if got.Viewport == "" {
		return "no report"
	}
	b, _ := json.Marshal(got)
	return string(b)
}

// checkWinThrottle returns what about a report is not as expected, or "".
func checkWinThrottle(got string, wantThrottled bool) string {
	var r winThrottleReport
	if err := json.Unmarshal([]byte(got), &r); err != nil {
		return got
	}
	var bad []string
	// Throttled, Chromium manages about 1 timer a second and no animation
	// frames; unthrottled, about 170 (its 4 ms nested-timer clamp) and the
	// display's frame rate.
	throttled := r.Timeout0 < 20 && r.RAF < 5
	unthrottled := r.Timeout0 >= 50 && r.RAF >= 20
	switch {
	case wantThrottled && !throttled:
		bad = append(bad, fmt.Sprintf("not throttled (%d timers, %d frames per second)", r.Timeout0, r.RAF))
	case !wantThrottled && !unthrottled:
		bad = append(bad, fmt.Sprintf("throttled (%d timers, %d frames per second)", r.Timeout0, r.RAF))
	}
	wantVis := "visible"
	if wantThrottled {
		wantVis = "hidden"
	}
	if r.Visibility != wantVis {
		bad = append(bad, "visibilityState "+r.Visibility)
	}
	if r.Viewport != "640x480" {
		bad = append(bad, "viewport "+r.Viewport)
	}
	if r.Restored != nil && (r.Restored.Visibility != "visible" || r.Restored.RAF < 20) {
		bad = append(bad, fmt.Sprintf("after restore: %s, %d frames per second", r.Restored.Visibility, r.Restored.RAF))
	}
	return strings.Join(bad, "; ")
}

func TestHiddenNotThrottled(t *testing.T) {
	got, _ := resWinHiddenThrottle.Load().(string)
	requireGUI(t, got)
	if bad := checkWinThrottle(got, false); bad != "" {
		t.Fatalf("Hidden web view: %s (%s)", bad, got)
	}
}

// TestMinimizedThrottled pins the default: a minimized window's page is
// throttled (its web view is sized to nothing), which saves battery.
func TestMinimizedThrottled(t *testing.T) {
	got, _ := resWinMinimized.Load().(string)
	requireGUI(t, got)
	if bad := checkWinThrottle(got, true); bad != "" {
		t.Fatalf("minimized web view: %s (%s)", bad, got)
	}
}

func TestMinimizedDisableThrottling(t *testing.T) {
	got, _ := resWinMinimizedNoThrott.Load().(string)
	requireGUI(t, got)
	if bad := checkWinThrottle(got, false); bad != "" {
		t.Fatalf("minimized web view with DisableThrottling: %s (%s)", bad, got)
	}
}
