//go:build darwin || linux

package glaze

// callNavigation runs the app's handler with panic containment: it is called
// from native delegate callbacks, where an unwound panic kills the process.
func callNavigation(f func(NavigationEvent), ev NavigationEvent) {
	if f == nil {
		return
	}
	defer func() { _ = recover() }()
	f(ev)
}
