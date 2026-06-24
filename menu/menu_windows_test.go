package menu

import "testing"

// TestCommandRouting builds a menu with an OnClick item, then sends the
// WM_COMMAND that Windows posts on a click straight to the subclass procedure and
// confirms the Go callback fires. No window or message loop is needed for the
// command path, so this runs headless on CI.
func TestCommandRouting(t *testing.T) {
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
	bar := buildMenu([]Item{
		{Title: "File", Submenu: []Item{
			{Title: "Ping", OnClick: func() { fired = true }},
		}},
	}, true)
	if bar == 0 {
		t.Fatal("CreateMenu returned 0")
	}
	defer destroyMenu(bar)

	// "Ping" is the first item with a callback, so it got id 1. Deliver its
	// WM_COMMAND (HIWORD 0 = from a menu).
	menuWndProc(0, wmCommand, 1, 0)
	if !fired {
		t.Fatal("WM_COMMAND did not route to the menu item's OnClick")
	}
}
