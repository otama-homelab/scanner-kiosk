// Package scannermetadata defines bounded, coupled native capability metadata.
// A snapshot is not scanner readiness or authority to acquire/release a lease.
package scannermetadata

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"scanner-kiosk/internal/scanner"
)

const MaxAge = 5 * time.Minute

var ErrUnavailable = errors.New("scanner metadata unavailable")

type Device struct {
	ID       string            `json:"id"`
	Name     string            `json:"name,omitempty"`
	Profiles []scanner.Profile `json:"profiles"`
}
type Snapshot struct {
	PublishedAtMS int64    `json:"published_at_ms"`
	ExpiresAtMS   int64    `json:"expires_at_ms"`
	Devices       []Device `json:"devices"`
}
type Source interface {
	ReadScannerMetadata(context.Context) (Snapshot, error)
}
type Inspector interface {
	InspectCapture(context.Context, string, scanner.Capture) (scanner.Profile, error)
}

func ValidCapture(c scanner.Capture) bool {
	switch c.Size {
	case "a4", "a5", "a6", "letter", "photo10x15":
	default:
		return false
	}
	if c.Color != "color" && c.Color != "gray" {
		return false
	}
	switch c.DPI {
	case 75, 100, 150, 200, 300, 600:
		return true
	}
	return false
}
func ValidID(id string) bool {
	if len(id) < 16 || len(id) > 96 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
func Validate(s Snapshot, now time.Time) error {
	ms := now.UnixMilli()
	if s.PublishedAtMS < 0 || s.PublishedAtMS > ms || s.ExpiresAtMS <= ms || s.ExpiresAtMS <= s.PublishedAtMS || s.ExpiresAtMS-s.PublishedAtMS > MaxAge.Milliseconds() || s.Devices == nil || len(s.Devices) > 32 {
		return ErrUnavailable
	}
	seen := map[string]bool{}
	for _, device := range s.Devices {
		if !utf8.ValidString(device.Name) || utf8.RuneCountInString(device.Name) > 256 || strings.ContainsFunc(device.Name, unicode.IsControl) || !ValidID(device.ID) || seen[device.ID] || len(device.Profiles) < 1 || len(device.Profiles) > 60 {
			return ErrUnavailable
		}
		seen[device.ID] = true
		profiles := map[scanner.Capture]bool{}
		for _, p := range device.Profiles {
			if !ValidCapture(p.Capture) || profiles[p.Capture] || p.Width < 1 || p.Height < 1 || p.Width > 1000000 || p.Height > 1000000 || p.CheckedAtMS < 0 || p.CheckedAtMS > s.PublishedAtMS || ms-p.CheckedAtMS >= MaxAge.Milliseconds() || s.ExpiresAtMS-p.CheckedAtMS > MaxAge.Milliseconds() {
				return ErrUnavailable
			}
			profiles[p.Capture] = true
		}
	}
	return nil
}
