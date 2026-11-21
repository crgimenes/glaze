//go:build darwin || linux || windows

package core

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var resSnapshot atomic.Value // string

// snapshotScenario loads a page with two known colors, lets it paint, and
// snapshots it. The page reports its viewport and devicePixelRatio, so the
// test can hold the image to the documented size: device pixels.
func snapshotScenario() string {
	var w WebView
	result := make(chan string, 1)
	page := `<!DOCTYPE html><html><body style="margin:0;background:#4a90d9">
<div style="width:200px;height:120px;background:#e8b04b"></div><script>
addEventListener('load', function() {
  new Image().src = 'loaded?' + [innerWidth, innerHeight, devicePixelRatio].join(',');
});
</script></body></html>`
	var once sync.Once
	handler := func(req *SchemeRequest) *SchemeResponse {
		_, viewport, found := strings.Cut(req.URL, "loaded?")
		if !found {
			return &SchemeResponse{Body: []byte(page), MIMEType: "text/html"}
		}
		once.Do(func() {
			go func() {
				defer w.Terminate()
				time.Sleep(900 * time.Millisecond) // loaded is not yet painted
				result <- captureForTest(w, viewport)
			}()
		})
		return &SchemeResponse{}
	}

	w, err := NewWithOptions(Options{
		// "app", like every scheme in the Windows test process: it registers
		// one scheme set (see newEnvOptions).
		SchemeHandlers: map[string]SchemeHandler{"app": handler},
	})
	if err != nil {
		return "new error: " + err.Error()
	}
	defer w.Destroy()
	w.SetSize(480, 320, HintNone)
	defer time.AfterFunc(40*time.Second, w.Terminate).Stop()
	w.Navigate("app://test/index.html")
	w.Run()

	select {
	case r := <-result:
		return r
	default:
		return "no result: the page never reported its load"
	}
}

// captureForTest runs off the UI thread and summarizes the capture for
// TestSnapshot.
func captureForTest(w WebView, viewport string) string {
	s, ok := w.(Snapshotter)
	if !ok {
		return "not a Snapshotter"
	}
	// Linux and Windows refuse a call from the UI thread at once. macOS has no
	// such check, only its timeout, which is too slow to exercise here.
	if runtime.GOOS != "darwin" {
		refused := make(chan error, 1)
		w.Dispatch(func() {
			_, err := s.Snapshot()
			refused <- err
		})
		err := <-refused
		if !errors.Is(err, errSnapshotUIThread) {
			return "on the UI thread: error = " + errString(err)
		}
	}
	data, err := s.Snapshot()
	if err != nil {
		return "error: " + err.Error()
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return "decode: " + err.Error()
	}
	return fmt.Sprintf("w=%d h=%d colors=%d bytes=%d viewport=%s",
		img.Bounds().Dx(), img.Bounds().Dy(), countColors(img), len(data), viewport)
}

func countColors(img image.Image) int {
	seen := map[uint32]struct{}{}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y += 4 {
		for x := b.Min.X; x < b.Max.X; x += 4 {
			r, g, bl, _ := img.At(x, y).RGBA()
			seen[r>>8<<16|g>>8<<8|bl>>8] = struct{}{}
			if len(seen) > 64 {
				return len(seen)
			}
		}
	}
	return len(seen)
}

func TestSnapshot(t *testing.T) {
	got, _ := resSnapshot.Load().(string)
	requireGUI(t, got)
	var w, h, colors, n, vw, vh int
	var dpr float64
	_, err := fmt.Sscanf(got, "w=%d h=%d colors=%d bytes=%d viewport=%d,%d,%g", &w, &h, &colors, &n, &vw, &vh, &dpr)
	if err != nil {
		t.Fatalf("snapshot: %s", got)
	}
	t.Logf("snapshot: %s", got)
	// A blank frame is the failure that matters on a composited surface, so
	// "returned some bytes" is not enough: the two page colors must be there.
	if colors < 2 {
		t.Fatalf("capture is blank (%d color): %s", colors, got)
	}
	// Device pixels, as Snapshotter documents: logical viewport x scale.
	wantW, wantH := int(math.Round(float64(vw)*dpr)), int(math.Round(float64(vh)*dpr))
	if abs(w-wantW) > 1 || abs(h-wantH) > 1 {
		t.Fatalf("image is %dx%d, want %dx%d (viewport %dx%d at devicePixelRatio %g)", w, h, wantW, wantH, vw, vh, dpr)
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
