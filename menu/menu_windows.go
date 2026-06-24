// Windows backend: a Win32 menu bar (CreateMenu / AppendMenuW / SetMenu) attached
// to the caller's HWND. Menu clicks arrive as WM_COMMAND on the window's
// procedure, so the package subclasses that procedure (SetWindowLongPtrW with
// GWLP_WNDPROC): it handles WM_COMMAND for its own item IDs and forwards every
// other message to the original procedure via CallWindowProcW. Release restores
// the original procedure. This is self-contained, so it works for any window (a
// glaze window, a game's window) without that framework's cooperation.
//
// Keyboard accelerators (the Shortcut field) are not wired here: that needs an
// accelerator table translated in the owner's message loop, which the package
// does not control. On Windows the Shortcut is ignored; items are clickable.
// Windows has no dlopen, so user32 symbols are resolved with LoadLibrary/
// GetProcAddress and bound with purego.RegisterFunc.

package menu

import (
	"errors"
	"fmt"
	"sync"
	"syscall"

	"github.com/ebitengine/purego"
)

const (
	mfString    = 0x0000
	mfGrayed    = 0x0001
	mfPopup     = 0x0010
	mfSeparator = 0x0800

	gwlpWndProc = -4
	wmCommand   = 0x0111
)

var (
	initOnce sync.Once
	initErr  error

	createMenu        func() uintptr
	createPopupMenu   func() uintptr
	appendMenuW       func(hMenu uintptr, flags uint32, idNewItem uintptr, lpNewItem *uint16) int32
	setMenu           func(hwnd, hMenu uintptr) int32
	drawMenuBar       func(hwnd uintptr) int32
	destroyMenu       func(hMenu uintptr) int32
	setWindowLongPtrW func(hwnd uintptr, index int32, newLong uintptr) uintptr
	callWindowProcW   func(proc, hwnd, msg, wParam, lParam uintptr) uintptr
	defWindowProcW    func(hwnd, msg, wParam, lParam uintptr) uintptr

	menuWndProcCB uintptr // shared subclass procedure

	cbMu       sync.Mutex
	callbacks  = map[int]func(){} // WM_COMMAND id -> OnClick
	cbSeq      int
	cbDispatch func(func())

	hwndMu   sync.Mutex
	origProc = map[uintptr]uintptr{} // hwnd -> original WndProc
	curBar   = map[uintptr]uintptr{} // hwnd -> current menu-bar HMENU
)

func ensureInit() error {
	initOnce.Do(func() {
		user32, err := syscall.LoadLibrary("user32.dll")
		if err != nil {
			initErr = fmt.Errorf("menu: load user32.dll: %w", err)
			return
		}
		reg := func(p any, name string) {
			if initErr != nil {
				return
			}
			addr, e := syscall.GetProcAddress(user32, name)
			if e != nil {
				initErr = fmt.Errorf("menu: resolve %s: %w", name, e)
				return
			}
			purego.RegisterFunc(p, addr)
		}
		reg(&createMenu, "CreateMenu")
		reg(&createPopupMenu, "CreatePopupMenu")
		reg(&appendMenuW, "AppendMenuW")
		reg(&setMenu, "SetMenu")
		reg(&drawMenuBar, "DrawMenuBar")
		reg(&destroyMenu, "DestroyMenu")
		reg(&setWindowLongPtrW, "SetWindowLongPtrW")
		reg(&callWindowProcW, "CallWindowProcW")
		reg(&defWindowProcW, "DefWindowProcW")
		if initErr != nil {
			return
		}
		menuWndProcCB = purego.NewCallback(menuWndProc)
	})
	return initErr
}

