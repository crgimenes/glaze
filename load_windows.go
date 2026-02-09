package webview

import "syscall"

func libraryPath() string {
	return "webview.dll"
}

func loadLibrary(name string) (uintptr, error) {
	handle, err := syscall.LoadLibrary(name)
	return uintptr(handle), err
}

func loadSymbol(lib uintptr, name string) uintptr {
	ptr, err := syscall.GetProcAddress(syscall.Handle(lib), name)
	if err != nil {
		panic("webview: failed to load symbol " + name + ": " + err.Error())
	}
	return ptr
}
