package capture

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"

	"github.com/kbinani/screenshot"
	xdraw "golang.org/x/image/draw"
)

// Monitor selection sentinels (match config.Monitor semantics).
const (
	MonitorPrimary = -1
	MonitorAll     = -2
)

// Result is the outcome of a screen capture.
type Result struct {
	PNGPath   string // full-resolution PNG written to disk
	ThumbB64  string // small JPEG data URI for the UI preview
	UploadB64 string // downscaled JPEG data URI for the vision API
}

// Capture grabs the screen for the given monitor selection, writes a full PNG to
// pngPath, and returns downscaled encodings for preview and upload.
func Capture(monitor int, pngPath string) (*Result, error) {
	img, err := grab(monitor)
	if err != nil {
		return nil, err
	}

	if err := writePNG(pngPath, img); err != nil {
		return nil, err
	}

	thumb, err := jpegDataURI(resizeToWidth(img, 900), 85)
	if err != nil {
		return nil, err
	}
	upload, err := jpegDataURI(resizeToWidth(img, 1280), 80)
	if err != nil {
		return nil, err
	}

	return &Result{PNGPath: pngPath, ThumbB64: thumb, UploadB64: upload}, nil
}

func grab(monitor int) (*image.RGBA, error) {
	n := screenshot.NumActiveDisplays()
	if n == 0 {
		return nil, fmt.Errorf("no active displays found")
	}

	switch monitor {
	case MonitorAll:
		return screenshot.CaptureRect(screenshot.GetDisplayBounds(0).Union(unionAll(n)))
	case MonitorPrimary:
		return screenshot.CaptureDisplay(0)
	default:
		if monitor < 0 || monitor >= n {
			monitor = 0
		}
		return screenshot.CaptureDisplay(monitor)
	}
}

func unionAll(n int) image.Rectangle {
	r := screenshot.GetDisplayBounds(0)
	for i := 1; i < n; i++ {
		r = r.Union(screenshot.GetDisplayBounds(i))
	}
	return r
}

func resizeToWidth(src image.Image, maxW int) image.Image {
	b := src.Bounds()
	if b.Dx() <= maxW {
		return src
	}
	h := b.Dy() * maxW / b.Dx()
	dst := image.NewRGBA(image.Rect(0, 0, maxW, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Over, nil)
	return dst
}

func jpegDataURI(img image.Image, quality int) (string, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return "", err
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
