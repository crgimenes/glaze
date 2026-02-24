package webview

import (
	"fmt"
	"syscall"
)

func libraryPath() string {
	return "webview.dll"
}

func loadLibrary(name string) (uintptr, error) {
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
