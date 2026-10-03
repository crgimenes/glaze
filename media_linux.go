package glaze

import (
	"net/url"

	"github.com/ebitengine/purego"
)

// Media capture on WebKitGTK: every permission request reaches
// permission-request; the camera and microphone go to OnMediaCapture, the
// rest (geolocation, notifications, ...) is denied, which is also what the
// engine does when the signal is left alone.

var (
	gTypeCheckInstanceIsA              func(instance, typ uintptr) bool
	webkitUserMediaRequestType         func() uintptr
	webkitUserMediaIsForAudio          func(request uintptr) bool
	webkitUserMediaIsForVideo          func(request uintptr) bool
	webkitPermissionRequestAllow       func(request uintptr)
	webkitPermissionRequestDeny        func(request uintptr)
	webkitSettingsSetEnableMediaStream func(settings uintptr, enabled bool)
	webkitSettingsSetEnableWebRTC      func(settings uintptr, enabled bool) // nil before WebKitGTK 2.38

	permissionRequestFn uintptr
)

func registerMediaFuncs(webkit, gobject uintptr) {
	purego.RegisterLibFunc(&gTypeCheckInstanceIsA, gobject, "g_type_check_instance_is_a")
	purego.RegisterLibFunc(&webkitUserMediaRequestType, webkit, "webkit_user_media_permission_request_get_type")
	purego.RegisterLibFunc(&webkitUserMediaIsForAudio, webkit, "webkit_user_media_permission_is_for_audio_device")
	purego.RegisterLibFunc(&webkitUserMediaIsForVideo, webkit, "webkit_user_media_permission_is_for_video_device")
	purego.RegisterLibFunc(&webkitPermissionRequestAllow, webkit, "webkit_permission_request_allow")
	purego.RegisterLibFunc(&webkitPermissionRequestDeny, webkit, "webkit_permission_request_deny")
	purego.RegisterLibFunc(&webkitSettingsSetEnableMediaStream, webkit, "webkit_settings_set_enable_media_stream")
	_, err := purego.Dlsym(webkit, "webkit_settings_set_enable_webrtc")
	if err == nil {
		purego.RegisterLibFunc(&webkitSettingsSetEnableWebRTC, webkit, "webkit_settings_set_enable_webrtc")
	}

	// gboolean permission-request(WebKitWebView*, WebKitPermissionRequest*, gpointer)
	permissionRequestFn = purego.NewCallback(func(webview, request, userData uintptr) uintptr {
		allow := false
		w := lookupEngine(userData)
		switch {
		case w == nil:
		case gTypeCheckInstanceIsA(request, webkitUserMediaRequestType()):
			// The request carries no origin (WebKitGTK drops it), so the
			// top-level page's stands for it, as OnMediaCapture documents.
			allow = w.decideMediaCapture(originOf(cstr(webkitWebViewGetURI(webview))),
				webkitUserMediaIsForVideo(request), webkitUserMediaIsForAudio(request))
		}
		if allow {
			webkitPermissionRequestAllow(request)
		} else {
			webkitPermissionRequestDeny(request)
		}
		return 1
	})
}

// mediaSettings turns on what a call needs -- media streams and WebRTC, which
// WebKitGTK leaves off -- only for an app that decides media capture.
func (w *webview) mediaSettings(settings uintptr) {
	if w.onMediaCapture == nil {
		return
	}
	webkitSettingsSetEnableMediaStream(settings, true)
	if webkitSettingsSetEnableWebRTC != nil {
		webkitSettingsSetEnableWebRTC(settings, true)
	}
}

// originOf is the scheme://host[:port] of a URL, "" when it has no host.
func originOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}
