package glaze

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ebitengine/purego/objc"
)

// nsappFirstEnv makes the test binary act as the child process for
// TestNewAfterAppFinishedLaunching instead of running the test suite.
const nsappFirstEnv = "GLAZE_TEST_NSAPP_FIRST"

// nsappFirstChild finishes launching the application the way any other
// AppKit user in the process would (a tray icon, a game window, a dialog),
// lets that run loop end, and only then creates the first web view.
//
// applicationDidFinishLaunching: is delivered once per process, so the first
// web view's bootstrap must not wait for it here.
func nsappFirstChild() int {
	runtime.LockOSThread()
	err := ensureInit()
	if err != nil {
		fmt.Println("error:", err)
		return 1
	}
	app := class("NSApplication").Send(sel("sharedApplication"))
	// Run the application and stop it, as native/tray does around tray.Run.
	// [NSApp run] finishes launching on the way in.
	// The stop is queued once the loop has been up a moment: stopped on its
	// very first turn, AppKit has not finished launching yet.
	go func() {
		time.Sleep(time.Second)
		dispatchMain(func() {
			autorelease(func() {
				app.Send(sel("stop:"), objc.ID(0))
				postWakeEvent(app)
			})
		})
	}()
	app.Send(sel("run"))

	go func() {
		time.Sleep(10 * time.Second)
		fmt.Println("hang: New did not return")
		os.Exit(2)
	}()
	w, err := New(false)
	if err != nil {
		fmt.Println("error:", err)
		return 1
	}
	w.Destroy()
	fmt.Println("ok")
	return 0
}

// TestNewAfterAppFinishedLaunching runs in a child process: the condition is
// "nothing in this process has created a web view yet", which the scenarios in
// TestMain have already spent.
func TestNewAfterAppFinishedLaunching(t *testing.T) {
	if testing.Short() {
		t.Skip("GUI test skipped (-short)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), nsappFirstEnv+"=1")
	out, err := cmd.CombinedOutput()
	got := strings.TrimSpace(string(out))
	if err != nil || !strings.HasSuffix(got, "ok") {
		t.Fatalf("New after the app finished launching: %v\n%s", err, got)
	}
}
