package art

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// Set INPUTGUARD_ART_PREVIEW to a folder to save the icons as PNGs to look at.
func TestIcons(t *testing.T) {
	dir := os.Getenv("INPUTGUARD_ART_PREVIEW")
	for _, size := range []int{16, 20, 24, 32, 48, 256} {
		images := map[string]*image.NRGBA{
			"brand":    BrandMark(size),
			"unlocked": TrayIcon(size, false),
			"locked":   TrayIcon(size, true),
		}
		for name, img := range images {
			if img.Bounds().Dx() != size || img.Bounds().Dy() != size {
				t.Fatalf("%s %d: wrong size", name, size)
			}
			corner, center := img.NRGBAAt(0, 0), img.NRGBAAt(size/2, size/2)
			if corner.A != 0 || center.A != 255 {
				t.Errorf("%s %d: corners must be clear and the middle solid (%v, %v)", name, size, corner, center)
			}
			if dir != "" {
				f, err := os.Create(filepath.Join(dir, name+"-"+itoa(size)+".png"))
				if err != nil {
					t.Fatal(err)
				}
				png.Encode(f, img)
				f.Close()
			}
		}
		if TrayIcon(size, true).NRGBAAt(size/8, size/2) == TrayIcon(size, false).NRGBAAt(size/8, size/2) {
			t.Errorf("%d: locked and unlocked tray icons must differ", size)
		}
	}
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}
