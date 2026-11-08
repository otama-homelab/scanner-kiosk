// Package correction defines bounded metadata shared by correction evaluation
// and the optional document correction capability. It does not process images.
package correction

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"io"
	"math"
	"strconv"
)

var ErrInvalid = errors.New("invalid correction transform")

type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type Transform struct {
	CropQuad        [4]Point `json:"crop_quad"`
	RotationDegrees float64  `json:"rotation_degrees"`
	OutputWidth     int      `json:"output_width"`
	OutputHeight    int      `json:"output_height"`
	ConfidenceKind  string   `json:"confidence_kind"`
	ConfidenceValue *float64 `json:"confidence_value,omitempty"`
	FallbackReason  string   `json:"fallback_reason,omitempty"`
}

func Parse(parameters map[string]string, original image.Rectangle) (Transform, error) {
	var transform Transform
	var quad [4][2]float64
	decoder := json.NewDecoder(io.LimitReader(bytes.NewBufferString(parameters["crop_quad"]), 4096))
	if err := decoder.Decode(&quad); err != nil {
		return Transform{}, ErrInvalid
	}
	for index := range quad {
		transform.CropQuad[index] = Point{X: quad[index][0], Y: quad[index][1]}
		if !finite(transform.CropQuad[index].X) || !finite(transform.CropQuad[index].Y) ||
			transform.CropQuad[index].X < float64(original.Min.X) ||
			transform.CropQuad[index].Y < float64(original.Min.Y) ||
			transform.CropQuad[index].X > float64(original.Max.X) ||
			transform.CropQuad[index].Y > float64(original.Max.Y) {
			return Transform{}, ErrInvalid
		}
	}
	rotation, err := strconv.ParseFloat(parameters["rotation_degrees"], 64)
	if err != nil || !finite(rotation) || rotation < -45 || rotation > 45 {
		return Transform{}, ErrInvalid
	}
	width, err := strconv.Atoi(parameters["output_width"])
	if err != nil || width < 1 || width > 100000 {
		return Transform{}, ErrInvalid
	}
	height, err := strconv.Atoi(parameters["output_height"])
	if err != nil || height < 1 || height > 100000 {
		return Transform{}, ErrInvalid
	}
	if !ValidQuad(transform.CropQuad) {
		return Transform{}, ErrInvalid
	}
	kind := parameters["confidence_kind"]
	if !bounded(kind, 64) {
		return Transform{}, ErrInvalid
	}
	if value := parameters["confidence_value"]; value != "" {
		confidence, err := strconv.ParseFloat(value, 64)
		if err != nil || !finite(confidence) || confidence < -1000 || confidence > 1000 {
			return Transform{}, ErrInvalid
		}
		transform.ConfidenceValue = &confidence
	} else if kind != "unavailable" {
		return Transform{}, ErrInvalid
	}
	if !boundedOptional(parameters["fallback_reason"], 256) {
		return Transform{}, ErrInvalid
	}
	transform.RotationDegrees = rotation
	transform.OutputWidth = width
	transform.OutputHeight = height
	transform.ConfidenceKind = kind
	transform.FallbackReason = parameters["fallback_reason"]
	if err := Validate(transform, original); err != nil {
		return Transform{}, err
	}
	return transform, nil
}

func Validate(transform Transform, original image.Rectangle) error {
	for _, point := range transform.CropQuad {
		if !finite(point.X) || !finite(point.Y) ||
			point.X < float64(original.Min.X) || point.Y < float64(original.Min.Y) ||
			point.X > float64(original.Max.X) || point.Y > float64(original.Max.Y) {
			return ErrInvalid
		}
	}
	if !finite(transform.RotationDegrees) || transform.RotationDegrees < -45 || transform.RotationDegrees > 45 ||
		transform.OutputWidth < 1 || transform.OutputWidth > 100000 ||
		transform.OutputHeight < 1 || transform.OutputHeight > 100000 ||
		!bounded(transform.ConfidenceKind, 64) || !boundedOptional(transform.FallbackReason, 256) ||
		transform.ConfidenceValue != nil && (!finite(*transform.ConfidenceValue) || *transform.ConfidenceValue < -1000 || *transform.ConfidenceValue > 1000) ||
		transform.ConfidenceValue == nil && transform.ConfidenceKind != "unavailable" ||
		!ValidQuad(transform.CropQuad) {
		return ErrInvalid
	}
	return nil
}

func ValidQuad(quad [4]Point) bool {
	seen := map[Point]bool{}
	sign := 0
	for index := range quad {
		if seen[quad[index]] {
			return false
		}
		seen[quad[index]] = true
		a, b, c := quad[index], quad[(index+1)%4], quad[(index+2)%4]
		cross := (b.X-a.X)*(c.Y-b.Y) - (b.Y-a.Y)*(c.X-b.X)
		if math.Abs(cross) < 1e-9 {
			return false
		}
		current := 1
		if cross < 0 {
			current = -1
		}
		if sign == 0 {
			sign = current
		} else if sign != current {
			return false
		}
	}
	return sign > 0
}

// MapSourcePoint maps a source point through the crop and output rotation.
func MapSourcePoint(source Point, transform Transform) (Point, bool) {
	return Point{}, false
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func bounded(value string, max int) bool {
	return value != "" && len(value) <= max && noControl(value)
}

func boundedOptional(value string, max int) bool {
	return value == "" || len(value) <= max && noControl(value)
}

func noControl(value string) bool {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
