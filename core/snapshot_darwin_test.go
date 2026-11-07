//go:build darwin

package core

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var resSnapshot_ atomic.Value // string

// snapshotScenario loads a page with two known colours, waits for it to paint,
// and snapshots it. The interesting question is not "does it return bytes" but
// "are the pixels real": a blank capture is the failure mode that matters.
func snapshotScenario() string {
	page := `<html><body style="margin:0;background:#4a90d9">` +
		`<div style="width:200px;height:120px;background:#e8b04b"></div></body></html>`

	var out atomicString
	var w WebView
	var err error
	w, err = NewWithOptions(Options{
		SchemeHandlers: map[string]SchemeHandler{"shot": func(*SchemeRequest) *SchemeResponse {
			return &SchemeResponse{Body: []byte(page), MIMEType: "text/html"}
		}},
		OnNavigation: func(NavigationEvent) {
			go func() {
				defer w.Terminate()
				time.Sleep(900 * time.Millisecond) // let WebKit paint
				s, ok := w.(Snapshotter)
				if !ok {
					out.set("not a Snapshotter")
					return
				}
				data, err := s.Snapshot()
				if err != nil {
					out.set("error: " + err.Error())
					return
				}
				img, err := png.Decode(bytes.NewReader(data))
				if err != nil {
					out.set("decode: " + err.Error())
					return
				}
				out.set(fmt.Sprintf("w=%d h=%d colors=%d bytes=%d",
					img.Bounds().Dx(), img.Bounds().Dy(), countColors(img), len(data)))
			}()
		},
	})
	if err != nil {
		return "new error: " + err.Error()
	}
	defer w.Destroy()
	w.Navigate("shot://test/")
	w.Run()
	return out.get()
}

func countColors(img image.Image) int {
	seen := map[uint32]struct{}{}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y += 4 {
		for x := b.Min.X; x < b.Max.X; x += 4 {
			r, g, bl, _ := img.At(x, y).RGBA()
			seen[uint32(r>>8)<<16|uint32(g>>8)<<8|uint32(bl>>8)] = struct{}{}
			if len(seen) > 64 {
				return len(seen)
			}
		}
	}
	return len(seen)
}

func TestSnapshot(t *testing.T) {
	got, _ := resSnapshot_.Load().(string)
	requireGUI(t, got)
	if !strings.HasPrefix(got, "w=") {
		t.Fatalf("snapshot: %s", got)
	}
	var w, h, colors, n int
	if _, err := fmt.Sscanf(got, "w=%d h=%d colors=%d bytes=%d", &w, &h, &colors, &n); err != nil {
		t.Fatalf("unparsable result %q: %v", got, err)
	}
	if w == 0 || h == 0 || n == 0 {
		t.Fatalf("empty capture: %s", got)
	}
	t.Logf("snapshot: %s", got)
	if colors < 2 {
		t.Fatalf("capture is blank (%d colour): %s", colors, got)
	}
}

// atomicString is a tiny atomic.Value wrapper with a zero value.
type atomicString struct{ v atomic.Value }

func (a *atomicString) set(s string) { a.v.Store(s) }

func (a *atomicString) get() string {
	if v, ok := a.v.Load().(string); ok {
		return v
	}
	return ""
}
