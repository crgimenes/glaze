// Linux backend: shortcuts without a menu bar. A portable menu bar is
// fragmented (GtkMenuBar in GTK3, GMenu plus the shell's global menu in GTK4,
// the Wayland app-menu protocol) and none binds simply, so no bar is drawn.
// What a keyboard-driven app needs from its menu is the shortcuts, and those
// are installed on the window: a key handler in the capture phase, ahead of
// the focused widget (a WebKitWebView included), answers the items'
// shortcuts and lets every other key through. cmd maps to ctrl, the Linux
// convention for what macOS puts on cmd.
//
// The GTK the process already loaded (4 or 3) is the one bound here, found
// with RTLD_NOLOAD: the package stays independent of glaze.

package menu

import (
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/ebitengine/purego"
)

const (
	rtldNoload = 0x4 // glibc RTLD_NOLOAD: bind to a library only if already loaded

	gdkShiftMask   = 1 << 0
	gdkControlMask = 1 << 2
	gdkAltMask     = 1 << 3 // GDK_MOD1_MASK in GTK3, GDK_ALT_MASK in GTK4
	modMask        = gdkShiftMask | gdkControlMask | gdkAltMask

	gtkPhaseCapture = 1
)

var (
	initOnce sync.Once
	initErr  error
	gtk4     bool

	gSignalConnectData       func(instance uintptr, signal string, handler, data, destroy uintptr, flags int) uint64
	gSignalHandlerDisconnect func(instance uintptr, id uint64)
	gdkKeyvalToLower         func(keyval uint32) uint32
	gdkUnicodeToKeyval       func(wc uint32) uint32
	gdkKeyvalFromName        func(name string) uint32

	// GTK3
	gdkEventGetKeyval func(event uintptr, keyval *uint32) bool
	gdkEventGetState  func(event uintptr, state *uint32) bool

	// GTK4
	gtkEventControllerKeyNew              func() uintptr
	gtkEventControllerSetPropagationPhase func(controller uintptr, phase int)
	gtkWidgetAddController                func(widget, controller uintptr)
	gtkWidgetRemoveController             func(widget, controller uintptr)

	keyPressGTK3CB uintptr
	keyPressGTK4CB uintptr

	bindMu   sync.Mutex
	bindings = map[uintptr]*binding{} // id -> installed shortcuts
	bindSeq  uintptr
	current  *Menu // Set replaces the previous menu
)

type shortcut struct {
	keyval uint32
	mods   uint32
	do     func()
}

type binding struct {
	shortcuts []shortcut
	dispatch  func(func())
}

func ensureInit() error {
	initOnce.Do(func() { initErr = load() })
	return initErr
}

func load() error {
	gtk, err := purego.Dlopen("libgtk-4.so.1", purego.RTLD_LAZY|rtldNoload)
	gdk := gtk
	gtk4 = err == nil
	if !gtk4 {
		gtk, err = purego.Dlopen("libgtk-3.so.0", purego.RTLD_LAZY|rtldNoload)
		if err != nil {
			return fmt.Errorf("menu: no GTK loaded in this process: %w", ErrUnsupported)
		}
		gdk, err = purego.Dlopen("libgdk-3.so.0", purego.RTLD_LAZY|rtldNoload)
		if err != nil {
			return fmt.Errorf("menu: libgdk-3: %w", err)
		}
	}
	gobject, err := purego.Dlopen("libgobject-2.0.so.0", purego.RTLD_LAZY|rtldNoload)
	if err != nil {
		return fmt.Errorf("menu: libgobject: %w", err)
	}
	purego.RegisterLibFunc(&gSignalConnectData, gobject, "g_signal_connect_data")
	purego.RegisterLibFunc(&gSignalHandlerDisconnect, gobject, "g_signal_handler_disconnect")
	purego.RegisterLibFunc(&gdkKeyvalToLower, gdk, "gdk_keyval_to_lower")
	purego.RegisterLibFunc(&gdkUnicodeToKeyval, gdk, "gdk_unicode_to_keyval")
	purego.RegisterLibFunc(&gdkKeyvalFromName, gdk, "gdk_keyval_from_name")

	if gtk4 {
		purego.RegisterLibFunc(&gtkEventControllerKeyNew, gtk, "gtk_event_controller_key_new")
		purego.RegisterLibFunc(&gtkEventControllerSetPropagationPhase, gtk, "gtk_event_controller_set_propagation_phase")
		purego.RegisterLibFunc(&gtkWidgetAddController, gtk, "gtk_widget_add_controller")
		purego.RegisterLibFunc(&gtkWidgetRemoveController, gtk, "gtk_widget_remove_controller")
		// gboolean key-pressed(GtkEventControllerKey*, guint keyval, guint keycode,
		// GdkModifierType state, gpointer). A guint arrives in a full register
		// whose upper half is unspecified, hence the uint32 conversions.
		keyPressGTK4CB = purego.NewCallback(func(ctrl, keyval, keycode, state, data uintptr) uintptr {
			return handleKey(data, uint32(keyval), uint32(state)) // #nosec G115 -- guint arguments
		})
		return nil
	}
	purego.RegisterLibFunc(&gdkEventGetKeyval, gdk, "gdk_event_get_keyval")
	purego.RegisterLibFunc(&gdkEventGetState, gdk, "gdk_event_get_state")
	// gboolean key-press-event(GtkWidget*, GdkEventKey*, gpointer). Connected on
	// the toplevel, it runs before the default handler hands the key to the
	// focused widget.
	keyPressGTK3CB = purego.NewCallback(func(widget, event, data uintptr) uintptr {
		var keyval, state uint32
		if !gdkEventGetKeyval(event, &keyval) || !gdkEventGetState(event, &state) {
			return 0
		}
		return handleKey(data, keyval, state)
	})
	return nil
}

