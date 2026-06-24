// Linux has no menu backend. A portable application menu bar is fragmented:
// GtkMenuBar (GTK3), GMenu plus the desktop shell's global menu (GTK4), and the
// Wayland app-menu protocol all disagree, and none is "reasonably simple" to bind
// the way the macOS menu bar is. So Set returns ErrUnsupported on Linux.

package menu

func set(items []Item, opts Options) (*Menu, error) {
	return nil, ErrUnsupported
}
