// macOS backend: NSMenu / NSMenuItem on NSApplication.mainMenu, via purego's
// Objective-C runtime. The menu bar is application-global, so this works for any
// app that has an NSApplication and a run loop (a glaze window, a game, a bare
// CLI that sets up NSApp). Menu-item actions are routed to Go through one
// registered target object (glazeMenuAction:) keyed by each item's tag.

package menu

import (
	"fmt"
	"runtime"
	"strings"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// NSEventModifierFlags used for key equivalents.
const (
	modShift   = 1 << 17
	modControl = 1 << 18
	modOption  = 1 << 19
	modCommand = 1 << 20
)

var (
	initOnce sync.Once
	initErr  error
	selCache sync.Map // string -> objc.SEL

	menuTarget objc.ID // shared, retained target for every item's action

	cbMu       sync.Mutex
	callbacks  = map[int]func(){} // NSMenuItem tag -> OnClick
	cbSeq      int
	cbDispatch func(func()) // current Options.Dispatch, used by the action handler
)

func ensureInit() error {
	initOnce.Do(func() {
		for _, fw := range []string{
			"/System/Library/Frameworks/Foundation.framework/Foundation",
			"/System/Library/Frameworks/AppKit.framework/AppKit",
		} {
			_, err := purego.Dlopen(fw, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
			if err != nil {
				initErr = fmt.Errorf("menu: load %s: %w", fw, err)
				return
			}
		}
		cls, err := objc.RegisterClass(
			"GlazeMenuTarget", objc.GetClass("NSObject"), nil, nil,
			[]objc.MethodDef{{
				Cmd: sel("glazeMenuAction:"),
				Fn:  menuAction,
			}})
		if err != nil {
			initErr = fmt.Errorf("menu: register target class: %w", err)
			return
		}
		menuTarget = objc.ID(cls).Send(sel("alloc")).Send(sel("init"))
		menuTarget.Send(sel("retain"))
	})
	return initErr
}

// menuAction is the single target-action for every menu item; it dispatches to
// the Go OnClick recorded under the sender's tag. AppKit calls this on the main
// thread.
func menuAction(self objc.ID, _cmd objc.SEL, sender objc.ID) {
	tag := int(sender.Send(sel("tag"))) // #nosec G115 -- tag is a small int set in buildItem
	cbMu.Lock()
	fn := callbacks[tag]
	d := cbDispatch
	cbMu.Unlock()
	if fn == nil {
		return
	}
	if d != nil {
		d(fn)
		return
	}
	fn()
}

func set(items []Item, opts Options) (*Menu, error) {
	err := ensureInit()
	if err != nil {
		return nil, err
	}

	build := func() {
		cbMu.Lock()
		callbacks = map[int]func(){}
		cbSeq = 0
		cbDispatch = opts.Dispatch
		cbMu.Unlock()

		app := class("NSApplication").Send(sel("sharedApplication"))
		autorelease(func() {
			app.Send(sel("setMainMenu:"), buildMenu(items))
		})
	}

	// AppKit work must run on the main thread. If a dispatcher is given, marshal
	// the build onto it and block; otherwise assume the caller is already there.
	if opts.Dispatch != nil {
		done := make(chan struct{})
		opts.Dispatch(func() {
			build()
			close(done)
		})
		<-done
	} else {
		build()
	}

	return &Menu{release: func() {
		// Drop the Go closures so they can be collected; the NSMenu itself is
		// owned by NSApplication and is replaced by the next Set.
		cbMu.Lock()
		callbacks = map[int]func(){}
		cbMu.Unlock()
	}}, nil
}

// buildMenu turns items into an autoreleased NSMenu. setAutoenablesItems:NO so
// the Disabled flag is honored instead of AppKit auto-enabling.
func buildMenu(items []Item) objc.ID {
	m := class("NSMenu").Send(sel("alloc")).Send(sel("init"))
	m.Send(sel("autorelease"))
	m.Send(sel("setAutoenablesItems:"), false)
	for _, it := range items {
		m.Send(sel("addItem:"), buildItem(it))
	}
	return m
}

func buildItem(it Item) objc.ID {
	if it.Separator {
		return class("NSMenuItem").Send(sel("separatorItem"))
	}

	key, mods := parseShortcut(it.Shortcut)
	item := class("NSMenuItem").Send(sel("alloc")).
		Send(sel("initWithTitle:action:keyEquivalent:"), nsstr(it.Title), objc.SEL(0), nsstr(key))
	item.Send(sel("autorelease"))
	if mods != 0 {
		// mods is a small positive NSEventModifierFlags bitmask.
		item.Send(sel("setKeyEquivalentModifierMask:"), uint(mods)) // #nosec G115
	}

	if len(it.Submenu) > 0 {
		item.Send(sel("setSubmenu:"), buildMenu(it.Submenu))
		return item
	}

	if it.Disabled {
		item.Send(sel("setEnabled:"), false)
		return item
	}
	if it.OnClick != nil {
		cbMu.Lock()
		cbSeq++
		tag := cbSeq
		callbacks[tag] = it.OnClick
		cbMu.Unlock()
		item.Send(sel("setTag:"), tag)
		item.Send(sel("setTarget:"), menuTarget)
		item.Send(sel("setAction:"), sel("glazeMenuAction:"))
	}
	return item
}

// parseShortcut splits "cmd+shift+z" into the key equivalent ("z") and the
// modifier mask. The last token is the key; the rest are modifiers.
func parseShortcut(s string) (key string, mods int) {
	if s == "" {
		return "", 0
	}
	parts := strings.Split(s, "+")
	for i, p := range parts {
		p = strings.ToLower(strings.TrimSpace(p))
		if i == len(parts)-1 {
			key = p
			continue
		}
		switch p {
		case "cmd", "command", "super", "meta":
			mods |= modCommand
		case "ctrl", "control":
			mods |= modControl
		case "alt", "opt", "option":
			mods |= modOption
		case "shift":
			mods |= modShift
		}
	}
	return key, mods
}

// --- objc helpers (self-contained; the menu package does not depend on glaze) --

func sel(name string) objc.SEL {
	v, ok := selCache.Load(name)
	if ok {
		return v.(objc.SEL)
	}
	s := objc.RegisterName(name)
	selCache.Store(name, s)
	return s
}

func class(name string) objc.ID { return objc.ID(objc.GetClass(name)) }

func nsstr(s string) objc.ID {
	return class("NSString").Send(sel("stringWithUTF8String:"), s)
}

// autorelease wraps f in an NSAutoreleasePool. LockOSThread pins the goroutine
// for the pool's lifetime: an NSAutoreleasePool is thread-local, so if the
// goroutine migrated between creating the pool and the deferred drain, the pool
// would drain on the wrong thread and corrupt the autorelease stack (an
// intermittent SIGSEGV). Defers run LIFO, so drain happens before UnlockOSThread.
func autorelease(f func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := class("NSAutoreleasePool").Send(sel("alloc")).Send(sel("init"))
	defer pool.Send(sel("drain"))
	f()
}
