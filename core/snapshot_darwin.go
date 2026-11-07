package core

import (
	"errors"
	"fmt"
	"time"
	"unsafe"

	"github.com/ebitengine/purego/objc"
)

// Snapshot rasterizes the page with -[WKWebView takeSnapshotWithConfiguration:
// completionHandler:] and returns PNG bytes.
//
// The completion handler is a block, which purego builds and copies for us —
// the same pattern Find already uses for -findString:withConfiguration:
// completionHandler:. WebKit delivers it on the main queue, which Run drains.
func (w *webview) Snapshot() ([]byte, error) {
	type result struct {
		png []byte
		err error
	}
	done := make(chan result, 1)

	performOnMain(func() {
		if w.webView == 0 {
			done <- result{err: errors.New("glaze: snapshot before the web view exists")}
			return
		}
		autorelease(func() {
			// WKSnapshotConfiguration exists from 10.13; nil is accepted and
			// means "the whole visible rect at native scale".
			var cfg objc.ID
			if cls := objc.GetClass("WKSnapshotConfiguration"); cls != 0 {
				cfg = objc.ID(cls).Send(sel("new")).Send(sel("autorelease"))
			}
			// WebKit copies the block; this reference is released below.
			block := objc.NewBlock(func(_ objc.Block, image objc.ID, _ objc.ID) {
				if image == 0 {
					done <- result{err: errors.New("glaze: snapshot returned no image")}
					return
				}
				png, err := pngFromNSImage(image)
				done <- result{png: png, err: err}
			})
			w.webView.Send(sel("takeSnapshotWithConfiguration:completionHandler:"), cfg, block)
			block.Release()
		})
	})

	select {
	case r := <-done:
		return r.png, r.err
	case <-time.After(15 * time.Second):
		return nil, errors.New("glaze: snapshot timed out (called from the UI thread?)")
	}
}

// pngFromNSImage converts an NSImage into PNG bytes through the bitmap
// representation, which is the path AppKit offers without extra frameworks.
func pngFromNSImage(image objc.ID) ([]byte, error) {
	ti := image.Send(sel("TIFFRepresentation"))
	if ti == 0 {
		return nil, errors.New("glaze: snapshot image has no TIFF representation")
	}
	rep := class("NSBitmapImageRep").Send(sel("imageRepWithData:"), ti)
	if rep == 0 {
		return nil, errors.New("glaze: could not build a bitmap representation")
	}
	const nsBitmapImageFileTypePNG = 4
	data := rep.Send(sel("representationUsingType:properties:"), uint64(nsBitmapImageFileTypePNG), objc.ID(0))
	if data == 0 {
		return nil, errors.New("glaze: PNG encoding failed")
	}
	n := objc.Send[uint64](data, sel("length"))
	ptr := objc.Send[unsafe.Pointer](data, sel("bytes"))
	if n == 0 || ptr == nil {
		return nil, errors.New("glaze: PNG representation is empty")
	}
	// The NSData owns the buffer and is autoreleased; copy out of it.
	out := make([]byte, int(n))
	copy(out, unsafe.Slice((*byte)(ptr), int(n))) // #nosec G103 -- reading an NSData buffer
	return out, nil
}

var _ = fmt.Sprintf
