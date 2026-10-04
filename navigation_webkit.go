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

// callScriptDialog asks the app, with panic containment; a panic answers as a
// dismissed dialog.
func callScriptDialog(f func(ScriptDialog) (bool, string), d ScriptDialog) (ok bool, text string) {
	defer func() {
		if recover() != nil {
			ok, text = false, ""
		}
	}()
	return f(d)
}

func callShown(f func()) {
	if f == nil {
		return
	}
	defer func() { _ = recover() }()
	f()
}

// The URL of the page on screen, for OnURLChange. While a load is in flight
// the engine's URL is first its target, which may still fail -- set as soon
// as the app asks, before the engine reports the start -- so during a load a
// change counts only once that load committed. Outside a load (same-document
// moves) a change counts as it comes; repeats never do.
func (w *webview) loadStarted() { w.committed = false }

func (w *webview) loadCommitted(url string) {
	w.committed = true
	w.urlChanged(url, true)
}

func (w *webview) loadEnded() { w.committed = false }

func (w *webview) urlChanged(url string, loading bool) {
	if loading && !w.committed || url == "" || url == w.pageURL {
		return
	}
	w.pageURL = url
	callURL(w.onURLChange, url)
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

// decideMediaCapture asks the app, with panic containment; without
// OnMediaCapture the answer is no.
func (w *webview) decideMediaCapture(origin string, camera, microphone bool) (allow bool) {
	if w.onMediaCapture == nil {
		return false
	}
	defer func() { _ = recover() }()
	return w.onMediaCapture(origin, camera, microphone)
}
