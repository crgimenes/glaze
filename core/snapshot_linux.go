package core

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

// webkit_web_view_get_snapshot is the same call on both stacks, but its result
// is not: webkit2gtk-4.x (GTK3) finishes with a cairo_surface_t, webkitgtk-6.0
// (GTK4) with a GdkTexture. Both hand over their pixels in cairo's ARGB32
// layout, so the two paths meet in argb32ToRGBA and Go's image/png does the
// encoding; no PNG writer of cairo or GDK is involved.
var (
	webkitWebViewGetSnapshot       func(webview uintptr, region, options int32, cancellable, callback, userData uintptr)
	webkitWebViewGetSnapshotFinish func(webview, result uintptr, gerr *uintptr) uintptr
	gErrorFree                     func(gerr uintptr)

	// GTK3: the result is a cairo_surface_t.
	cairoSurfaceMapToImage     func(surface, extents uintptr) uintptr
	cairoSurfaceUnmapImage     func(surface, image uintptr)
	cairoSurfaceFlush          func(surface uintptr)
	cairoSurfaceDestroy        func(surface uintptr)
	cairoImageSurfaceGetData   func(surface uintptr) uintptr
	cairoImageSurfaceGetFormat func(surface uintptr) int32
	cairoImageSurfaceGetWidth  func(surface uintptr) int32
	cairoImageSurfaceGetHeight func(surface uintptr) int32
	cairoImageSurfaceGetStride func(surface uintptr) int32

	// GTK4: the result is a GdkTexture.
	gdkTextureGetWidth  func(texture uintptr) int32
	gdkTextureGetHeight func(texture uintptr) int32
	gdkTextureDownload  func(texture uintptr, data *byte, stride uintptr)

	snapshotInitOnce sync.Once
	snapshotInitErr  error
	snapshotReadyFn  uintptr // GAsyncReadyCallback

	// Captures in flight, keyed by the integer passed as the callback's
	// user_data. GTK thread only.
	snapshotPending = map[uintptr]chan snapshotResult{}
	snapshotSeq     uintptr
)

const (
	webkitSnapshotRegionVisible = 0
	webkitSnapshotOptionsNone   = 0

	cairoFormatARGB32 = 0
	cairoFormatRGB24  = 1
)

type snapshotResult struct {
	img *image.RGBA
	err error
}

// ensureSnapshotInit resolves the snapshot symbols once. libcairo is opened
// only on the GTK3 stack, where GTK already has it in the process.
func ensureSnapshotInit() error {
	err := ensureInit()
	if err != nil {
		return err
	}
	snapshotInitOnce.Do(func() {
		if gtk4 {
			purego.RegisterLibFunc(&gdkTextureGetWidth, gtkLib, "gdk_texture_get_width")
			purego.RegisterLibFunc(&gdkTextureGetHeight, gtkLib, "gdk_texture_get_height")
			purego.RegisterLibFunc(&gdkTextureDownload, gtkLib, "gdk_texture_download")
		} else {
			cairo, e := openFirst("libcairo.so.2")
			if e != nil {
				snapshotInitErr = e
				return
			}
			purego.RegisterLibFunc(&cairoSurfaceMapToImage, cairo, "cairo_surface_map_to_image")
			purego.RegisterLibFunc(&cairoSurfaceUnmapImage, cairo, "cairo_surface_unmap_image")
			purego.RegisterLibFunc(&cairoSurfaceFlush, cairo, "cairo_surface_flush")
			purego.RegisterLibFunc(&cairoSurfaceDestroy, cairo, "cairo_surface_destroy")
			purego.RegisterLibFunc(&cairoImageSurfaceGetData, cairo, "cairo_image_surface_get_data")
			purego.RegisterLibFunc(&cairoImageSurfaceGetFormat, cairo, "cairo_image_surface_get_format")
			purego.RegisterLibFunc(&cairoImageSurfaceGetWidth, cairo, "cairo_image_surface_get_width")
			purego.RegisterLibFunc(&cairoImageSurfaceGetHeight, cairo, "cairo_image_surface_get_height")
			purego.RegisterLibFunc(&cairoImageSurfaceGetStride, cairo, "cairo_image_surface_get_stride")
		}
		purego.RegisterLibFunc(&webkitWebViewGetSnapshot, webkitLib, "webkit_web_view_get_snapshot")
		purego.RegisterLibFunc(&webkitWebViewGetSnapshotFinish, webkitLib, "webkit_web_view_get_snapshot_finish")
		purego.RegisterLibFunc(&gErrorFree, glibLib, "g_error_free")

		// (GObject* source, GAsyncResult* result, gpointer token). The token is
		// an integer key into snapshotPending, so no Go pointer crosses into C.
		snapshotReadyFn = purego.NewCallback(func(source, result, token uintptr) uintptr {
			done := snapshotPending[token]
			delete(snapshotPending, token)
			var gerr uintptr
			obj := webkitWebViewGetSnapshotFinish(source, result, &gerr)
			if obj == 0 {
				msg := "no image"
				if gerr != 0 {
					_, _, msg = readGError(gerr)
					gErrorFree(gerr)
				}
				done <- snapshotResult{err: errors.New("glaze: snapshot failed: " + msg)}
				return 0
			}
			img, e := snapshotPixels(obj)
			done <- snapshotResult{img: img, err: e}
			return 0
		})
	})
	return snapshotInitErr
}

