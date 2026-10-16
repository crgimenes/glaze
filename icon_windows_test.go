package glaze

import "testing"

// The test binary carries no icon resource, so the class must get none and
// keep the generic icon; a binary with one is checked by eye (title bar and
// taskbar show it). The call also proves the shell32 binding resolves.
func TestExeIconsNoneWithoutResource(t *testing.T) {
	err := ensureWinInit()
	if err != nil {
		t.Skipf("win32 init: %v", err)
	}
	big, small := exeIcons()
	if big != 0 || small != 0 {
		t.Errorf("exeIcons() = %#x, %#x for a binary without an icon resource, want 0, 0", big, small)
	}
	big2, small2 := exeIcons()
	if big2 != big || small2 != small {
		t.Errorf("exeIcons() changed between calls: %#x, %#x then %#x, %#x", big, small, big2, small2)
	}
}
