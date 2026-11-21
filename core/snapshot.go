package core

import (
	"errors"
	"time"
)

// Snapshotter is implemented by the web view of every backend (macOS, Linux,
// Windows). It is a side interface rather than a method on WebView, so adding
// it broke no other implementation of WebView.
//
// Type-assert for it:
//
//	s, ok := w.(glaze.Snapshotter)
//	if ok {
//		png, err := s.Snapshot()
//	}
type Snapshotter interface {
	// Snapshot captures the page as the web view is showing it right now and
	// returns it as an encoded image.
	//
	// Format: the bytes are always a PNG file, on every platform, ready for
	// os.WriteFile or image/png.Decode. There is no JPEG or raw-pixel variant.
	//
	// Area: the visible viewport only, which is the web view's content area.
	// The window's title bar and borders are not included, and neither is the
	// part of a long page that is scrolled out of view.
	//
	// Scale / DPI: the image is measured in device (physical) pixels, not in
	// the logical units that SetSize and CSS use (points on macOS,
	// device-independent pixels on Windows, application pixels on GTK). Its
	// size is the viewport's logical size multiplied by the scale factor of
	// the display the window is on, which is window.devicePixelRatio at 100%
	// zoom:
	//
	//	viewport 800x600 on a standard display (scale 1)       ->  800x600 PNG
	//	viewport 800x600 on a Retina / 200% HiDPI display      -> 1600x1200 PNG
	//	viewport 800x600 on a 150% Windows display             -> 1200x900 PNG
	//
	// So the same window yields a different image size on a different
	// monitor, and a page coordinate (CSS px) maps to an image coordinate by
	// multiplying by that scale factor. Snapshot does not resample: to get a
	// logical-size image on a HiDPI display, scale the result down by the
	// factor. Any DPI metadata inside the PNG is whatever the platform's
	// encoder wrote and is not part of this contract; go by the pixel
	// dimensions.
	//
	// Threading: unlike most WebView methods, Snapshot blocks until the
	// engine delivers the image, and the engine delivers it on the UI thread.
	// Call it from any other goroutine (a Bind callback is one), with Run
	// running. Called from the UI thread it cannot complete and returns an
	// error instead: immediately on Linux and Windows, after a timeout on
	// macOS.
	//
	// The engine can only capture a page it is painting: a minimized window,
	// or one created with Options.Hidden, may produce an error or a stale
	// frame.
	Snapshot() ([]byte, error)
}

// snapshotTimeout bounds how long Snapshot waits for the engine's completion
// callback, so a capture that never completes cannot hang the caller.
const snapshotTimeout = 15 * time.Second

var (
	errSnapshotNoWebView = errors.New("glaze: snapshot before the web view exists")
	errSnapshotTimeout   = errors.New("glaze: snapshot timed out")
	errSnapshotUIThread  = errors.New("glaze: Snapshot called from the UI thread; call it from another goroutine")
)
