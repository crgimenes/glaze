//go:build darwin || linux
// +build darwin linux

package webview

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/ebitengine/purego"
)

func libraryPath() string {
	var name string
	var paths []string

	webviewPath := os.Getenv("WEBVIEW_PATH")
	execPath, _ := os.Executable()
	dir := filepath.Dir(execPath)

	switch runtime.GOOS {
	case "linux":
		name = "libwebview.so"
		paths = []string{webviewPath, dir}
	case "darwin":
		name = "libwebview.dylib"
		paths = []string{webviewPath, dir, filepath.Join(dir, "..", "Frameworks")}
	}

	for _, v := range paths {
		n := filepath.Join(v, name)
		if _, err := os.Stat(n); err == nil {
			name = n
			break
		}
	}

	return name
}

func loadLibrary(name string) (uintptr, error) {
	return purego.Dlopen(name, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
}

func loadSymbol(lib uintptr, name string) uintptr {
	ptr, err := purego.Dlsym(lib, name)
	if err != nil {
		panic("webview: failed to load symbol " + name + ": " + err.Error())
	}
	return ptr
}
