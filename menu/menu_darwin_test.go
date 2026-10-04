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

// TestSelectorWiring checks that a Selector item carries the native action with
// a nil target, so AppKit resolves it down the responder chain. Headless, same
// as TestCallbackRouting.
func TestSelectorWiring(t *testing.T) {
	err := ensureInit()
	if err != nil {
		t.Fatalf("ensureInit: %v", err)
	}

	autorelease(func() {
		item := buildItem(Item{Title: "Paste", Shortcut: "cmd+v", Selector: "paste:", OnClick: func() {}})

		if item.Send(sel("target")) != 0 {
			t.Error("selector item must have a nil target (responder chain)")
		}
		if objc.SEL(item.Send(sel("action"))) != sel("paste:") {
			t.Error("item action is not the requested selector")
		}
	})
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

// TestServicesItem checks that a Services item gets an empty submenu that
// AppKit may fill and enable on its own, and that the build records it for
// setServicesMenu:. Headless, same as TestCallbackRouting.
func TestServicesItem(t *testing.T) {
	err := ensureInit()
	if err != nil {
		t.Fatalf("ensureInit: %v", err)
	}

	autorelease(func() {
		servicesMenu = 0
		m := buildMenu([]Item{{Title: "App", Submenu: []Item{
			{Title: "Services", Services: true, OnClick: func() {}},
		}}})
		app := m.Send(sel("itemAtIndex:"), 0).Send(sel("submenu"))
		item := app.Send(sel("itemAtIndex:"), 0)
		sub := item.Send(sel("submenu"))
		if sub == 0 || sub != servicesMenu {
			t.Fatalf("services submenu %v, recorded %v", sub, servicesMenu)
		}
		if !objc.Send[bool](sub, sel("autoenablesItems")) {
			t.Error("the Services menu must auto-enable its items")
		}
		if objc.Send[int](sub, sel("numberOfItems")) != 0 {
			t.Error("the Services menu must start empty")
		}
		if item.Send(sel("target")) == menuTarget || objc.SEL(item.Send(sel("action"))) == sel("glazeMenuAction:") {
			t.Error("the Services item must not be wired to a Go callback")
		}
		servicesMenu = 0
		buildMenu([]Item{{Title: "App", Submenu: []Item{{Title: "Quit", OnClick: func() {}}}}})
		if servicesMenu != 0 {
			t.Error("a menu without a Services item recorded one")
		}
	})
}
