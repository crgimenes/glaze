package glaze

import (
	"sync"

	"github.com/ebitengine/purego"
)

// Options.DisableThrottling on Linux.
//
// WebKitGTK treats a page whose window is not mapped as hidden: DOM timers
// drop to about one a second, requestAnimationFrame and CSS animations stop,
// and the viewport collapses. Turning off its hidden-page features restores
// the timers but nothing else, so a Hidden view also needs a window that is
// mapped yet never on screen: a GtkOffscreenWindow on GTK3 (which is why a
// Hidden view with DisableThrottling prefers the GTK3 stack), or otherwise
// a fully transparent, undecorated, click-through window that is never
// presented. GTK4 has no offscreen window, and WebKitGTK 2.48.0 to 2.48.3
// crash mapping a web view in one: they look up the monitor of the
// toplevel window, which WebKit never tracks for an offscreen window, and
// dereference null (fixed in 2.48.4).

// throttlingFeatures are the WebKitFeature identifiers turned off.
var throttlingFeatures = map[string]bool{
	"HiddenPageDOMTimerThrottling":              true,
	"HiddenPageDOMTimerThrottlingAutoIncreases": true,
	"HiddenPageCSSAnimationSuspension":          true,
}

var (
	throttleOnce sync.Once

	webkitSettingsGetAllFeatures    func() uintptr
	webkitFeatureListGetLength      func(list uintptr) uintptr
	webkitFeatureListGet            func(list, index uintptr) uintptr
	webkitFeatureListUnref          func(list uintptr)
	webkitFeatureGetIdentifier      func(feature uintptr) uintptr
	webkitSettingsSetFeatureEnabled func(settings, feature uintptr, enabled bool)

	webkitGetMajorVersion func() uint32
	webkitGetMinorVersion func() uint32
	webkitGetMicroVersion func() uint32

	gtkOffscreenWindowNew  func() uintptr                         // GTK3
	gtkWindowSetAcceptFoc  func(window uintptr, accept bool)      // GTK3
	gtkWindowSetFocusOnMap func(window uintptr, focus bool)       // GTK3
	gtkWindowSetSkipTask   func(window uintptr, skip bool)        // GTK3
	gtkWindowSetSkipPager  func(window uintptr, skip bool)        // GTK3
	gtkWidgetGetWindow     func(widget uintptr) uintptr           // GTK3
	gdkWindowInputShape    func(window, region uintptr, x, y int) // GTK3
	gtkWindowSetDecorated  func(window uintptr, decorated bool)
	gtkWidgetSetOpacity    func(widget uintptr, opacity float64)
	gtkWidgetRealize       func(widget uintptr)
	gtkNativeGetSurface    func(native uintptr) uintptr     // GTK4
	gdkSurfaceSetInputRgn  func(surface, region uintptr)    // GTK4
	gdkX11SurfaceGetType   func() uintptr                   // GTK4, X11 backend
	gdkX11SetSkipTaskbar   func(surface uintptr, skip bool) // GTK4, X11 backend
	gdkX11SetSkipPager     func(surface uintptr, skip bool) // GTK4, X11 backend
	cairoRegionCreate      func() uintptr                   // GTK4
	cairoRegionDestroy     func(region uintptr)             // GTK4
)

// registerOptional binds fn to name in lib if lib exports it, leaving fn nil
// otherwise.
func registerOptional(fn any, lib uintptr, name string) {
	if lib == 0 {
		return
	}
	addr, err := purego.Dlsym(lib, name)
	if err == nil && addr != 0 {
		purego.RegisterLibFunc(fn, lib, name)
	}
}

func loadThrottleFuncs() {
	throttleOnce.Do(func() {
		// WebKitFeature arrived in WebKitGTK 2.42; older versions keep the
		// hidden-page throttling.
		_, err := purego.Dlsym(webkitLib, "webkit_settings_get_all_features")
		if err == nil {
			purego.RegisterLibFunc(&webkitSettingsGetAllFeatures, webkitLib, "webkit_settings_get_all_features")
			purego.RegisterLibFunc(&webkitFeatureListGetLength, webkitLib, "webkit_feature_list_get_length")
			purego.RegisterLibFunc(&webkitFeatureListGet, webkitLib, "webkit_feature_list_get")
			purego.RegisterLibFunc(&webkitFeatureListUnref, webkitLib, "webkit_feature_list_unref")
			purego.RegisterLibFunc(&webkitFeatureGetIdentifier, webkitLib, "webkit_feature_get_identifier")
			purego.RegisterLibFunc(&webkitSettingsSetFeatureEnabled, webkitLib, "webkit_settings_set_feature_enabled")
		}
		purego.RegisterLibFunc(&gtkWindowSetDecorated, gtkLib, "gtk_window_set_decorated")
		purego.RegisterLibFunc(&gtkWidgetSetOpacity, gtkLib, "gtk_widget_set_opacity")
		purego.RegisterLibFunc(&gtkWidgetRealize, gtkLib, "gtk_widget_realize")
		purego.RegisterLibFunc(&webkitGetMajorVersion, webkitLib, "webkit_get_major_version")
		purego.RegisterLibFunc(&webkitGetMinorVersion, webkitLib, "webkit_get_minor_version")
		purego.RegisterLibFunc(&webkitGetMicroVersion, webkitLib, "webkit_get_micro_version")
		// GTK links cairo, so this finds the loaded copy.
		cairo, err := openFirst("libcairo.so.2")
		if err == nil {
			registerOptional(&cairoRegionCreate, cairo, "cairo_region_create")
			registerOptional(&cairoRegionDestroy, cairo, "cairo_region_destroy")
		}
		if !gtk4 {
			purego.RegisterLibFunc(&gtkOffscreenWindowNew, gtkLib, "gtk_offscreen_window_new")
			purego.RegisterLibFunc(&gtkWindowSetAcceptFoc, gtkLib, "gtk_window_set_accept_focus")
			purego.RegisterLibFunc(&gtkWindowSetFocusOnMap, gtkLib, "gtk_window_set_focus_on_map")
			purego.RegisterLibFunc(&gtkWindowSetSkipTask, gtkLib, "gtk_window_set_skip_taskbar_hint")
			purego.RegisterLibFunc(&gtkWindowSetSkipPager, gtkLib, "gtk_window_set_skip_pager_hint")
			purego.RegisterLibFunc(&gtkWidgetGetWindow, gtkLib, "gtk_widget_get_window")
			purego.RegisterLibFunc(&gdkWindowInputShape, gtkLib, "gdk_window_input_shape_combine_region")
			return
		}
		purego.RegisterLibFunc(&gtkNativeGetSurface, gtkLib, "gtk_native_get_surface")
		purego.RegisterLibFunc(&gdkSurfaceSetInputRgn, gtkLib, "gdk_surface_set_input_region")
		registerOptional(&gdkX11SurfaceGetType, gtkLib, "gdk_x11_surface_get_type")
		registerOptional(&gdkX11SetSkipTaskbar, gtkLib, "gdk_x11_surface_set_skip_taskbar_hint")
		registerOptional(&gdkX11SetSkipPager, gtkLib, "gdk_x11_surface_set_skip_pager_hint")
	})
}

