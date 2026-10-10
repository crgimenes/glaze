package glaze

import (
	"fmt"
	"os"
	"sync/atomic"
	"testing"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

var resWindowRelease atomic.Value // string

// serverWindows counts this process's windows in the WindowServer, which lists
// a window until its NSWindow is deallocated -- hidden, closed or alpha 0
// included. NSApp.windows cannot see a leak: a closed window leaves that list.
func serverWindows() int {
	cg, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_LAZY)
	if err != nil {
		return -1
	}
	var copyInfo func(option uint32, relativeTo uint32) objc.ID
	purego.RegisterLibFunc(&copyInfo, cg, "CGWindowListCopyWindowInfo")
	list := copyInfo(0, 0) // kCGWindowListOptionAll, kCGNullWindowID
	if list == 0 {
		return -1
	}
	defer list.Send(sel("release")) // Copy rule: the caller owns it
	pid := int64(os.Getpid())
	n := 0
	count := int(list.Send(sel("count")))
	for i := 0; i < count; i++ {
		info := list.Send(sel("objectAtIndex:"), uint(i))
		owner := info.Send(sel("objectForKey:"), nsstr("kCGWindowOwnerPID"))
		if owner != 0 && int64(owner.Send(sel("longLongValue"))) == pid {
			n++
		}
	}
	return n
}

// settleWindows turns the run loop until the WindowServer's window count has
// held still for half a second: earlier scenarios' windows are still being
// dropped, and counting before that hides a leak behind their departure.
func settleWindows() int {
	last, still := serverWindows(), 0
	for i := 0; i < 200 && still < 10; i++ {
		autorelease(func() {
			rl := class("NSRunLoop").Send(sel("currentRunLoop"))
			rl.Send(sel("runUntilDate:"), class("NSDate").Send(sel("dateWithTimeIntervalSinceNow:"), 0.05))
		})
		if n := serverWindows(); n != last {
			last, still = n, 0
		} else {
			still++
		}
	}
	return last
}

// windowReleaseScenario opens windows and tears each down both ways -- Destroy
// alone, and the user closing the window first (the red button) -- and returns
// how many windows the WindowServer still lists for the process afterwards.
// Every leaked reference keeps one: the engine used to retain the window, the
// web view and the widget twice and release them once, so the NSWindow was
// never deallocated (and its WebContent process never exited).
func windowReleaseScenario() string {
	const n = 3
	base := settleWindows()
	if base < 0 {
		return "WindowServer list unavailable"
	}
	for _, closeFirst := range []bool{false, true} {
		for i := 0; i < n; i++ {
			w, err := New(false)
			if err != nil {
				return "new error: " + err.Error()
			}
			if closeFirst {
				// windowWillClose: -> onWindowWillClose, as the red button does.
				// AppKit runs this under its own autorelease pool.
				autorelease(func() { w.(*webview).window.Send(sel("close")) })
			}
			w.Destroy()
		}
	}
	// The WindowServer drops a window a moment after its dealloc.
	left := settleWindows() - base
	return fmt.Sprintf("left=%d", left)
}

func TestDestroyLeavesNoWindowInTheServer(t *testing.T) {
	got, _ := resWindowRelease.Load().(string)
	requireGUI(t, got)
	if got != "left=0" {
		t.Fatalf("windows still listed by the WindowServer after Destroy: %s", got)
	}
}
