//go:build linux

package sane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
)

var ErrParameters = errors.New("scanner parameters unavailable or unsupported")

// Parameters is native read-back for one selected backend context. It is not
// scanner readiness, a durable capability lease or evidence of physical idle.
type Parameters struct {
	ProtocolVersion int    `json:"protocol_version"`
	Size            string `json:"size"`
	Color           string `json:"color"`
	DPI             int    `json:"dpi"`
	Width           int    `json:"width"`
	Height          int    `json:"height"`
	BytesPerLine    int    `json:"bytes_per_line"`
	Depth           int    `json:"depth"`
	Channels        int    `json:"channels"`
	LastFrame       bool   `json:"last_frame"`
	SANEVersion     int64  `json:"sane_version"`
}

func (a *Adapter) ProbeParameters(ctx context.Context, selection Selection) (Parameters, *ProbeAttempt, error) {
	if _, err := selectionArguments(selection); err != nil {
		return Parameters{}, nil, err
	}
	if a.options.ParametersBinary == "" {
		return Parameters{}, nil, ErrParameters
	}
	args := []string{"--device-name", a.options.DeviceName, "--size", selection.Size, "--color", selection.Color, "--dpi", strconv.Itoa(selection.DPI)}
	if a.options.AllowTestBackend {
		args = append(args, "--allow-test-backend")
	}
	data, attempt, err := a.inspect(ctx, a.options.ParametersBinary, args)
	if err != nil {
		if errors.Is(err, ErrCapabilities) {
			err = ErrParameters
		}
		return Parameters{}, attempt, err
	}
	params, err := decodeParameters(data, selection, a.options.MaxPixels)
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return Parameters{}, attempt, err
	}
	return params, attempt, nil
}
func decodeParameters(data []byte, selection Selection, maxPixels int64) (Parameters, error) {
	var params Parameters
	if len(data) == 0 || len(data) > 4096 || maxPixels < 1 || maxPixels > 1<<60 {
		return params, ErrParameters
	}
	required := map[string]bool{"protocol_version": false, "size": false, "color": false, "dpi": false, "width": false, "height": false, "bytes_per_line": false, "depth": false, "channels": false, "last_frame": false, "sane_version": false}
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return params, ErrParameters
	}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return params, ErrParameters
		}
		name, ok := key.(string)
		seen, known := required[name]
		if !ok || !known || seen {
			return params, ErrParameters
		}
		required[name] = true
		var value json.RawMessage
		if decoder.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return params, ErrParameters
		}
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return params, ErrParameters
	}
	if _, err = decoder.Token(); err != io.EOF {
		return params, ErrParameters
	}
	for _, seen := range required {
		if !seen {
			return params, ErrParameters
		}
	}
	if json.Unmarshal(data, &params) != nil {
		return Parameters{}, ErrParameters
	}
	if err := params.Validate(selection, maxPixels); err != nil {
		return Parameters{}, err
	}
	return params, nil
}

// Validate checks trusted native metadata before it can shape a capture Plan.
func (params Parameters) Validate(selection Selection, maxPixels int64) error {
	if _, err := selectionArguments(selection); err != nil {
		return err
	}
	if maxPixels < 1 || maxPixels > 1<<60 {
		return ErrParameters
	}
	channels := 1
	if selection.Color == "color" {
		channels = 3
	}
	if params.ProtocolVersion != 1 || params.Size != selection.Size || params.Color != selection.Color || params.DPI != selection.DPI || params.Width < 1 || params.Height < 1 || int64(params.Width) > maxPixels/int64(params.Height) || params.Depth != 8 || params.Channels != channels || !params.LastFrame || params.SANEVersion>>24 != 1 || int64(params.BytesPerLine) < int64(params.Width)*int64(channels) || int64(params.BytesPerLine) > maxPixels*int64(channels) {
		return ErrParameters
	}
	return nil
}