// disableThrottling turns off WebKit's hidden-page timer throttling and CSS
// animation suspension in settings, where this WebKitGTK offers them.
func disableThrottling(settings uintptr) {
	loadThrottleFuncs()
	if webkitSettingsGetAllFeatures == nil {
		return
	}
	list := webkitSettingsGetAllFeatures()
	if list == 0 {
		return
	}
	defer webkitFeatureListUnref(list)
	n := webkitFeatureListGetLength(list)
	for i := range n {
		f := webkitFeatureListGet(list, i)
		if throttlingFeatures[cstr(webkitFeatureGetIdentifier(f))] {
			webkitSettingsSetFeatureEnabled(settings, f, false)
		}
	}
}

// offscreenWindowCrashes reports whether this WebKitGTK is one that crashes
// mapping a web view in a GtkOffscreenWindow (see the top of this file).
func offscreenWindowCrashes() bool {
	return webkitGetMajorVersion() == 2 && webkitGetMinorVersion() == 48 && webkitGetMicroVersion() < 4
}

// useOffscreenWindow reports whether a Hidden view with DisableThrottling
// gets a GtkOffscreenWindow rather than a transparent one.
func useOffscreenWindow() bool {
	return !gtk4 && !offscreenWindowCrashes()
}

// newUnthrottledHiddenWindow creates the window for a Hidden view with
// DisableThrottling: one that can be mapped, so WebKit sees the page as
// visible, without appearing on screen. showUnthrottledHiddenWindow maps it.
func newUnthrottledHiddenWindow() uintptr {
	loadThrottleFuncs()
	if useOffscreenWindow() {
		return gtkOffscreenWindowNew()
	}
	win := gtkNewWindow()
	gtkWindowSetDecorated(win, false) // no client-side title bar eating the viewport
	gtkWidgetSetOpacity(win, 0)
	if !gtk4 {
		gtkWindowSetAcceptFoc(win, false)
		gtkWindowSetFocusOnMap(win, false)
		gtkWindowSetSkipTask(win, true)
		gtkWindowSetSkipPager(win, true)
	}
	return win
}

// showUnthrottledHiddenWindow maps a window from newUnthrottledHiddenWindow
// without presenting or focusing it. A transparent window is also made
// click-through and, on X11, kept out of the taskbar and pager.
func showUnthrottledHiddenWindow(win uintptr) {
	switch {
	case useOffscreenWindow():
		gtkWidgetShow(win)
	case !gtk4:
		gtkWidgetShow(win)
		if cairoRegionCreate != nil && cairoRegionDestroy != nil {
			empty := cairoRegionCreate()
			gdkWindowInputShape(gtkWidgetGetWindow(win), empty, 0, 0)
			cairoRegionDestroy(empty)
		}
	default:
		showGTK4TransparentWindow(win)
	}
}

func showGTK4TransparentWindow(win uintptr) {
	gtkWidgetRealize(win)
	surface := gtkNativeGetSurface(win)
	isX11 := surface != 0 && gdkX11SurfaceGetType != nil &&
		gTypeCheckInstanceIsA(surface, gdkX11SurfaceGetType())
	if isX11 && gdkX11SetSkipTaskbar != nil && gdkX11SetSkipPager != nil {
		// Set before mapping, so the window manager sees them from the start.
		gdkX11SetSkipTaskbar(surface, true)
		gdkX11SetSkipPager(surface, true)
	}
	gtkWidgetSetVisible(win, true)
	if surface != 0 && cairoRegionCreate != nil && cairoRegionDestroy != nil {
		empty := cairoRegionCreate()
		gdkSurfaceSetInputRgn(surface, empty)
		cairoRegionDestroy(empty)
	}
}
