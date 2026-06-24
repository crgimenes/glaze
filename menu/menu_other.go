//go:build !darwin && !windows && !linux

// Fallback for platforms without a menu backend; keeps the package building for
// every GOOS.

package menu

func set(items []Item, opts Options) (*Menu, error) {
	return nil, ErrUnsupported
}
