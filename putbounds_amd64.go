//go:build windows && amd64

package glaze

import (
	"unsafe"

	"github.com/ebitengine/purego"
)

// putBounds (amd64): per the Win64 ABI, a struct larger than 8 bytes (RECT is
// 16) is passed by hidden reference, so the caller passes a pointer to it.
func (i *iController) putBounds(r rect) {
	purego.SyscallN(i.vtbl.PutBounds, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(&r)))
}
