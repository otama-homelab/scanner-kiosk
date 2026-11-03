//go:build linux

package sane

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func parameterFixture() Parameters {
	return Parameters{ProtocolVersion: 1, Size: "a4", Color: "gray", DPI: 300, Width: 2480, Height: 3507, BytesPerLine: 2480, Depth: 8, Channels: 1, LastFrame: true, SANEVersion: 1<<24 | 2<<16 | 1}
}
func TestParametersRequireExactBoundedReadback(t *testing.T) {
	for _, mode := range []string{"valid", "wrong-selection", "pixel-limit", "zero-height", "stride", "depth", "channels", "last-frame", "protocol", "sane-version", "unknown-key", "duplicate-key", "null", "missing", "wrong-case", "trailing"} {
		t.Run(mode, func(t *testing.T) {
			value := parameterFixture()
			limit := int64(40000000)
			switch mode {
			case "wrong-selection":
				value.DPI = 600
			case "pixel-limit":
				limit = 100
			case "zero-height":
				value.Height = 0
			case "stride":
				value.BytesPerLine = 2479
			case "depth":
				value.Depth = 16
			case "channels":
				value.Channels = 3
			case "last-frame":
				value.LastFrame = false
			case "protocol":
				value.ProtocolVersion = 2
			case "sane-version":
				value.SANEVersion = 2 << 24
			}
			data, _ := json.Marshal(value)
			raw := string(data)
			switch mode {
			case "unknown-key":
				raw = strings.Replace(raw, "{", `{"path":"private",`, 1)
			case "duplicate-key":
				raw = strings.Replace(raw, "{", `{"width":2480,`, 1)
			case "null":
				raw = strings.Replace(raw, `"width":2480`, `"width":null`, 1)
			case "missing":
				raw = strings.Replace(raw, `"depth":8,`, "", 1)
			case "wrong-case":
				raw = strings.Replace(raw, `"width"`, `"Width"`, 1)
			case "trailing":
				raw += "{}"
			}
			parsed, err := decodeParameters([]byte(raw), Selection{Size: "a4", Color: "gray", DPI: 300}, limit)
			if mode == "valid" {
				if err != nil || parsed != value {
					t.Fatal(parsed, err)
				}
			} else if !errors.Is(err, ErrParameters) || parsed != (Parameters{}) {
				t.Fatal("invalid native metadata accepted", mode, parsed, err)
			}
		})
	}
}
func TestParameterProbeUsesSupervisedPrivateInspection(t *testing.T) {
	t.Setenv("SCANNER_OIDC_CLIENT_SECRET", "must-not-inherit")
	raw, _ := json.Marshal(parameterFixture())
	adapter, options := fixture(t, "test -z \"$SCANNER_OIDC_CLIENT_SECRET\" || exit 99; printf '%s' '"+string(raw)+"'")
	options.ParametersBinary = options.Binary
	options.MaxPixels = 40000000
	adapter, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	params, attempt, err := adapter.ProbeParameters(context.Background(), Selection{Size: "a4", Color: "gray", DPI: 300})
	if attempt != nil {
		defer attempt.Close()
	}
	if err != nil || params.Width != 2480 || attempt == nil || attempt.StoppedAtMS() <= 0 {
		t.Fatal("parameter probe failed", err)
	}
	if err = attempt.Close(); err != nil {
		t.Fatal(err)
	}
	if files, err := os.ReadDir(options.WorkRoot); err != nil || len(files) != 0 {
		t.Fatal("parameter scratch retained", err)
	}
}
func TestParameterProbeRequiresConfiguredHelper(t *testing.T) {
	adapter, _ := fixture(t, "exit 99")
	if _, attempt, err := adapter.ProbeParameters(context.Background(), Selection{Size: "a4", Color: "gray", DPI: 300}); err != ErrParameters || attempt != nil {
		t.Fatal("implicit helper fallback", err)
	}
}
