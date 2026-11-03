//go:build linux

package sane

import (
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var ErrCapabilities = errors.New("scanner backend capabilities unavailable or unsupported")

// Range describes the backend's millimetre geometry constraint. A zero step
// denotes an unquantized range. These constraints are not pixel dimensions.
type Range struct{ Min, Max, Step float64 }
type Capabilities struct {
	Modes                    []string
	DPI                      []int
	Left, Top, Width, Height Range
}

var numericRange = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)\.\.([0-9]+(?:\.[0-9]+)?)(mm|dpi)(?: \(in steps of ([0-9]+(?:\.[0-9]+)?)\))?$`)

// parseCapabilities handles the pinned scanimage -A option listing in LC_ALL=C.
// It accepts only explicit active constraints, never defaults or descriptions as
// capability evidence. Unknown/truncated/duplicate required options fail closed.
func parseCapabilities(data []byte, device string) (Capabilities, error) {
	var out Capabilities
	if len(data) == 0 || len(data) > 256<<10 {
		return out, ErrCapabilities
	}
	marker := "All options specific to device `" + device + "':"
	lines := strings.Split(string(data), "\n")
	found := false
	required := map[string]string{"--mode": "", "--source": "", "--resolution": "", "-l": "", "-t": "", "-x": "", "-y": ""}
	for _, line := range lines {
		if strings.TrimSpace(line) == marker {
			if found {
				return Capabilities{}, ErrCapabilities
			}
			found = true
			continue
		}
		if !found || !strings.HasPrefix(line, "    ") {
			continue
		}
		field := strings.TrimPrefix(line, "    ")
		space := strings.IndexByte(field, ' ')
		if space < 0 {
			continue
		}
		name := field[:space]
		if _, ok := required[name]; !ok {
			continue
		}
		if required[name] != "" {
			return Capabilities{}, ErrCapabilities
		}
		value := field[space+1:]
		suffix := strings.LastIndex(value, " [")
		if suffix < 1 || !strings.HasSuffix(value, "]") || value[suffix:] == " [inactive]" {
			return Capabilities{}, ErrCapabilities
		}
		required[name] = value[:suffix]
	}
	if !found {
		return out, ErrCapabilities
	}
	for _, value := range required {
		if value == "" {
			return Capabilities{}, ErrCapabilities
		}
	}
	modes := strings.Split(required["--mode"], "|")
	for _, mode := range modes {
		switch mode {
		case "Gray":
			out.Modes = append(out.Modes, "gray")
		case "Color":
			out.Modes = append(out.Modes, "color")
		}
	}
	flatbed := false
	for _, source := range strings.Split(required["--source"], "|") {
		if source == "Flatbed" {
			flatbed = true
		}
	}
	if !flatbed || len(out.Modes) == 0 {
		return Capabilities{}, ErrCapabilities
	}
	resolution := required["--resolution"]
	if strings.Contains(resolution, "..") {
		r, err := parseRange(resolution, "dpi")
		if err != nil {
			return Capabilities{}, err
		}
		for _, value := range []int{75, 100, 150, 200, 300, 600} {
			if r.Contains(float64(value)) {
				out.DPI = append(out.DPI, value)
			}
		}
	} else {
		if !strings.HasSuffix(resolution, "dpi") {
			return Capabilities{}, ErrCapabilities
		}
		seen := map[int]bool{}
		for _, part := range strings.Split(strings.TrimSuffix(resolution, "dpi"), "|") {
			value, err := strconv.Atoi(part)
			if err != nil || value < 1 || value > 100000 || seen[value] {
				return Capabilities{}, ErrCapabilities
			}
			seen[value] = true
		}
		for _, value := range []int{75, 100, 150, 200, 300, 600} {
			if seen[value] {
				out.DPI = append(out.DPI, value)
			}
		}
	}
	if len(out.DPI) == 0 {
		return Capabilities{}, ErrCapabilities
	}
	for name, target := range map[string]*Range{"-l": &out.Left, "-t": &out.Top, "-x": &out.Width, "-y": &out.Height} {
		r, err := parseRange(required[name], "mm")
		if err != nil {
			return Capabilities{}, err
		}
		*target = r
	}
	return out, nil
}
func parseRange(text, unit string) (Range, error) {
	match := numericRange.FindStringSubmatch(text)
	if match == nil || match[3] != unit {
		return Range{}, ErrCapabilities
	}
	min, e1 := strconv.ParseFloat(match[1], 64)
	max, e2 := strconv.ParseFloat(match[2], 64)
	step := 0.0
	var e3 error
	if match[4] != "" {
		step, e3 = strconv.ParseFloat(match[4], 64)
	}
	if e1 != nil || e2 != nil || e3 != nil || min < 0 || max <= min || max > 100000 || step < 0 || step > max-min || math.IsInf(max, 0) {
		return Range{}, ErrCapabilities
	}
	return Range{Min: min, Max: max, Step: step}, nil
}
func (r Range) Contains(value float64) bool {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < r.Min || value > r.Max {
		return false
	}
	if r.Step == 0 {
		return true
	}
	steps := (value - r.Min) / r.Step
	return math.Abs(steps-math.Round(steps)) < 1e-6
}
