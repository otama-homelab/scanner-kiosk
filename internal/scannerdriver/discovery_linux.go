//go:build linux

package scannerdriver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"scanner-kiosk/internal/config"
	"scanner-kiosk/internal/scanner"
	"scanner-kiosk/internal/scanner/escl"
	"scanner-kiosk/internal/scannerdiscovery"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

func (s *Session) IDs() []string { return s.driver.IDs() }
func (s *Session) Devices() []config.Scanner {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]config.Scanner(nil), s.devices...)
}

// Discover verifies the advertised endpoint before publishing a stable UUID-derived ID.
// Registration is append-only: existing jobs, physical pins and device gates remain valid.
func (s *Session) Discover(ctx context.Context, candidate scannerdiscovery.Candidate) (bool, error) {
	if candidate.Name == "" || !utf8.ValidString(candidate.Name) || utf8.RuneCountInString(candidate.Name) > 256 || strings.ContainsFunc(candidate.Name, unicode.IsControl) {
		return false, ErrConfiguration
	}
	probe := config.Scanner{ID: "scanner_discovery_probe", Name: candidate.Name, Driver: "sane-airscan", Endpoint: candidate.Endpoint}
	if config.ValidateScannerEndpoints([]config.Scanner{probe}) != nil {
		return false, ErrConfiguration
	}
	observer, err := escl.New([]escl.Device{{ID: probe.ID, Endpoint: probe.Endpoint}}, escl.Options{Timeout: 2 * time.Second, MaxResponseBytes: 256 << 10})
	if err != nil {
		return false, ErrConfiguration
	}
	identity, err := observer.ObserveIdentity(ctx, probe.ID)
	if err != nil {
		return false, err
	}
	digest := sha256.Sum256([]byte(identity.PhysicalID))
	probe.ID = "scanner_" + hex.EncodeToString(digest[:])
	// shortcut: endpoint bindings stay fixed for this worker boot; restart after an address change.
	if s.driver.device(probe.ID) != nil {
		return false, nil
	}
	devices := s.Devices()
	for _, device := range devices {
		check, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		current, err := s.ObserveIdentity(check, device.ID)
		cancel()
		if err != nil {
			if errors.Is(err, scanner.ErrUnresolved) || ctx.Err() != nil {
				return false, err
			}
			// An offline scanner must not prevent another device from being discovered.
			continue
		}
		if current.PhysicalID == identity.PhysicalID {
			return false, nil
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.driver.closed.Load() {
		return false, ErrClosed
	}
	if len(s.devices) >= 32 {
		return false, ErrConfiguration
	}
	if err = s.beforeLocked(ctx); err != nil {
		return false, err
	}
	if config.ValidateScannerEndpoints(append(append([]config.Scanner(nil), s.devices...), probe)) != nil {
		return false, ErrConfiguration
	}
	native, idle, err := s.configureDevice(probe)
	if err != nil {
		return false, errors.Join(scanner.ErrUnresolved, err)
	}
	if err = s.identity.Register(idle); err != nil {
		return false, errors.Join(scanner.ErrUnresolved, err)
	}
	if err = s.driver.register(native, identity.PhysicalID); err != nil {
		return false, errors.Join(scanner.ErrUnresolved, err)
	}
	s.devices = append(s.devices, probe)
	return true, nil
}
