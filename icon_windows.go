package glaze

import (
	"os"
	"sync"
)

var (
	exeIconOnce sync.Once
	exeIconBig  uintptr
	exeIconSm   uintptr
)

// exeIcons is the executable's first icon in the two sizes a window class
// takes, for the title bar and the taskbar. A Go binary carries one only when
// a resource was embedded (a .syso); with none, both are 0 and the class gets
// the generic icon, as before. The handles live as long as the process, like
// the class that holds them.
func exeIcons() (big, small uintptr) {
	exeIconOnce.Do(func() {
		exe, err := os.Executable()
		if err != nil {
			return
		}
		extractIconExW(utf16(exe), 0, &exeIconBig, &exeIconSm, 1)
	})
	return exeIconBig, exeIconSm
}
