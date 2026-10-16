package glaze

import (
	"fmt"
	"sync/atomic"
	"testing"
)

var resOnClose atomic.Value // string

// onCloseScenario closes a glaze-owned window the way its close button does,
// through the delegate's windowWillClose:, and checks that OnClose ran once and
// only for that window, that the other one survived, and that the Destroy
// afterwards does not call it again. The window count returning to where it
// started is part of the assertion: a person's close leaves the teardown queued
// on the main queue, and if Destroy did not flush it the count would leak into
// the scenario that runs next.
func onCloseScenario() string {
	start := atomic.LoadInt32(&windowCount)
	var closedA, closedB int
	a, err := NewWithOptions(Options{NoBridge: true, OnClose: func() { closedA++ }})
	if err != nil {
		return "new a: " + err.Error()
	}
	b, err := NewWithOptions(Options{NoBridge: true, OnClose: func() { closedB++ }})
	if err != nil {
		a.Destroy()
		return "new b: " + err.Error()
	}

	a.(*webview).window.Send(sel("close"))
	aGone := a.Window() == nil
	bAlive := b.Window() != nil

	a.Destroy() // after a person's close: releases, must not call OnClose again
	b.Destroy()
	return fmt.Sprintf("a=%d b=%d aGone=%v bAlive=%v count=%+d",
		closedA, closedB, aGone, bAlive, atomic.LoadInt32(&windowCount)-start)
}

func TestOnClose(t *testing.T) {
	want := "a=1 b=0 aGone=true bAlive=true count=+0"
	got, _ := resOnClose.Load().(string)
	requireGUI(t, got)
	if got != want {
		t.Fatalf("OnClose: got %q, want %q", got, want)
	}
}
