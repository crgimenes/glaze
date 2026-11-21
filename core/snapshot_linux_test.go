package core

import (
	"bytes"
	"testing"
)

// The conversion is the one part of the Linux snapshot that runs without a
// display: channel order, row padding (stride wider than the row), and the
// undefined alpha byte of RGB24.
func TestARGB32ToRGBA(t *testing.T) {
	const pad = 0xEE
	// 2x2, stride 12: each row is two B,G,R,A pixels then four padding bytes.
	src := []byte{
		0x01, 0x02, 0x03, 0xFF, 0x10, 0x20, 0x30, 0x80, pad, pad, pad, pad,
		0x00, 0x00, 0xFF, 0xFF, 0x0A, 0x0B, 0x0C, 0x00, pad, pad, pad, pad,
	}
	want := []byte{
		0x03, 0x02, 0x01, 0xFF, 0x30, 0x20, 0x10, 0x80,
		0xFF, 0x00, 0x00, 0xFF, 0x0C, 0x0B, 0x0A, 0x00,
	}
	img := argb32ToRGBA(src, 2, 2, 12, false)
	if !bytes.Equal(img.Pix, want) {
		t.Fatalf("ARGB32: got % X, want % X", img.Pix, want)
	}

	opaque := argb32ToRGBA(src, 2, 2, 12, true)
	for i := 3; i < len(opaque.Pix); i += 4 {
		if opaque.Pix[i] != 0xFF {
			t.Fatalf("RGB24: alpha at %d = %#x, want 0xFF", i, opaque.Pix[i])
		}
	}
	if opaque.Pix[4] != 0x30 || opaque.Pix[5] != 0x20 || opaque.Pix[6] != 0x10 {
		t.Fatalf("RGB24: color changed: % X", opaque.Pix[4:7])
	}
}
