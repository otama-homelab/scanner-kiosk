package correction

import (
	"encoding/json"
	"image"
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
