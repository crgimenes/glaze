package menu

import (
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/crgimenes/glaze"
)

// withWindow loads GTK through a real glaze window, which the shortcut
// backend binds to; it skips where there is no display.
func withWindow(t *testing.T) glaze.WebView {
	t.Helper()
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Skip("no display")
	}
	runtime.LockOSThread()
	w, err := glaze.New(false)
	if err != nil {
		t.Skip("no GTK window: ", err)
	}
	t.Cleanup(w.Destroy)
	return w
}

func TestShortcutsMatchKeysAndModifiers(t *testing.T) {
	w := withWindow(t)
	var got []string
	hit := func(name string) func() { return func() { got = append(got, name) } }
	m, err := Set([]Item{
		{Title: "View", Submenu: []Item{
			{Title: "Reload", Shortcut: "cmd+r", OnClick: hit("reload")},
			{Title: "Redo", Shortcut: "cmd+shift+z", OnClick: hit("redo")},
			{Title: "Back", Shortcut: "cmd+[", OnClick: hit("back")},
			{Title: "Copy", Shortcut: "cmd+c", Selector: "copy:"},
			{Title: "Off", Shortcut: "cmd+o", OnClick: hit("off"), Disabled: true},
			{Title: "F5", Shortcut: "f5", OnClick: hit("f5")},
		}},
	}, Options{Window: w.Window()})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Release()

	id := bindSeq
	key := func(r rune) uint32 { return gdkUnicodeToKeyval(uint32(r)) }
	cases := []struct {
		keyval, state uint32
		consumed      bool
	}{
		{key('r'), gdkControlMask, true},
		{key('R'), gdkControlMask, true},                // caps lock: keyval lowered
		{key('r'), gdkControlMask | 1<<4, true},         // Mod2 (num lock) ignored
		{key('r'), 0, false},                            // plain r types into the page
		{key('r'), gdkControlMask | gdkAltMask, false},  // extra modifier: not ours
		{key('Z'), gdkControlMask | gdkShiftMask, true}, // redo
		{key('z'), gdkControlMask, false},               // undo belongs to the page
		{key('['), gdkControlMask, true},                // back
		{key('c'), gdkControlMask, false},               // selector item: page copies
		{key('o'), gdkControlMask, false},               // disabled item
		{gdkKeyvalFromName("F5"), 0, true},
	}
	for _, c := range cases {
		consumed := handleKey(id, c.keyval, c.state) == 1
		if consumed != c.consumed {
			t.Errorf("keyval %#x state %#x: consumed=%v, want %v", c.keyval, c.state, consumed, c.consumed)
		}
	}
	want := "reload reload reload redo back f5"
	s := strings.Join(got, " ")
	if s != want {
		t.Errorf("ran %q, want %q", s, want)
	}
}

func TestNoWindowIsUnsupported(t *testing.T) {
	_, err := Set([]Item{{Title: "x", Shortcut: "cmd+r", OnClick: func() {}}}, Options{})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
}
