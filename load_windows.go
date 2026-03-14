package glaze

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func libraryPath() string {
	const name = "webview.dll"

	// Prefer an absolute path from WEBVIEW_PATH to avoid DLL search order
	// hijacking (CWD, system dirs, etc.).
	webviewPath := os.Getenv("WEBVIEW_PATH")
	if webviewPath != "" {
		abs := filepath.Join(webviewPath, name)
		if _, err := os.Stat(abs); err == nil {
			return abs
		}
	}

	// Fall back to the directory of the running executable.
	execPath, _ := os.Executable()
	if execPath != "" {
		abs := filepath.Join(filepath.Dir(execPath), name)
		if _, err := os.Stat(abs); err == nil {
			return abs
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
	handle, err := syscall.LoadLibrary(name)
	return uintptr(handle), err
}

func loadSymbol(lib uintptr, name string) (uintptr, error) {
	ptr, err := syscall.GetProcAddress(syscall.Handle(lib), name)
	if err != nil {
		return 0, fmt.Errorf("webview: failed to load symbol %s: %w", name, err)
	}
	return ptr, nil
}
