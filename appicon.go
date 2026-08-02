package glaze

import "errors"

// SetAppIcon gives the running application the icon in png — the picture the
// Dock, the taskbar or the switcher shows for the PROCESS, which is a
// different thing from the icon baked into the executable file.
//
// It is application-wide, not per window, because that is what every platform
// that has the concept models: a process has one face. It is also why this is
// a package function rather than a WebView method — a program with no webview
// at all (a game window, a headless helper that raises a dialog) wants it just
// as much.
//
// Where a platform has no such concept, it reports ErrIconUnsupported, which
// callers are expected to log and carry on: an application that refuses to
// start because it could not wear its own icon is worse than a plain one.
func SetAppIcon(png []byte) error { return setAppIcon(png) }

// ErrIconUnsupported is returned where the platform has no application-level
// icon to set at runtime — on Windows and Linux the icon comes from the
// executable's resources or the desktop entry, both decided before the
// process exists.
var ErrIconUnsupported = errors.New("glaze: setting the application icon at runtime is not supported on this platform")
