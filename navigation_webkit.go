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

// callURL runs an app handler that takes a URL (new window, navigation
// start) with panic containment, like callNavigation.
func callURL(f func(string), url string) {
	if f == nil {
		return
	}
	defer func() { _ = recover() }()
	f(url)
}

func callFind(f func(bool), found bool) {
	if f == nil {
		return
	}
	defer func() { _ = recover() }()
	f(found)
}

func callDownload(f func(string) string, name string) (path string) {
	if f == nil {
		return ""
	}
	defer func() {
		if recover() != nil {
			path = ""
		}
	}()
	return f(name)
}

func callDownloadDone(f func(string, error), path string, err error) {
	if f == nil {
		return
	}
	defer func() { _ = recover() }()
	f(path, err)
}
