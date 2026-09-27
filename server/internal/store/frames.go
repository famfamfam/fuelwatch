package store

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"os"
	"path/filepath"
	"regexp"
	"time"

	xdraw "golang.org/x/image/draw"
)

const (
	thumbWidth   = 320
	fullQuality  = 90
	thumbQuality = 80
)

var frameIDRe = regexp.MustCompile(`^[0-9A-Za-z-]{8,64}$`)

func ValidFrameID(id string) bool { return frameIDRe.MatchString(id) }

type SavedFrame struct {
	Path, ThumbPath string // относительно FramesDir
	Width, Height   int
}

// SaveFrame поворачивает JPEG по rotation (0/90/180/270, по часовой), сохраняет полный кадр и миниатюру.
func SaveFrame(dir, deviceID, frameID string, takenAt time.Time, data []byte, rotation int) (SavedFrame, error) {
	if !ValidFrameID(frameID) {
		return SavedFrame{}, fmt.Errorf("bad frame id")
	}
	src, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return SavedFrame{}, fmt.Errorf("decode jpeg: %w", err)
	}
	img := src
	full := data
	if rotation != 0 {
		img = rotate(src, rotation)
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: fullQuality}); err != nil {
			return SavedFrame{}, err
		}
		full = buf.Bytes()
	}

	b := img.Bounds()
	tw := min(thumbWidth, b.Dx())
	th := max(1, b.Dy()*tw/b.Dx())
	thumb := image.NewRGBA(image.Rect(0, 0, tw, th))
	xdraw.ApproxBiLinear.Scale(thumb, thumb.Bounds(), img, b, xdraw.Src, nil)
	var tbuf bytes.Buffer
	if err := jpeg.Encode(&tbuf, thumb, &jpeg.Options{Quality: thumbQuality}); err != nil {
		return SavedFrame{}, err
	}

	rel := filepath.Join(deviceID, takenAt.UTC().Format("2006-01-02"))
	if err := os.MkdirAll(filepath.Join(dir, rel), 0o755); err != nil {
		return SavedFrame{}, err
	}
	out := SavedFrame{
		Path:      filepath.ToSlash(filepath.Join(rel, frameID+".jpg")),
		ThumbPath: filepath.ToSlash(filepath.Join(rel, frameID+"_t.jpg")),
		Width:     b.Dx(), Height: b.Dy(),
	}
	if err := os.WriteFile(filepath.Join(dir, out.Path), full, 0o644); err != nil {
		return SavedFrame{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, out.ThumbPath), tbuf.Bytes(), 0o644); err != nil {
		return SavedFrame{}, err
	}
	return out, nil
}

func RemoveFrameFiles(dir string, paths ...string) {
	for _, p := range paths {
		if p != "" {
			os.Remove(filepath.Join(dir, filepath.FromSlash(p)))
		}
	}
}

// rotate поворачивает изображение по часовой стрелке на 90/180/270.
func rotate(src image.Image, deg int) image.Image {
	sb := src.Bounds()
	in := image.NewRGBA(image.Rect(0, 0, sb.Dx(), sb.Dy()))
	draw.Draw(in, in.Bounds(), src, sb.Min, draw.Src)
	w, h := sb.Dx(), sb.Dy()

	var out *image.RGBA
	if deg == 180 {
		out = image.NewRGBA(image.Rect(0, 0, w, h))
	} else {
		out = image.NewRGBA(image.Rect(0, 0, h, w))
	}
	for y := 0; y < h; y++ {
		row := in.Pix[y*in.Stride : y*in.Stride+w*4]
		for x := 0; x < w; x++ {
			var nx, ny int
			switch deg {
			case 90:
				nx, ny = h-1-y, x
			case 180:
				nx, ny = w-1-x, h-1-y
			case 270:
				nx, ny = y, w-1-x
			default:
				nx, ny = x, y
			}
			o := ny*out.Stride + nx*4
			copy(out.Pix[o:o+4], row[x*4:x*4+4])
		}
	}
	return out
}