// Snapshot captures the visible viewport with webkit_web_view_get_snapshot and
// returns PNG bytes. WebKitGTK renders the snapshot at the widget's device
// scale, so the pixels are device pixels; see Snapshotter for the contract.
func (w *webview) Snapshot() ([]byte, error) {
	// The completion callback runs on the GTK thread: waiting for it there
	// would wait forever.
	if onUIThread() {
		return nil, errSnapshotUIThread
	}
	done := make(chan snapshotResult, 1)
	dispatchMain(func() {
		err := ensureSnapshotInit()
		if err != nil {
			done <- snapshotResult{err: err}
			return
		}
		if w.webview == 0 {
			done <- snapshotResult{err: errSnapshotNoWebView}
			return
		}
		snapshotSeq++
		snapshotPending[snapshotSeq] = done
		webkitWebViewGetSnapshot(w.webview, webkitSnapshotRegionVisible, webkitSnapshotOptionsNone, 0, snapshotReadyFn, snapshotSeq)
	})

	select {
	case r := <-done:
		if r.err != nil {
			return nil, r.err
		}
		// Encoded here, on the caller's goroutine, to keep the GTK thread free.
		var buf bytes.Buffer
		err := png.Encode(&buf, r.img)
		if err != nil {
			return nil, fmt.Errorf("glaze: snapshot PNG encoding failed: %w", err)
		}
		return buf.Bytes(), nil
	case <-time.After(snapshotTimeout):
		// The pending entry stays until WebKit calls back; done is buffered,
		// so that late send does not block the GTK thread.
		return nil, errSnapshotTimeout
	}
}

// snapshotPixels copies the pixels out of what get_snapshot_finish returned,
// which the caller owns, and releases it.
func snapshotPixels(obj uintptr) (*image.RGBA, error) {
	if gtk4 {
		defer gObjectUnref(obj)
		return texturePixels(obj)
	}
	defer cairoSurfaceDestroy(obj)
	return surfacePixels(obj)
}

// texturePixels downloads a GdkTexture, which gdk_texture_download always
// delivers as CAIRO_FORMAT_ARGB32.
func texturePixels(texture uintptr) (*image.RGBA, error) {
	width, height := int(gdkTextureGetWidth(texture)), int(gdkTextureGetHeight(texture))
	if width <= 0 || height <= 0 {
		return nil, errors.New("glaze: snapshot is empty")
	}
	stride := width * 4
	pix := make([]byte, stride*height)
	gdkTextureDownload(texture, &pix[0], uintptr(stride))
	return argb32ToRGBA(pix, width, height, stride, false), nil
}

// surfacePixels reads a cairo surface through cairo_surface_map_to_image, which
// yields an image surface whatever the surface's backend is.
func surfacePixels(surface uintptr) (*image.RGBA, error) {
	img := cairoSurfaceMapToImage(surface, 0)
	defer cairoSurfaceUnmapImage(surface, img)
	cairoSurfaceFlush(img)

	format := cairoImageSurfaceGetFormat(img)
	if format != cairoFormatARGB32 && format != cairoFormatRGB24 {
		return nil, fmt.Errorf("glaze: snapshot has an unsupported pixel format (cairo format %d)", format)
	}
	width, height := int(cairoImageSurfaceGetWidth(img)), int(cairoImageSurfaceGetHeight(img))
	stride := int(cairoImageSurfaceGetStride(img))
	data := cairoImageSurfaceGetData(img)
	if data == 0 || width <= 0 || height <= 0 || stride < width*4 {
		return nil, errors.New("glaze: snapshot is empty")
	}
	p := *(*unsafe.Pointer)(unsafe.Pointer(&data)) // #nosec G103 -- pixel buffer owned by the mapped surface until unmap
	src := unsafe.Slice((*byte)(p), stride*height) // #nosec G103 -- stride*height is the buffer cairo reports
	return argb32ToRGBA(src, width, height, stride, format == cairoFormatRGB24), nil
}

// argb32ToRGBA converts cairo's ARGB32 / RGB24 pixels to an image.RGBA. Cairo
// stores each pixel as a native-endian 0xAARRGGBB word, which on the
// little-endian machines glaze targets (amd64, arm64) is the byte order
// B, G, R, A. Both formats are alpha-premultiplied, as image.RGBA is, so only
// the channel order changes. RGB24 leaves the top byte undefined: opaque
// forces it to 0xFF.
func argb32ToRGBA(src []byte, width, height, stride int, opaque bool) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		s := src[y*stride : y*stride+width*4]
		d := img.Pix[y*img.Stride : y*img.Stride+width*4]
		for x := 0; x < len(s); x += 4 {
			d[x], d[x+1], d[x+2], d[x+3] = s[x+2], s[x+1], s[x], s[x+3]
			if opaque {
				d[x+3] = 0xFF
			}
		}
	}
	return img
}
