package glaze

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// swatch is the smallest thing AppKit will accept as an image.
func swatch(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			img.SetRGBA(x, y, color.RGBA{R: 0xff, G: 0x55, B: 0x55, A: 0xff})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encoding the swatch: %v", err)
	}
	return buf.Bytes()
}

// The icon has to actually reach NSApplication — reading it back is the only
// way to know, since nothing about the call fails when AppKit quietly refuses
// the bytes.
func TestSetAppIconReachesTheApplication(t *testing.T) {
	if err := SetAppIcon(swatch(t)); err != nil {
		t.Fatalf("SetAppIcon: %v", err)
	}
	app := class("NSApplication").Send(sel("sharedApplication"))
	got := app.Send(sel("applicationIconImage"))
	if got == 0 {
		t.Fatal("the application has no icon after one was set")
	}
	// An NSImage built from bytes AppKit could not decode still exists as an
	// object but is not valid, which is exactly the failure a nil check misses.
	if got.Send(sel("isValid")) == 0 {
		t.Fatal("the icon AppKit holds is not a valid image")
	}
}

// An icon that is not an image must be reported, not silently ignored: a
// caller passing the wrong bytes deserves to hear about it once.
func TestSetAppIconRejectsWhatIsNotAnImage(t *testing.T) {
	if err := SetAppIcon(nil); err == nil {
		t.Error("an empty icon was accepted")
	}
	if err := SetAppIcon([]byte("this is not a png")); err == nil {
		t.Error("a string was accepted as an image")
	}
}
