// Windows backend: planned but not yet implemented (CreateMenu / AppendMenuW /
// SetMenu attached to the caller's HWND, with WM_COMMAND routed back by
// subclassing the window procedure). Until then Set returns ErrUnsupported.

package menu

func set(items []Item, opts Options) (*Menu, error) {
	return nil, ErrUnsupported
}
