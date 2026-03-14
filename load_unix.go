//go:build darwin || linux

package glaze

import (
	"fmt"
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
			return n
		}
	}

	return name
}

func loadLibrary(name string) (uintptr, error) {
	if VerifyBeforeLoad != nil {
		if err := VerifyBeforeLoad(name); err != nil {
			return 0, fmt.Errorf("webview: library verification failed: %w", err)
		}
	}
	return purego.Dlopen(name, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
}

func loadSymbol(lib uintptr, name string) (uintptr, error) {
	ptr, err := purego.Dlsym(lib, name)
	if err != nil {
		return 0, fmt.Errorf("webview: failed to load symbol %s: %w", name, err)
	}
	return ptr, nil
}
