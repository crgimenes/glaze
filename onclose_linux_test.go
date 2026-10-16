package glaze

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

var resOnClose atomic.Value // string

// onCloseScenario opens two windows and closes the first as the person would
// (gtk_window_close, what the close button does): only its OnClose runs, the
// loop keeps the second one alive, and Destroy never calls OnClose.
func onCloseScenario() string {
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
	a.SetHtml("<p>a</p>")
	b.SetHtml("<p>b</p>")
	var bAlive bool
	b.Dispatch(func() { gtkWindowClose(a.(*webview).window) })
	time.AfterFunc(time.Second, func() {
		b.Dispatch(func() {
			bAlive = b.Window() != nil
			b.Terminate()
		})
	})
	defer time.AfterFunc(15*time.Second, b.Terminate).Stop()
	b.Run()
	a.Destroy() // after a person's close: releases, must not call OnClose again
	b.Destroy()
	return fmt.Sprintf("a=%d b=%d aGone=%v bAlive=%v", closedA, closedB, a.Window() == nil, bAlive)
}

func TestOnClose(t *testing.T) {
	got, _ := resOnClose.Load().(string)
	requireGUI(t, got)
	want := "a=1 b=0 aGone=true bAlive=true"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
