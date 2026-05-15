//go:build integration

package integration

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"
)

// UniquePNG produces a 128x128 PNG whose pixel color is derived from the
// current millisecond timestamp. Each call returns content-unique bytes so
// Mattermost will accept it as a new profile image and advance
// last_picture_update. Replaces build/gen-unique-png.py.
func UniquePNG(t *testing.T) []byte {
	t.Helper()
	const size = 128
	// Mask is < 2^24 so the conversions below cannot overflow.
	ts := time.Now().UnixMilli() & 0xFFFFFF
	c := color.RGBA{
		R: byte(ts & 0xFF),
		G: byte((ts >> 8) & 0xFF),
		B: byte((ts >> 16) & 0xFF),
		A: 0xFF,
	}
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}
