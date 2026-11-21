package core

import (
	"errors"
	"fmt"
	"math"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

var iidCapturePreviewCompleted = guid{0x697E05E9, 0x3D8F, 0x45FA, [8]byte{0x96, 0xF4, 0x8F, 0xFE, 0x1E, 0xDE, 0xDA, 0xF5}}

const (
	capturePreviewImageFormatPNG = 0 // COREWEBVIEW2_CAPTURE_PREVIEW_IMAGE_FORMAT_PNG

	streamSeekSet = 0
	streamSeekEnd = 2
)

// iStreamVtbl is IStream through Seek; later methods are omitted, none are
// called.
type iStreamVtbl struct {
	iUnknownVtbl
	Read  uintptr
	Write uintptr
	Seek  uintptr
}

type iStream struct{ vtbl *iStreamVtbl }

func asStream(p uintptr) *iStream { return (*iStream)(ptr(p)) }

func (i *iStream) Release() { purego.SyscallN(i.vtbl.Release, uintptr(unsafe.Pointer(i))) }

// seek moves the stream's position and returns the new one. LARGE_INTEGER is
// passed by value in one register on the 64-bit targets glaze builds for.
func (i *iStream) seek(offset int64, origin uint32) (uint64, int32) {
	var pos uint64
	r, _, _ := purego.SyscallN(i.vtbl.Seek, uintptr(unsafe.Pointer(i)), uintptr(offset), uintptr(origin), uintptr(unsafe.Pointer(&pos))) // #nosec G115 -- offset is 0 at both call sites
	return pos, int32(r)                                                                                                                 // #nosec G115 -- an HRESULT is 32 bits
}

// readAll returns everything written to the stream.
func (i *iStream) readAll() ([]byte, error) {
	size, hr := i.seek(0, streamSeekEnd)
	if hr < 0 {
		return nil, fmt.Errorf("glaze: snapshot stream seek failed (HRESULT 0x%08X)", uint32(hr)) // #nosec G115 -- printing the HRESULT's bits
	}
	if size == 0 || size > math.MaxInt32 {
		return nil, fmt.Errorf("glaze: snapshot is empty or too large (%d bytes)", size)
	}
	_, hr = i.seek(0, streamSeekSet)
	if hr < 0 {
		return nil, fmt.Errorf("glaze: snapshot stream seek failed (HRESULT 0x%08X)", uint32(hr)) // #nosec G115 -- printing the HRESULT's bits
	}
	buf := make([]byte, size)
	var n uint32
	r, _, _ := purego.SyscallN(i.vtbl.Read, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(&buf[0])), uintptr(size), uintptr(unsafe.Pointer(&n)))
	hr = int32(r) // #nosec G115 -- an HRESULT is 32 bits
	if hr < 0 || uint64(n) != size {
		return nil, fmt.Errorf("glaze: snapshot stream read %d of %d bytes (HRESULT 0x%08X)", n, size, uint32(hr)) // #nosec G115 -- printing the HRESULT's bits
	}
	return buf, nil
}

func (i *iCoreWebView2) CapturePreview(format uint32, stream, handler uintptr) int32 {
	r, _, _ := purego.SyscallN(i.vtbl.CapturePreview, uintptr(unsafe.Pointer(i)), uintptr(format), stream, handler)
	return int32(r) // #nosec G115 -- an HRESULT is 32 bits
}

type snapshotResult struct {
	png []byte
	err error
}

// Snapshot captures the visible viewport with ICoreWebView2::CapturePreview,
// which encodes the PNG itself into an IStream, and returns those bytes.
// WebView2 captures at the controller's rasterization scale, so the pixels are
// device pixels; see Snapshotter for the contract.
func (w *webview) Snapshot() ([]byte, error) {
	// The completion handler is invoked from the message loop: waiting for it
	// on the UI thread would wait forever.
	if onUIThread() {
		return nil, errSnapshotUIThread
	}
	w.snapshotMu.Lock()
	defer w.snapshotMu.Unlock()

	done := make(chan snapshotResult, 1)
	w.Dispatch(func() { w.beginCapture(done) })
	select {
	case r := <-done:
		return r.png, r.err
	case <-time.After(snapshotTimeout):
		return nil, errSnapshotTimeout
	}
}

// beginCapture starts the capture; finishCapture, called from the completion
// handler, ends it. Both run on the UI thread.
func (w *webview) beginCapture(done chan snapshotResult) {
	if w.webview2 == 0 {
		done <- snapshotResult{err: errSnapshotNoWebView}
		return
	}
	// Only after a timed-out Snapshot whose capture is still running: there is
	// one completion handler and one stream slot per web view.
	if w.captureDone != nil {
		done <- snapshotResult{err: errors.New("glaze: a previous snapshot is still in progress")}
		return
	}
	var stream uintptr
	hr := createStreamOnHGlobal(0, 1, &stream) // 1: the stream frees its memory on Release
	if hr < 0 || stream == 0 {
		done <- snapshotResult{err: fmt.Errorf("glaze: CreateStreamOnHGlobal failed (HRESULT 0x%08X)", uint32(hr))} // #nosec G115 -- printing the HRESULT's bits
		return
	}
	if w.captureH == nil {
		w.captureH = newHandler(w.id, kindCapture, &iidCapturePreviewCompleted)
	}
	hr = asWebView2(w.webview2).CapturePreview(capturePreviewImageFormatPNG, stream, handlerPtr(w.captureH))
	if hr < 0 {
		asStream(stream).Release()
		done <- snapshotResult{err: fmt.Errorf("glaze: CapturePreview failed (HRESULT 0x%08X)", uint32(hr))} // #nosec G115 -- printing the HRESULT's bits
		return
	}
	w.captureStream, w.captureDone = stream, done
}

func (w *webview) finishCapture(hr int32) {
	stream, done := w.captureStream, w.captureDone
	w.captureStream, w.captureDone = 0, nil
	if done == nil {
		return
	}
	defer asStream(stream).Release()
	if hr < 0 {
		done <- snapshotResult{err: fmt.Errorf("glaze: snapshot failed (HRESULT 0x%08X)", uint32(hr))} // #nosec G115 -- printing the HRESULT's bits
		return
	}
	data, err := asStream(stream).readAll()
	done <- snapshotResult{png: data, err: err}
}
