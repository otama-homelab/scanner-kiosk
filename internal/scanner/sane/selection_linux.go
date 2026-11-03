//go:build linux

package sane

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Shared by inspection and capture so the native selection cannot diverge.
func selectionArguments(selection Selection) ([]string, error) {
	width, height, ok := paper(selection.Size)
	if !ok || !dpi(selection.DPI) || selection.Color != "gray" && selection.Color != "color" {
		return nil, ErrInvalid
	}
	mode := "Color"
	if selection.Color == "gray" {
		mode = "Gray"
	}
	return []string{"--source=Flatbed", "--mode=" + mode, fmt.Sprintf("--resolution=%d", selection.DPI), "-l", "0", "-t", "0", "-x", width, "-y", height}, nil
}

// Defaults alone do not establish support. After parsing the active constraints,
// these selected values additionally prove that the requested options survived
// backend quantization/capping. This is not an image geometry/pixel calculation.
func validateSelection(data []byte, caps Capabilities, selection Selection) error {
	mode := "Color"
	if selection.Color == "gray" {
		mode = "Gray"
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		for _, name := range []string{"--source", "--mode", "--resolution", "-l", "-t", "-x", "-y"} {
			if !strings.HasPrefix(line, "    "+name+" ") {
				continue
			}
			begin := strings.LastIndex(line, " [")
			if begin < 0 || !strings.HasSuffix(line, "]") || values[name] != "" {
				return ErrCapabilities
			}
			values[name] = line[begin+2 : len(line)-1]
		}
	}
	if values["--source"] != "Flatbed" || values["--mode"] != mode {
		return ErrCapabilities
	}
	widthText, heightText, _ := paper(selection.Size)
	width, _ := strconv.ParseFloat(widthText, 64)
	height, _ := strconv.ParseFloat(heightText, 64)
	if !caps.Left.Contains(0) || !caps.Top.Contains(0) || !caps.Width.Contains(width) || !caps.Height.Contains(height) {
		return ErrCapabilities
	}
	supportsMode, supportsDPI := false, false
	for _, value := range caps.Modes {
		supportsMode = supportsMode || value == selection.Color
	}
	for _, value := range caps.DPI {
		supportsDPI = supportsDPI || value == selection.DPI
	}
	if !supportsMode || !supportsDPI {
		return ErrCapabilities
	}
	for key, wanted := range map[string]float64{"--resolution": float64(selection.DPI), "-l": 0, "-t": 0, "-x": width, "-y": height} {
		actual, err := strconv.ParseFloat(values[key], 64)
		// Compare the selected numeric values printed by the pinned CLI exactly;
		// constraints and stderr checks independently reject adjusted selections.
		if err != nil || math.IsNaN(actual) || math.IsInf(actual, 0) || actual != wanted {
			return ErrCapabilities
		}
	}
	return nil
}

// AreaMM returns the fixed application paper area for setting native options.
// It is never a source of expected pixel dimensions.
func (selection Selection) AreaMM() (float64, float64, error) {
	if _, err := selectionArguments(selection); err != nil {
		return 0, 0, err
	}
	width, height, _ := paper(selection.Size)
	w, _ := strconv.ParseFloat(width, 64)
	h, _ := strconv.ParseFloat(height, 64)
	return w, h, nil
}
