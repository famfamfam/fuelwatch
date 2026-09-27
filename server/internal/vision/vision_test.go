package vision

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"strings"
	"testing"

	"fuelwatch/internal/vision/llm"
	"fuelwatch/internal/zones"
)

func solid(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, 255
	}
	return img
}

func decode(t *testing.T, b []byte) image.Image {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

var testZones = []zones.Zone{
	{Type: zones.Monitor, Points: [][2]float64{{0.5, 0.5}, {0.9, 0.5}, {0.9, 0.9}, {0.5, 0.9}}},
	{Type: zones.Ignore, Points: [][2]float64{{0.5, 0.5}, {0.6, 0.5}, {0.6, 0.6}, {0.5, 0.6}}},
}

func images(parts []llm.Part) (out [][]byte) {
	for _, p := range parts {
		if p.ImageJPEG != nil {
			out = append(out, p.ImageJPEG)
		}
	}
	return out
}

func TestBuildPartsCropsAroundMonitor(t *testing.T) {
	frame := solid(1920, 1080, color.RGBA{200, 200, 200, 255})
	ref := solid(1920, 1080, color.RGBA{200, 200, 200, 255})
	parts, err := BuildParts(ClassifyInput{Frame: frame, Reference: ref, Zones: testZones}, 0.1, 1024)
	if err != nil {
		t.Fatal(err)
	}
	imgs := images(parts)
	if len(imgs) != 2 {
		t.Fatalf("want reference+current, got %d images", len(imgs))
	}
	// MONITOR 0.5..0.9 + запас 0.1 → 0.4..1.0: 1152×648 px
	cur := decode(t, imgs[1])
	if b := cur.Bounds(); b.Dx() != 1024 || b.Dy() != 576 {
		t.Errorf("current crop scaled to %v, want 1024x576", b.Size())
	}
	// контур MONITOR зелёный: левый верхний угол зоны в вырезе — (0.1/0.6, 0.1/0.6) от размера
	x, y := 1024/6, 576/6+30
	r, g, b, _ := cur.At(x, y).RGBA()
	if !(g>>8 > 120 && r>>8 < 90 && b>>8 < 120) {
		t.Errorf("no green outline at %d,%d: %d %d %d", x, y, r>>8, g>>8, b>>8)
	}
	if !strings.Contains(parts[0].Text, "REFERENCE") || !strings.Contains(parts[len(parts)-1].Text, "бензовоз") {
		t.Errorf("unexpected text parts: %q … %q", parts[0].Text, parts[len(parts)-1].Text)
	}
}

func TestBuildPartsKeyframeAddsFullView(t *testing.T) {
	frame := solid(1920, 1080, color.RGBA{100, 100, 100, 255})
	parts, err := BuildParts(ClassifyInput{Frame: frame, Reference: frame, Zones: testZones, Keyframe: true}, 0.1, 1024)
	if err != nil {
		t.Fatal(err)
	}
	imgs := images(parts)
	if len(imgs) != 4 {
		t.Fatalf("keyframe: want 4 images, got %d", len(imgs))
	}
	if b := decode(t, imgs[3]).Bounds(); b.Dx() != 768 {
		t.Errorf("full view width %d, want 768", b.Dx())
	}
}

func TestBuildPartsUsesPhoneCrop(t *testing.T) {
	crop := solid(600, 400, color.RGBA{50, 50, 50, 255})
	r := Rect{0.4, 0.4, 1, 1}
	parts, err := BuildParts(ClassifyInput{Frame: crop, CropRect: &r, Zones: testZones}, 0.1, 1024)
	if err != nil {
		t.Fatal(err)
	}
	imgs := images(parts)
	if len(imgs) != 1 || !strings.Contains(parts[0].Text, "нет") {
		t.Fatalf("no reference: want 1 image and a note, got %d images, %q", len(imgs), parts[0].Text)
	}
	if b := decode(t, imgs[0]).Bounds(); b.Dx() != 600 {
		t.Errorf("phone crop must not be cropped again: width %d", b.Dx())
	}
}

func TestParseObservation(t *testing.T) {
	o, err := ParseObservation("```json\n{\"tanker_present\":true,\"confidence\":1.7,\"tanker_in_zone\":true,\"note\":\"бензовоз у колонки\"}\n```")
	if err != nil {
		t.Fatal(err)
	}
	if !o.TankerPresent || o.Confidence != 1 || !o.ViewMatchesReference {
		t.Errorf("unexpected %+v", o)
	}
	if _, err := ParseObservation(`{"note":"нет полей"}`); err == nil {
		t.Error("missing fields must fail")
	}
	if _, err := ParseObservation(`not json`); err == nil {
		t.Error("garbage must fail")
	}
	if _, err := ParseObservation(`{"tanker_present":"yes","confidence":0.5,"tanker_in_zone":true}`); err == nil {
		t.Error("wrong types must fail")
	}
}
