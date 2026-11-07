package glaze

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var resUnthrottledHidden atomic.Value // string

// throttleChildEnv makes the test binary run only unthrottledHiddenScenario,
// as the first web view of a fresh process, so the stack preference applies.
const throttleChildEnv = "GLAZE_TEST_UNTHROTTLED_HIDDEN_CHILD"

// throttleReport is what the page measures over a fixed window.
type throttleReport struct {
	Timeout0   int    `json:"timeout0"`  // setTimeout(0) chain, per second
	Interval1  int    `json:"interval1"` // setInterval(1), per second
	RAF        int    `json:"raf"`       // requestAnimationFrame, per second
	Visibility string `json:"visibility"`
	Focus      bool   `json:"focus"`
	Viewport   string `json:"viewport"`
	Stack      string `json:"stack"`
	Shown      bool   `json:"shown"`
}

const throttlePage = `<!DOCTYPE html><html><body><script>
const D = 1500;
let t0 = 0, rafs = 0, ticks = 0, ivals = 0;
function loop() { ticks++; if (performance.now() - t0 < D) setTimeout(loop, 0); }
function raf() { rafs++; if (performance.now() - t0 < D) requestAnimationFrame(raf); }
setTimeout(() => {
  t0 = performance.now();
  const iv = setInterval(() => ivals++, 1);
  loop(); requestAnimationFrame(raf);
  setTimeout(() => {
    clearInterval(iv);
    const s = (performance.now() - t0) / 1000;
    report({timeout0: Math.round(ticks / s), interval1: Math.round(ivals / s),
      raf: Math.round(rafs / s), visibility: document.visibilityState,
      focus: document.hasFocus(), viewport: innerWidth + 'x' + innerHeight});
  }, D);
}, 300);
</script></body></html>`

// unthrottledHiddenScenario measures a Hidden web view with DisableThrottling
// and returns the report as JSON.
func unthrottledHiddenScenario() string {
	w, err := NewWithOptions(Options{Hidden: true, DisableThrottling: true})
	if err != nil {
		return "new error: " + err.Error()
	}
	defer w.Destroy()
	var got throttleReport
	result := "no report"
	err = w.Bind("report", func(r throttleReport) {
		got = r
		w.Terminate()
	})
	if err != nil {
		return "bind error: " + err.Error()
	}
	w.SetSize(640, 480, HintNone)
	defer time.AfterFunc(20*time.Second, w.Terminate).Stop()
	w.SetHtml(throttlePage)
	w.Run()
	if got.Viewport != "" {
		got.Stack = "gtk3"
		if gtk4 {
			got.Stack = "gtk4"
		}
		got.Shown = windowShown(w)
		b, _ := json.Marshal(got)
		result = string(b)
	}
	return result
}

// checkUnthrottled returns what is wrong with a report, or "".
func checkUnthrottled(got string) string {
	var r throttleReport
	err := json.Unmarshal([]byte(got), &r)
	if err != nil {
		return got
	}
	var bad []string
	// A throttled hidden page manages about 4 and 2 timers a second, and no
	// animation frames; an unthrottled one is clamped near 250 and 1000 by
	// WebKit, and paces frames by the display.
	if r.Timeout0 < 50 || r.Interval1 < 50 {
		bad = append(bad, fmt.Sprintf("timers throttled (%d, %d per second)", r.Timeout0, r.Interval1))
	}
	if r.RAF < 20 {
		bad = append(bad, fmt.Sprintf("requestAnimationFrame at %d per second", r.RAF))
	}
	if r.Visibility != "visible" {
		bad = append(bad, "visibilityState "+r.Visibility)
	}
	if r.Viewport != "640x480" {
		bad = append(bad, "viewport "+r.Viewport)
	}
	if !r.Shown {
		bad = append(bad, "window not mapped")
	}
	// GTK3's offscreen window never takes focus; GTK4's transparent one may,
	// depending on the window manager (documented on DisableThrottling).
	if r.Stack == "gtk3" && r.Focus {
		bad = append(bad, "page has focus")
	}
	return strings.Join(bad, "; ")
}

func TestUnthrottledHidden(t *testing.T) {
	got, _ := resUnthrottledHidden.Load().(string)
	requireGUI(t, got)
	bad := checkUnthrottled(got)
	if bad != "" {
		t.Fatalf("Hidden + DisableThrottling: %s\n%s", bad, got)
	}
}

// TestUnthrottledHiddenPrefersGTK3 runs the scenario as the first web view of
// a fresh process, where it should pick GTK3 whenever webkit2gtk is installed
// (alongside webkitgtk-6.0 or not), and checks the page runs unthrottled.
func TestUnthrottledHiddenPrefersGTK3(t *testing.T) {
	got, _ := resUnthrottledHidden.Load().(string)
	requireGUI(t, got)
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), throttleChildEnv+"=1")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("child: %v\n%s", err, out)
	}
	report := strings.TrimSpace(string(out))
	i := strings.LastIndex(report, "\n")
	if i >= 0 {
		report = report[i+1:]
	}
	bad := checkUnthrottled(report)
	if bad != "" {
		t.Fatalf("Hidden + DisableThrottling in a fresh process: %s\n%s", bad, report)
	}
	// Probed through ldconfig: dlopen-ing webkit2gtk here would pull GTK3
	// into a process that may already hold GTK4.
	libs, err := exec.Command("ldconfig", "-p").Output()
	if err != nil {
		t.Logf("ldconfig unavailable (%v); stack preference not checked", err)
		return
	}
	if strings.Contains(string(libs), "libwebkit2gtk-4.") && !strings.Contains(report, `"stack":"gtk3"`) {
		t.Fatalf("webkit2gtk is installed but the stack was not GTK3: %s", report)
	}
}