// menuWndProc is the subclassed window procedure: it claims WM_COMMAND for its
// own menu item IDs and passes everything else to the window's original proc.
func menuWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	// A menu command has the notification code (HIWORD) zero; controls and
	// accelerators set it, so this avoids stealing their WM_COMMAND.
	if msg == wmCommand && (wParam>>16)&0xFFFF == 0 {
		id := int(wParam & 0xFFFF)
		cbMu.Lock()
		fn := callbacks[id]
		d := cbDispatch
		cbMu.Unlock()
		if fn != nil {
			if d != nil {
				d(fn)
			} else {
				fn()
			}
			return 0
		}
	}
	hwndMu.Lock()
	old := origProc[hwnd]
	hwndMu.Unlock()
	if old != 0 {
		return callWindowProcW(old, hwnd, msg, wParam, lParam)
	}
	return defWindowProcW(hwnd, msg, wParam, lParam)
}

func set(items []Item, opts Options) (*Menu, error) {
	if opts.Window == nil {
		return nil, errors.New("menu: Options.Window (the HWND) is required on Windows")
	}
	err := ensureInit()
	if err != nil {
		return nil, err
	}
	hwnd := uintptr(opts.Window)

	build := func() {
		cbMu.Lock()
		callbacks = map[int]func(){}
		cbSeq = 0
		cbDispatch = opts.Dispatch
		cbMu.Unlock()

		bar := buildMenu(items, true)

		hwndMu.Lock()
		prev := curBar[hwnd]
		curBar[hwnd] = bar
		_, subclassed := origProc[hwnd]
		if !subclassed {
			origProc[hwnd] = setWindowLongPtrW(hwnd, gwlpWndProc, menuWndProcCB)
		}
		hwndMu.Unlock()

		setMenu(hwnd, bar)
		drawMenuBar(hwnd)
		if prev != 0 {
			destroyMenu(prev) // SetMenu does not free the old bar
		}
	}

	runOnUI(opts.Dispatch, build)

	return &Menu{release: func() {
		runOnUI(opts.Dispatch, func() {
			hwndMu.Lock()
			old, hadProc := origProc[hwnd]
			delete(origProc, hwnd)
			bar := curBar[hwnd]
			delete(curBar, hwnd)
			hwndMu.Unlock()

			if hadProc && old != 0 {
				setWindowLongPtrW(hwnd, gwlpWndProc, old)
			}
			setMenu(hwnd, 0)
			drawMenuBar(hwnd)
			if bar != 0 {
				destroyMenu(bar)
			}
			cbMu.Lock()
			callbacks = map[int]func(){}
			cbMu.Unlock()
		})
	}}, nil
}

// runOnUI runs f on the UI thread through dispatch (blocking) when one is given,
// or directly when the caller is already on it.
func runOnUI(dispatch func(func()), f func()) {
	if dispatch == nil {
		f()
		return
	}
	done := make(chan struct{})
	dispatch(func() {
		f()
		close(done)
	})
	<-done
}

// buildMenu creates a menu (a bar or a popup submenu) and appends items.
func buildMenu(items []Item, bar bool) uintptr {
	var h uintptr
	if bar {
		h = createMenu()
	} else {
		h = createPopupMenu()
	}
	for _, it := range items {
		addItem(h, it)
	}
	return h
}

func addItem(h uintptr, it Item) {
	if it.Separator {
		appendMenuW(h, mfSeparator, 0, nil)
		return
	}
	text := utf16(it.Title)
	if len(it.Submenu) > 0 {
		sub := buildMenu(it.Submenu, false)
		appendMenuW(h, mfPopup, sub, text)
		return
	}
	flags := uint32(mfString)
	if it.Disabled {
		flags |= mfGrayed
	}
	var id uintptr
	if it.OnClick != nil && !it.Disabled {
		cbMu.Lock()
		cbSeq++
		id = uintptr(cbSeq) // #nosec G115 -- cbSeq is a small positive command id
		callbacks[cbSeq] = it.OnClick
		cbMu.Unlock()
	}
	appendMenuW(h, flags, id, text)
}

// utf16 returns a NUL-terminated UTF-16 pointer for s ("" yields an empty
// string, never nil).
func utf16(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		empty, _ := syscall.UTF16PtrFromString("")
		return empty
	}
	return p
}
