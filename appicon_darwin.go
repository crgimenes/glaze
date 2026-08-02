package glaze

import (
	"errors"
	"unsafe"
)

// setAppIcon hands the bytes to AppKit as an NSImage and makes it the
// application's icon, which is what the Dock draws.
//
// It is safe before or after a window exists: sharedApplication returns the
// one NSApplication a process may have, creating it if the program has not got
// there yet, and the icon set on it survives whoever finishes the launch —
// including a toolkit (Ebitengine, say) that goes on to build its own windows.
func setAppIcon(png []byte) error {
	if len(png) == 0 {
		return errors.New("glaze: the application icon is empty")
	}
	var failed bool
	autorelease(func() {
		// #nosec G103 -- dataWithBytes:length: copies the buffer before it returns
		data := class("NSData").Send(sel("dataWithBytes:length:"), unsafe.Pointer(&png[0]), len(png))
		image := class("NSImage").Send(sel("alloc")).Send(sel("initWithData:"), data)
		if image == 0 {
			failed = true
			return
		}
		image.Send(sel("autorelease"))
		app := class("NSApplication").Send(sel("sharedApplication"))
		app.Send(sel("setApplicationIconImage:"), image)
	})
	if failed {
		return errors.New("glaze: the application icon is not an image AppKit can read")
	}
	return nil
}
