//go:build windows && arm64

package glaze

import (
	"unsafe"

	"github.com/ebitengine/purego"
)

// putBounds (arm64): per AAPCS64, a 16-byte integer aggregate (RECT) is passed
// by value in two consecutive registers, not by reference. Pack the four int32
// fields into two uintptrs (lo = {Left,Top}, hi = {Right,Bottom}).
func (i *iController) putBounds(r rect) {
	lo := uintptr(uint32(r.Left)) | uintptr(uint32(r.Top))<<32
	hi := uintptr(uint32(r.Right)) | uintptr(uint32(r.Bottom))<<32
	purego.SyscallN(i.vtbl.PutBounds, uintptr(unsafe.Pointer(i)), lo, hi)
}
