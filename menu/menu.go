// Package menu builds native application menus without cgo.
//
// It is a standalone package: it depends only on purego (and the Objective-C
// runtime on macOS), not on glaze's WebView. Any app with a native run loop can
// install a menu bar — a glaze window, an Ebitengine game, or a bare
// NSApplication. glaze is one consumer, not a requirement.
//
//	menu.Set([]menu.Item{
//		{Title: "App", Submenu: []menu.Item{
//			{Title: "About", OnClick: showAbout},
//			{Separator: true},
//			{Title: "Quit", Shortcut: "cmd+q", OnClick: quit},
//		}},
//		{Title: "Edit", Submenu: []menu.Item{
//			{Title: "Copy", Shortcut: "cmd+c", OnClick: copy},
//		}},
//	}, menu.Options{})
//
// Platform support: macOS (NSMenu) and Windows (a Win32 menu bar attached to the
// caller's window) are implemented; Linux returns ErrUnsupported (the GTK3/GTK4
// menu-bar story is too fragmented to do cheaply).
package menu

import (
	"errors"
	"unsafe"
)

// ErrUnsupported is returned by Set on a platform with no menu backend.
var ErrUnsupported = errors.New("menu: not supported on this platform")

// Item is one entry in a menu. A zero Item with Separator=true is a divider; an
// Item with a non-empty Submenu is a sub-menu (its OnClick is ignored).
type Item struct {
	// Title is the visible label. On macOS the first top-level item is the
	// application menu and its title is replaced by the app name.
	Title string

	// Shortcut is an optional accelerator like "cmd+q" or "cmd+shift+z". The
	// modifiers are cmd, ctrl, alt (a.k.a. opt) and shift; the last token is the
	// key. Empty means no shortcut.
	Shortcut string

	// OnClick is invoked when the item is chosen. It runs on the UI thread (see
	// Options.Dispatch). Ignored for separators and items that have a Submenu.
	OnClick func()

	// Submenu, when non-empty, makes this item open a sub-menu.
	Submenu []Item

	// Separator makes this item a divider; all other fields are ignored.
	Separator bool

	// Disabled greys the item out and blocks OnClick.
	Disabled bool
}

// Options configures Set.
type Options struct {
	// Window is the platform window handle the menu attaches to. macOS ignores it
	// (the menu bar is application-global); Windows needs the HWND. Pass what your
	// framework hands you (e.g. glaze's WebView.Window()).
	Window unsafe.Pointer

	// Dispatch runs f on the UI thread. OnClick callbacks are invoked through it,
	// so they can safely touch UI state. If nil, callbacks run on the thread that
	// delivers the event (the main thread on macOS, so nil is fine there) and Set
	// itself must be called on the UI thread.
	Dispatch func(func())
}

// Set installs items as the application's main menu, replacing any previous one,
// and returns a handle. Build menus must run on the UI thread; pass
// Options.Dispatch if you call Set from another goroutine.
func Set(items []Item, opts Options) (*Menu, error) { return set(items, opts) }

// Menu is an installed menu. Release tears it down (restores the previous menu /
// window procedure where applicable); it is optional and idempotent.
type Menu struct {
	release func()
}

// Release removes the menu and frees what Set retained. Safe to call once; later
// calls are no-ops.
func (m *Menu) Release() {
	if m == nil || m.release == nil {
		return
	}
	r := m.release
	m.release = nil
	r()
}
