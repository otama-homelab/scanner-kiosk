//go:build ignore

package main

import (
	"encoding/json"
	"os"
	"scanner-kiosk/internal/correction"
	"scanner-kiosk/internal/evaluation"
)

func main() {
	var in struct {
		Regions []struct {
			SourceID string                        `json:"source_id"`
			Text     []evaluation.CorrectionRegion `json:"text_regions"`
			Lines    []evaluation.CorrectionRegion `json:"thin_line_regions"`
		} `json:"regions"`
	}
	if e := json.NewDecoder(os.Stdin).Decode(&in); e != nil {
		panic(e)
	}
	t := correction.Transform{CropQuad: [4]correction.Point{{X: 0, Y: 0}, {X: 900, Y: 0}, {X: 900, Y: 700}, {X: 0, Y: 700}}, OutputWidth: 512, OutputHeight: 512, ConfidenceKind: "unavailable"}
	out := []map[string]any{}
	for _, c := range in.Regions {
		for _, group := range []struct {
			Name   string
			Values []evaluation.CorrectionRegion
		}{{"text", c.Text}, {"thin-line", c.Lines}} {
			for i, x := range group.Values {
				a, ok := correction.MapSourcePoint(correction.Point{X: float64(x.X), Y: float64(x.Y)}, t)
				if !ok {
					panic("source region start")
				}
				z, ok := correction.MapSourcePoint(correction.Point{X: float64(x.X + x.Width), Y: float64(x.Y + x.Height)}, t)
				if !ok {
					panic("source region end")
				}
				out = append(out, map[string]any{"source_id": c.SourceID, "kind": group.Name, "index": i, "source_region": x, "mapped_crop_box": [4]float64{a.X, a.Y, z.X, z.Y}})
			}
		}
	}
	if e := json.NewEncoder(os.Stdout).Encode(out); e != nil {
		panic(e)
	}
}
