//go:build !darwin

package glaze

// Windows and Linux have no application icon to set at runtime: Windows reads
// it from the executable's own resources, and a Linux desktop reads it from
// the .desktop entry that names the program. Both are decided before the
// process starts, so there is nothing honest to do here.
//
// Window icons are a separate thing and belong to whoever owns the window.
func setAppIcon(_ []byte) error { return ErrIconUnsupported }
