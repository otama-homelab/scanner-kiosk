package correction

import (
	"encoding/json"
	"image"
	"math"
	"testing"
)

func TestCorrectionTransform(t *testing.T) {
	quad, _ := json.Marshal([][2]int{{0, 0}, {100, 0}, {100, 200}, {0, 200}})
	parameters := map[string]string{
		"crop_quad": string(quad), "rotation_degrees": "0",
		"output_width": "100", "output_height": "200",
		"confidence_kind": "unavailable", "fallback_reason": "",
	}
	transform, err := Parse(parameters, image.Rect(0, 0, 100, 200))
	if err != nil || transform.OutputWidth != 100 || transform.ConfidenceKind != "unavailable" {
		t.Fatalf("valid transform rejected: %+v %v", transform, err)
	}
	for name, mutate := range map[string]func(map[string]string){
		"nan":       func(p map[string]string) { p["rotation_degrees"] = "NaN" },
		"reverse":   func(p map[string]string) { p["crop_quad"] = "[[0,0],[0,200],[100,200],[100,0]]" },
		"outside":   func(p map[string]string) { p["crop_quad"] = "[[-1,0],[100,0],[100,200],[0,200]]" },
		"dimension": func(p map[string]string) { p["output_width"] = "0" },
		"confidence": func(p map[string]string) {
			p["confidence_kind"] = "ratio"
		},
	} {
		t.Run(name, func(t *testing.T) {
			copy := map[string]string{}
			for key, value := range parameters {
				copy[key] = value
			}
			mutate(copy)
			if _, err := Parse(copy, image.Rect(0, 0, 100, 200)); err == nil {
				t.Fatal("invalid transform accepted")
			}
		})
	}
}

func TestCorrectionCanonicalMapping(t *testing.T) {
	quad := [4]Point{{X: 10, Y: 20}, {X: 110, Y: 20}, {X: 110, Y: 220}, {X: 10, Y: 220}}
	transform := Transform{CropQuad: quad, OutputWidth: 100, OutputHeight: 200}
	for source, want := range map[Point]Point{
		{X: 10, Y: 20}:   {X: 0, Y: 0},
		{X: 110, Y: 20}:  {X: 100, Y: 0},
		{X: 110, Y: 220}: {X: 100, Y: 200},
		{X: 10, Y: 220}:  {X: 0, Y: 200},
		{X: 60, Y: 120}:  {X: 50, Y: 100},
	} {
		got, ok := MapSourcePoint(source, transform)
		if !ok || math.Abs(got.X-want.X) > 1e-6 || math.Abs(got.Y-want.Y) > 1e-6 {
			t.Fatalf("map %+v = %+v, %v; want %+v; transform=%+v valid=%v", source, got, ok, want, transform, ValidQuad(transform.CropQuad))
		}
	}

	perspective := Transform{
		CropQuad:    [4]Point{{X: 10, Y: 20}, {X: 120, Y: 30}, {X: 100, Y: 230}, {X: 0, Y: 200}},
		OutputWidth: 100, OutputHeight: 200,
	}
	got, ok := MapSourcePoint(Point{X: 57.5, Y: 120}, perspective)
	if !ok || math.Abs(got.X-50) > 1e-6 || math.Abs(got.Y-100) > 1e-6 {
		t.Fatalf("perspective center mapped to %+v, %v", got, ok)
	}

	for _, test := range []struct {
		name     string
		rotation float64
		want     Point
	}{
		{name: "positive counter-clockwise", rotation: 45, want: Point{X: 50 - 10/math.Sqrt2, Y: 50 + 10/math.Sqrt2}},
		{name: "negative clockwise", rotation: -45, want: Point{X: 50 - 10/math.Sqrt2, Y: 50 - 10/math.Sqrt2}},
	} {
		t.Run(test.name, func(t *testing.T) {
			rotated := Transform{
				CropQuad:    [4]Point{{X: 0, Y: 0}, {X: 100, Y: 0}, {X: 100, Y: 100}, {X: 0, Y: 100}},
				OutputWidth: 100, OutputHeight: 100, RotationDegrees: test.rotation,
			}
			got, ok := MapSourcePoint(Point{X: 40, Y: 50}, rotated)
			if !ok || math.Abs(got.X-test.want.X) > 1e-6 || math.Abs(got.Y-test.want.Y) > 1e-6 {
				t.Fatalf("rotated point mapped to %+v, %v; want %+v", got, ok, test.want)
			}
		})
	}

	if _, ok := MapSourcePoint(Point{X: 5, Y: 20}, transform); ok {
		t.Fatal("point outside the crop quad mapped successfully")
	}
}
