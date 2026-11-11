//go:build ignore

package main

import (
	"encoding/json"
	"image"
	"os"
	"scanner-kiosk/internal/correction"
	"scanner-kiosk/internal/evaluation"
	"scanner-kiosk/pkg/ocrplugin"
)

type Item struct {
	ID        string                   `json:"id"`
	Oracle    evaluation.OCRPageOracle `json:"oracle"`
	Text      string                   `json:"text"`
	Positions ocrplugin.PositionedPage `json:"positions"`
}

func main() {
	var items []Item
	if e := json.NewDecoder(os.Stdin).Decode(&items); e != nil {
		panic(e)
	}
	out := []map[string]any{}
	for _, x := range items {
		src := image.Rect(0, 0, x.Oracle.Width, x.Oracle.Height)
		t := correction.Transform{CropQuad: [4]correction.Point{{X: 0, Y: 0}, {X: float64(src.Dx()), Y: 0}, {X: float64(src.Dx()), Y: float64(src.Dy())}, {X: 0, Y: float64(src.Dy())}}, OutputWidth: x.Positions.Width, OutputHeight: x.Positions.Height, ConfidenceKind: "unavailable"}
		for i, s := range x.Oracle.Spans {
			a, ok := correction.MapSourcePoint(correction.Point{X: s.Box[0], Y: s.Box[1]}, t)
			if !ok {
				panic("invalid source top-left")
			}
			z, ok := correction.MapSourcePoint(correction.Point{X: s.Box[0] + s.Box[2], Y: s.Box[1] + s.Box[3]}, t)
			if !ok {
				panic("invalid source bottom-right")
			}
			x.Oracle.Spans[i].Box = [4]float64{a.X, a.Y, z.X - a.X, z.Y - a.Y}
		}
		x.Oracle.Width = x.Positions.Width
		x.Oracle.Height = x.Positions.Height
		q, e := evaluation.AnalyzeOCRPage(x.Oracle, x.Text, x.Positions)
		if e != nil {
			panic(e)
		}
		out = append(out, map[string]any{"id": x.ID, "mapped_oracle": x.Oracle, "quality": q})
	}
	if e := json.NewEncoder(os.Stdout).Encode(out); e != nil {
		panic(e)
	}
}
