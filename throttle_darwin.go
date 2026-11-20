//go:build !glaze_appstore

package glaze

import "github.com/ebitengine/purego/objc"

// Options.DisableThrottling on macOS, the private-API part. These selectors
// are WebKit SPI: App Review rejects binaries that carry private selector
// names, so Mac App Store submissions build with -tags glaze_appstore, which
// swaps this file for no-ops and keeps only the public-API subset (see
// Options.DisableThrottling).

// disableThrottlingSPI turns off WebKit's hidden-page timer throttling and
// visibility-based WebContent process suppression. Each preference setter is
// checked first, since none is guaranteed by any macOS version.
func disableThrottlingSPI(prefs objc.ID) {
	for _, setter := range []string{
		"_setPageVisibilityBasedProcessSuppressionEnabled:",
		"_setHiddenPageDOMTimerThrottlingEnabled:",
		"_setHiddenPageDOMTimerThrottlingAutoIncreases:",
	} {
		if responds(prefs, setter) {
			prefs.Send(sel(setter), false)
		}
	}
}

// disableOcclusionDetection keeps WebKit from treating the page as hidden
// when its window is covered or fully transparent; without it, an occluded
// page is throttled no matter what the scheduling policy says.
func disableOcclusionDetection(webView objc.ID) {
	if responds(webView, "_setWindowOcclusionDetectionEnabled:") {
		webView.Send(sel("_setWindowOcclusionDetectionEnabled:"), false)
	}
}
