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
// Crop corners [TL,TR,BR,BL] map to [(0,0),(W,0),(W,H),(0,H)]. Pixel sampling
// is rounded down; positive rotation is counter-clockwise with y increasing down.
func MapSourcePoint(source Point, transform Transform) (Point, bool) {
	if !finite(source.X) || !finite(source.Y) || !finite(transform.RotationDegrees) ||
		transform.OutputWidth < 1 || transform.OutputHeight < 1 || !ValidQuad(transform.CropQuad) {
		return Point{}, false
	}
	p0, p1, p2, p3 := transform.CropQuad[0], transform.CropQuad[1], transform.CropQuad[2], transform.CropQuad[3]
	bx, by := p1.X-p0.X, p1.Y-p0.Y
	cx, cy := p3.X-p0.X, p3.Y-p0.Y
	dx, dy := p2.X-p1.X-p3.X+p0.X, p2.Y-p1.Y-p3.Y+p0.Y
	u, v := 0.5, 0.5
	for range 32 {
		qx := p0.X + u*bx + v*cx + u*v*dx
		qy := p0.Y + u*by + v*cy + u*v*dy
		fx, fy := source.X-qx, source.Y-qy
		j11, j12 := bx+v*dx, cx+u*dx
		j21, j22 := by+v*dy, cy+u*dy
		determinant := j11*j22 - j12*j21
		if math.Abs(determinant) < 1e-12 {
			return Point{}, false
		}
		du := (fx*j22 - fy*j12) / determinant
		dv := (fy*j11 - fx*j21) / determinant
		u += du
		v += dv
		if math.Abs(du)+math.Abs(dv) < 1e-10 {
			break
		}
	}
	const epsilon = 1e-7
	if !finite(u) || !finite(v) || u < -epsilon || u > 1+epsilon || v < -epsilon || v > 1+epsilon {
		return Point{}, false
	}
	u = math.Max(0, math.Min(1, u))
	v = math.Max(0, math.Min(1, v))
	x, y := u*float64(transform.OutputWidth), v*float64(transform.OutputHeight)
	if transform.RotationDegrees != 0 {
		centerX, centerY := float64(transform.OutputWidth)/2, float64(transform.OutputHeight)/2
		radians := transform.RotationDegrees * math.Pi / 180
		cosine, sine := math.Cos(radians), math.Sin(radians)
		relativeX, relativeY := x-centerX, y-centerY
		x = centerX + cosine*relativeX + sine*relativeY
		y = centerY - sine*relativeX + cosine*relativeY
	}
	if x < 0 || y < 0 || x > float64(transform.OutputWidth) || y > float64(transform.OutputHeight) {
		return Point{}, false
	}
	return Point{X: x, Y: y}, true
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