// handleKey runs the shortcut matching keyval and state and reports whether
// the key was consumed (TRUE stops it from reaching the page).
func handleKey(id uintptr, keyval, state uint32) uintptr {
	bindMu.Lock()
	b := bindings[id]
	bindMu.Unlock()
	if b == nil {
		return 0
	}
	keyval = gdkKeyvalToLower(keyval)
	state &= modMask
	for _, s := range b.shortcuts {
		if s.keyval != keyval || s.mods != state {
			continue
		}
		if b.dispatch != nil {
			b.dispatch(s.do)
			return 1
		}
		s.do()
		return 1
	}
	return 0
}

func set(items []Item, opts Options) (*Menu, error) {
	if opts.Window == nil {
		// No GtkWindow to hold the shortcuts (an Ebitengine window, say): the
		// same as no menu support at all, so callers that skip ErrUnsupported
		// keep doing so.
		return nil, fmt.Errorf("menu: no GtkWindow in Options.Window: %w", ErrUnsupported)
	}
	err := ensureInit()
	if err != nil {
		return nil, err
	}
	b := &binding{shortcuts: collect(items, nil), dispatch: opts.Dispatch}
	window := uintptr(opts.Window)

	bindMu.Lock()
	bindSeq++
	id := bindSeq
	bindings[id] = b
	prev := current
	bindMu.Unlock()
	prev.Release()

	var release func()
	if gtk4 {
		ctrl := gtkEventControllerKeyNew()
		gtkEventControllerSetPropagationPhase(ctrl, gtkPhaseCapture)
		gSignalConnectData(ctrl, "key-pressed", keyPressGTK4CB, id, 0, 0)
		gtkWidgetAddController(window, ctrl) // the window takes ownership
		release = func() { gtkWidgetRemoveController(window, ctrl) }
	} else {
		handler := gSignalConnectData(window, "key-press-event", keyPressGTK3CB, id, 0, 0)
		release = func() { gSignalHandlerDisconnect(window, handler) }
	}

	m := &Menu{release: func() {
		release()
		bindMu.Lock()
		delete(bindings, id)
		bindMu.Unlock()
	}}
	bindMu.Lock()
	current = m
	bindMu.Unlock()
	return m, nil
}

// collect gathers every enabled item that has both a shortcut and a Go
// callback; selector items belong to macOS's responder chain and are skipped.
func collect(items []Item, out []shortcut) []shortcut {
	for _, it := range items {
		if len(it.Submenu) > 0 {
			out = collect(it.Submenu, out)
			continue
		}
		if it.Separator || it.Disabled || it.OnClick == nil || it.Selector != "" || it.Shortcut == "" {
			continue
		}
		keyval, mods, ok := parseShortcut(it.Shortcut)
		if ok {
			out = append(out, shortcut{keyval: keyval, mods: mods, do: it.OnClick})
		}
	}
	return out
}

var keyNames = map[string]string{
	"escape": "Escape", "esc": "Escape", "enter": "Return", "return": "Return",
	"tab": "Tab", "space": "space", "backspace": "BackSpace", "delete": "Delete",
	"left": "Left", "right": "Right", "up": "Up", "down": "Down",
	"home": "Home", "end": "End", "pageup": "Page_Up", "pagedown": "Page_Down",
}

func parseShortcut(s string) (keyval, mods uint32, ok bool) {
	parts := strings.Split(s, "+")
	key := strings.ToLower(strings.TrimSpace(parts[len(parts)-1]))
	if key == "" && strings.HasSuffix(s, "+") { // "cmd++"
		key = "+"
	}
	for _, p := range parts[:len(parts)-1] {
		switch strings.ToLower(strings.TrimSpace(p)) {
		case "cmd", "command", "super", "meta", "ctrl", "control":
			mods |= gdkControlMask
		case "alt", "opt", "option":
			mods |= gdkAltMask
		case "shift":
			mods |= gdkShiftMask
		}
	}
	switch {
	case utf8.RuneCountInString(key) == 1:
		r, _ := utf8.DecodeRuneInString(key)
		keyval = gdkUnicodeToKeyval(uint32(r)) // #nosec G115 -- a rune is non-negative here
	case len(key) >= 2 && key[0] == 'f' && strings.Trim(key[1:], "0123456789") == "":
		keyval = gdkKeyvalFromName("F" + key[1:])
	case keyNames[key] != "":
		keyval = gdkKeyvalFromName(keyNames[key])
	}
	if keyval == 0 {
		return 0, 0, false
	}
	return gdkKeyvalToLower(keyval), mods, true
}
