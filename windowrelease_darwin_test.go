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

// The WindowServer lists a window until its NSWindow is deallocated, closed or
// not; NSApp.windows drops it on close and cannot see a leak.
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
	for i := range count {
		info := list.Send(sel("objectAtIndex:"), uint(i))
		owner := info.Send(sel("objectForKey:"), nsstr("kCGWindowOwnerPID"))
		if owner != 0 && int64(owner.Send(sel("longLongValue"))) == pid {
			n++
		}
	}
	return n
}

// Earlier scenarios' windows are still leaving; counting before the number
// holds still hides a leak behind their departure.
func settleWindows() int {
	last, still := serverWindows(), 0
	for i := 0; i < 200 && still < 10; i++ {
		autorelease(func() {
			rl := class("NSRunLoop").Send(sel("currentRunLoop"))
			rl.Send(sel("runUntilDate:"), class("NSDate").Send(sel("dateWithTimeIntervalSinceNow:"), 0.05))
		})
		n := serverWindows()
		if n != last {
			last, still = n, 0
			continue
		}
		still++
	}
	return last
}

// Tears windows down both ways, Destroy alone and the user closing first.
func windowReleaseScenario() string {
	const n = 3
	base := settleWindows()
	if base < 0 {
		return "WindowServer list unavailable"
	}
	for _, closeFirst := range []bool{false, true} {
		for range n {
			w, err := New(false)
			if err != nil {
				return "new error: " + err.Error()
			}
			if closeFirst {
				// The red button; AppKit runs it under its own autorelease pool.
				autorelease(func() { w.(*webview).window.Send(sel("close")) })
			}
			w.Destroy()
		}
	}
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
