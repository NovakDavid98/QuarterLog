package capture

import (
	"image"
	"os"

	// register PNG decoder
	_ "image/png"
)

// ThumbFromFile reads an image file and returns a downscaled JPEG data URI.
// Returns an empty string (no error) when the path is empty.
func ThumbFromFile(path string, width int) (string, error) {
	if path == "" {
		return "", nil
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return "", err
	}
	return jpegDataURI(resizeToWidth(img, width), 85)
}

// UploadFromFile reads an image file and returns a downscaled JPEG data URI
// suitable for the vision API.
func UploadFromFile(path string, width int) (string, error) {
	if path == "" {
		return "", nil
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return "", err
	}
	return jpegDataURI(resizeToWidth(img, width), 80)
}
