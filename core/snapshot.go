package core

// Snapshotter is implemented by backends that can rasterize the page they are
// showing. It is deliberately a side interface rather than a method on WebView:
// adding it there would force every backend to implement it, and the backends
// that cannot yet (Linux, Windows) would stop compiling.
//
// Type-assert for it:
//
//	if s, ok := w.(glaze.Snapshotter); ok { png, err := s.Snapshot() }
type Snapshotter interface {
	// Snapshot returns the current page as PNG bytes. It must be called from a
	// goroutine that is not the UI thread: the capture completes on the main
	// queue, so blocking the main thread would deadlock until the timeout.
	Snapshot() ([]byte, error)
}
