package menu

import (
	"testing"

	"github.com/ebitengine/purego/objc"
)

// TestCallbackRouting builds a menu item with an OnClick, checks its target and
// action are wired to our handler, then sends glazeMenuAction: through the
// Objective-C runtime (the exact call AppKit makes on a click) and confirms the
// Go callback runs. No NSApplication, run loop, or display is needed, so this
// runs headless on CI.
func TestCallbackRouting(t *testing.T) {
	err := ensureInit()
	if err != nil {
		t.Fatalf("ensureInit: %v", err)
	}

	cbMu.Lock()
	callbacks = map[int]func(){}
	cbSeq = 0
	cbDispatch = nil
	cbMu.Unlock()

	fired := false
	autorelease(func() {
		item := buildItem(Item{Title: "Ping", OnClick: func() { fired = true }})

		if item.Send(sel("target")) != menuTarget {
			t.Error("item target is not the shared menu target")
		}
		if objc.SEL(item.Send(sel("action"))) != sel("glazeMenuAction:") {
			t.Error("item action is not glazeMenuAction:")
		}

		menuTarget.Send(sel("glazeMenuAction:"), item)
	})

	if !fired {
		t.Fatal("OnClick did not run when glazeMenuAction: was dispatched")
	}
}

func TestParseShortcut(t *testing.T) {
	cases := []struct {
		in   string
		key  string
		mods int
	}{
		{"", "", 0},
		{"cmd+q", "q", modCommand},
		{"cmd+shift+z", "z", modCommand | modShift},
		{"ctrl+alt+x", "x", modControl | modOption},
		{"opt+space", "space", modOption},
	}
	for _, c := range cases {
		key, mods := parseShortcut(c.in)
		if key != c.key || mods != c.mods {
			t.Errorf("parseShortcut(%q) = (%q, %d), want (%q, %d)", c.in, key, mods, c.key, c.mods)
		}
	}
}
