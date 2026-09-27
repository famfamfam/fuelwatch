package vision

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"math"

	xdraw "golang.org/x/image/draw"

	"fuelwatch/internal/zones"
)

// Rect — прямоугольник в нормализованных координатах [0..1] полного повёрнутого кадра: x0, y0, x1, y1.
type Rect [4]float64

var (
	monitorColor = color.RGBA{22, 163, 74, 255}
	ignoreColor  = color.RGBA{128, 128, 128, 170}
)

const (
	outlineWidth  = 3
	jpegQuality   = 85
	fullViewSide  = 768
	minCropPixels = 8
)

// cropNorm вырезает из img область r (нормализованно к img).
func cropNorm(img image.Image, r Rect) image.Image {
	b := img.Bounds()
	rect := image.Rect(
		b.Min.X+int(math.Floor(r[0]*float64(b.Dx()))), b.Min.Y+int(math.Floor(r[1]*float64(b.Dy()))),
		b.Min.X+int(math.Ceil(r[2]*float64(b.Dx()))), b.Min.Y+int(math.Ceil(r[3]*float64(b.Dy()))),
	).Intersect(b)
	if rect.Dx() < minCropPixels || rect.Dy() < minCropPixels {
		return img
	}
	out := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(out, out.Bounds(), img, rect.Min, draw.Src)
	return out
}

func toRGBA(img image.Image) *image.RGBA {
	if r, ok := img.(*image.RGBA); ok {
		return r
	}
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), img, b.Min, draw.Src)
	return out
}

// drawZones рисует зоны на изображении, которое показывает область view полного кадра:
// MONITOR — зелёный контур, IGNORE — полупрозрачная серая заливка.
func drawZones(img *image.RGBA, zs []zones.Zone, view Rect) {
	w, h := float64(img.Bounds().Dx()), float64(img.Bounds().Dy())
	project := func(p [2]float64) (float64, float64) {
		return (p[0] - view[0]) / (view[2] - view[0]) * w, (p[1] - view[1]) / (view[3] - view[1]) * h
	}
	for _, z := range zs {
		if z.Type != zones.Ignore {
			continue
		}
		pts := make([][2]float64, len(z.Points))
		for i, p := range z.Points {
			pts[i][0], pts[i][1] = project(p)
		}
		fillPolygon(img, pts, ignoreColor)
	}
	for _, z := range zs {
		if z.Type != zones.Monitor {
			continue
		}
		for i := range z.Points {
			x0, y0 := project(z.Points[i])
			x1, y1 := project(z.Points[(i+1)%len(z.Points)])
			drawLine(img, x0, y0, x1, y1, monitorColor)
		}
	}
}

// fillPolygon — построчная заливка (even-odd) с альфа-смешиванием.
func fillPolygon(img *image.RGBA, pts [][2]float64, c color.RGBA) {
	b := img.Bounds()
	minY, maxY := math.Inf(1), math.Inf(-1)
	for _, p := range pts {
		minY, maxY = math.Min(minY, p[1]), math.Max(maxY, p[1])
	}
	a := float64(c.A) / 255
	for y := max(b.Min.Y, int(minY)); y <= min(b.Max.Y-1, int(maxY)); y++ {
		fy := float64(y) + 0.5
		var xs []float64
		for i := range pts {
			p, q := pts[i], pts[(i+1)%len(pts)]
			if (p[1] > fy) != (q[1] > fy) {
				xs = append(xs, p[0]+(fy-p[1])/(q[1]-p[1])*(q[0]-p[0]))
			}
		}
		sortFloats(xs)
		for i := 0; i+1 < len(xs); i += 2 {
			for x := max(b.Min.X, int(math.Ceil(xs[i]-0.5))); x <= min(b.Max.X-1, int(xs[i+1]-0.5)); x++ {
				o := img.PixOffset(x, y)
				for k, v := range [3]uint8{c.R, c.G, c.B} {
					img.Pix[o+k] = uint8(float64(img.Pix[o+k])*(1-a) + float64(v)*a)
				}
			}
		}
	}
}

func sortFloats(xs []float64) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j] < xs[j-1]; j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
}

// drawLine — отрезок толщиной outlineWidth (квадратная кисть по точкам отрезка).
func drawLine(img *image.RGBA, x0, y0, x1, y1 float64, c color.RGBA) {
	steps := int(math.Max(math.Abs(x1-x0), math.Abs(y1-y0))) + 1
	half := outlineWidth / 2
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		cx, cy := int(x0+(x1-x0)*t), int(y0+(y1-y0)*t)
		for dy := -half; dy <= half; dy++ {
			for dx := -half; dx <= half; dx++ {
				if (image.Point{cx + dx, cy + dy}).In(img.Bounds()) {
					img.SetRGBA(cx+dx, cy+dy, c)
				}
			}
		}
	}
}

// fit уменьшает изображение так, чтобы большая сторона была не больше maxSide.
func fit(img image.Image, maxSide int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if max(w, h) <= maxSide {
		return img
	}
	k := float64(maxSide) / float64(max(w, h))
	out := image.NewRGBA(image.Rect(0, 0, max(1, int(float64(w)*k)), max(1, int(float64(h)*k))))
	xdraw.CatmullRom.Scale(out, out.Bounds(), img, b, xdraw.Src, nil)
	return out
}

func encodeJPEG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality})
	return buf.Bytes(), err
}
