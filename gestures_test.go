//go:build darwin || linux

package glaze

import (
	"fmt"
	"sync/atomic"
	"testing"
)

var resGestures atomic.Value // string

// gesturesScenario reads the engine's swipe-navigation setting with and
// without NavigationGestures.
func gesturesScenario() string {
	var out []bool
	for _, on := range []bool{false, true} {
		v, err := NewWithOptions(Options{NoBridge: true, NavigationGestures: on})
		if err != nil {
			return "new error: " + err.Error()
		}
		out = append(out, navGesturesOn(v.(*webview)))
		v.Destroy()
	}
	return fmt.Sprintf("off=%v on=%v", out[0], out[1])
}

func TestNavigationGestures(t *testing.T) {
	got, _ := resGestures.Load().(string)
	requireGUI(t, got)
	if got != "off=false on=true" {
		t.Fatalf("navigation gestures: %s", got)
	}
}
