//go:build glaze_appstore

package glaze

import "github.com/ebitengine/purego/objc"

// Mac App Store builds: no private WebKit selector may appear in the binary,
// not even unused -- App Review scans for the strings. DisableThrottling then
// keeps its public-API subset only: App Nap opt-out, the macOS 14+ inactive
// scheduling policy, and the transparent on-screen window. An occluded page
// is throttled again, since occlusion detection has no public switch.

func disableThrottlingSPI(objc.ID) {}

func disableOcclusionDetection(objc.ID) {}
