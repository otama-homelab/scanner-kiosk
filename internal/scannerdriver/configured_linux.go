//go:build linux

package scannerdriver

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"scanner-kiosk/internal/config"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/scanner"
	"scanner-kiosk/internal/scanner/escl"
	"scanner-kiosk/internal/scanner/sane"
	"scanner-kiosk/internal/scannerreadiness"
	"scanner-kiosk/internal/workeridentity"
)

var ErrConfiguration = errors.New("scanner driver configuration unavailable")
var ErrConfigCleanup = errors.New("owned scanner configuration requires cleanup")

// Session borrows the guard until successful Close; it never releases that guard.
// Construction creates private config only, never native/status/device IO.
// A nonnil result must be retained/closed even when construction fails.
type Session struct {
	mu                 sync.RWMutex
	driver             *Driver
	config             config.Config
	devices            []config.Scanner
	guard              *workeridentity.Guard
	boot, documentRoot string
	workspace          *configWorkspace
	closeGate          chan struct{}
	identity           *escl.Observer
}

func Configure(ctx context.Context, c config.Config, guard *workeridentity.Guard) (*Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.Role != config.Worker || guard == nil || !guard.MatchesRoot(c.DocumentDir) || (!c.ScannerDiscovery && len(c.Scanners) == 0) || len(c.Scanners) > 32 || strings.Contains(c.DocumentDir, ":") {
		return nil, ErrConfiguration
	}
	// Validate every endpoint/native syntax before creating any files.
	if config.ValidateScannerEndpoints(c.Scanners) != nil {
		return nil, ErrConfiguration
	}
	seen := map[string]bool{}
	for _, device := range c.Scanners {
		if device.Driver != "sane-airscan" || !validID(device.ID) || seen[device.ID] {
			return nil, ErrConfiguration
		}
		if _, err := sane.ConfigFiles(device.ID, device.Endpoint); err != nil {
			return nil, ErrConfiguration
		}
		seen[device.ID] = true
	}
	if c.Limits["SCANNER_JOB_MAX_BYTES"] < 256<<10 || c.Limits["SCANNER_JOB_MAX_BYTES"] > 1<<40 || c.Limits["SCANNER_MAX_PAGE_PIXELS"] < 1 || c.Limits["SCANNER_MAX_PAGE_PIXELS"] > 1<<60 || c.Durations["SCANNER_SCAN_TIMEOUT_SECONDS"] <= 0 || c.Durations["SCANNER_PROCESS_STOP_GRACE_SECONDS"] <= 0 {
		return nil, ErrConfiguration
	}
	session := &Session{config: c, devices: append([]config.Scanner(nil), c.Scanners...), guard: guard, boot: guard.ID(), documentRoot: c.DocumentDir, closeGate: make(chan struct{}, 1)}
	session.closeGate <- struct{}{}
	workspace, err := newConfigWorkspace(c.DocumentDir)
	session.workspace = workspace
	if err != nil {
		return session, ErrConfiguration
	}
	nativeDevices := make([]Device, 0, len(c.Scanners))
	idleDevices := make([]escl.Device, 0, len(c.Scanners))
	for _, device := range c.Scanners {
		if err = ctx.Err(); err != nil {
			return session, err
		}
		native, idle, configureErr := session.configureDevice(device)
		if configureErr != nil {
			return session, configureErr
		}
		nativeDevices = append(nativeDevices, native)
		idleDevices = append(idleDevices, idle)
	}
	observer, err := escl.New(idleDevices, escl.Options{Timeout: min(5*time.Second, c.Durations["SCANNER_SCAN_TIMEOUT_SECONDS"]), MaxResponseBytes: 256 << 10})
	if err != nil {
		return session, ErrConfiguration
	}
	session.identity = observer
	session.driver, err = New(Options{BootID: session.boot, Devices: nativeDevices, Observer: &sessionObserver{session: session, observer: observer}, IdentityVerifier: &sessionIdentityVerifier{session}, MaxBytes: c.Limits["SCANNER_JOB_MAX_BYTES"], MaxPixels: c.Limits["SCANNER_MAX_PAGE_PIXELS"]})
	if err != nil {
		return session, ErrConfiguration
	}
	if err = session.before(ctx); err != nil {
		return session, err
	}
	return session, nil
}
func (s *Session) before(ctx context.Context) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.beforeLocked(ctx)
}
func (s *Session) beforeLocked(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.guard.ID() != s.boot || !s.guard.MatchesRoot(s.documentRoot) {
		return scanner.ErrUnresolved
	}
	if s.workspace == nil || s.workspace.verify() != nil {
		return scanner.ErrUnresolved
	}
	return nil
}
func (s *Session) Resolve(ctx context.Context, work documents.ScanWork) (scanner.Capturer, scanner.Plan, error) {
	if s.driver == nil {
		return nil, scanner.Plan{}, ErrConfiguration
	}
	// The production native composition never accepts an unbound legacy job.
	// Such internal fixtures may use Driver directly, not this Session boundary.
	if !validID(work.Snapshot.PhysicalID) || !validID(work.Snapshot.HandoffID) {
		return nil, scanner.Plan{}, errors.Join(scanner.ErrUnresolved, ErrInvalid)
	}
	return s.driver.Resolve(ctx, work)
}
func (s *Session) ObserveIdle(ctx context.Context, id string, after int64) (int64, error) {
	if s.driver == nil {
		return 0, ErrConfiguration
	}
	return s.driver.ObserveIdle(ctx, id, after)
}

// ObserveIdentity is read-only producer input, never readiness/fence authority.
func (s *Session) ObserveIdentity(ctx context.Context, id string) (escl.IdentityObservation, error) {
	if s.driver == nil {
		return escl.IdentityObservation{}, ErrConfiguration
	}
	state := s.driver.device(id)
	if state == nil {
		return escl.IdentityObservation{}, ErrInvalid
	}
	if err := state.acquire(ctx); err != nil {
		return escl.IdentityObservation{}, err
	}
	defer state.release()
	if err := s.driver.available(state); err != nil {
		return escl.IdentityObservation{}, err
	}
	return s.observeIdentity(ctx, id)
}

// Both callers pin the device gate: the public observation above and the
// Driver's private verifier callback below. Close must join them before cleanup.
func (s *Session) observeIdentity(ctx context.Context, id string) (escl.IdentityObservation, error) {
	if err := s.before(ctx); err != nil {
		return escl.IdentityObservation{}, err
	}
	if s.identity == nil {
		return escl.IdentityObservation{}, ErrConfiguration
	}
	observation, err := s.identity.ObserveIdentity(ctx, id)
	if err != nil {
		return escl.IdentityObservation{}, err
	}
	if err := s.before(ctx); err != nil {
		return escl.IdentityObservation{}, err
	}
	return observation, nil
}

type sessionIdentityVerifier struct{ session *Session }

func (v *sessionIdentityVerifier) VerifyIdentity(ctx context.Context, id, expected string) error {
	observation, err := v.session.observeIdentity(ctx, id)
	if err != nil {
		return err
	}
	if observation.PhysicalID != expected {
		return errors.Join(scanner.ErrUnresolved, ErrUnavailable)
	}
	return nil
}
func (s *Session) Close(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.closeGate:
	}
	defer func() { s.closeGate <- struct{}{} }()
	if s.driver != nil {
		if err := s.driver.Close(ctx); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.workspace != nil {
		return s.workspace.close(ctx)
	}
	return nil
}

type sessionBackend struct {
	session *Session
	backend Backend
}

func (b *sessionBackend) DeviceID() string { return b.backend.DeviceID() }
func (b *sessionBackend) InspectSelection(ctx context.Context, selection sane.Selection) (sane.Capabilities, Inspection, error) {
	if err := b.session.before(ctx); err != nil {
		if ctx.Err() != nil && err == ctx.Err() {
			return sane.Capabilities{}, &noStart{at: time.Now().UnixMilli()}, err
		}
		return sane.Capabilities{}, nil, err
	}
	return b.backend.InspectSelection(ctx, selection)
}
func (b *sessionBackend) InspectParameters(ctx context.Context, selection sane.Selection) (sane.Parameters, Inspection, error) {
	if err := b.session.before(ctx); err != nil {
		if ctx.Err() != nil && err == ctx.Err() {
			return sane.Parameters{}, &noStart{at: time.Now().UnixMilli()}, err
		}
		return sane.Parameters{}, nil, err
	}
	return b.backend.InspectParameters(ctx, selection)
}
func (b *sessionBackend) Capture(ctx context.Context, plan scanner.Plan) (scanner.Attempt, error) {
	if err := b.session.before(ctx); err != nil {
		if ctx.Err() != nil && err == ctx.Err() {
			return &noStart{at: time.Now().UnixMilli()}, err
		}
		return nil, err
	}
	return b.backend.Capture(ctx, plan)
}

type sessionObserver struct {
	session  *Session
	observer IdleObserver
}

func (o *sessionObserver) ObserveIdle(ctx context.Context, id string, after int64) (int64, error) {
	if err := o.session.before(ctx); err != nil {
		return 0, err
	}
	return o.observer.ObserveIdle(ctx, id, after)
}

func (s *Session) InspectCapture(ctx context.Context, id string, capture scanner.Capture) (scanner.Profile, error) {
	if s.driver == nil {
		return scanner.Profile{}, ErrConfiguration
	}
	return s.driver.InspectCapture(ctx, id, capture)
}

// ObserveApplicationReadiness covers this guarded deployment, not LAN writers.
// The device gate proves local operations joined; it cannot clear uncertainty.
func (s *Session) ObserveApplicationReadiness(ctx context.Context, id string, epoch int64, capacity int) (scannerreadiness.Evidence, error) {
	var empty scannerreadiness.Evidence
	if s.driver == nil || epoch < 0 || capacity < 1 || capacity > 32 {
		return empty, ErrConfiguration
	}
	state := s.driver.device(id)
	if state == nil {
		return empty, ErrInvalid
	}
	if err := state.acquire(ctx); err != nil {
		return empty, err
	}
	defer state.release()
	if err := s.driver.available(state); err != nil {
		return empty, err
	}
	if err := s.before(ctx); err != nil {
		return empty, err
	}
	owned := time.Now().UnixMilli()
	identity, err := s.observeIdentity(ctx, id)
	if err != nil {
		return empty, err
	}
	if state.physical != "" && state.physical != identity.PhysicalID {
		state.captureUnknown = true
		return empty, scanner.ErrUnresolved
	}
	state.physical = identity.PhysicalID
	stopped := time.Now().UnixMilli()
	idle, err := s.identity.ObserveIdle(ctx, id, stopped)
	if err != nil {
		return empty, err
	}
	if err = s.driver.checkIdentity(ctx, state, id); err != nil {
		return empty, err
	}
	if err = s.before(ctx); err != nil {
		return empty, err
	}
	return scannerreadiness.Evidence{DeviceID: id, PhysicalID: identity.PhysicalID, HandoffID: s.boot, LeaseEpoch: epoch, ScanConcurrency: capacity, OwnershipConfirmedAtMS: owned, LocalStoppedAtMS: stopped, IdleObservedAtMS: idle}, nil
}

func (session *Session) configureDevice(device config.Scanner) (Device, escl.Device, error) {
	c := session.config
	var err error
	directory, writeErr := sane.WriteConfigAt(session.workspace.root, device.ID, device.Endpoint)
	if directory != "" {
		if err = session.workspace.remember(directory); err != nil {
			return Device{}, escl.Device{}, ErrConfiguration
		}
	}
	if writeErr != nil {
		return Device{}, escl.Device{}, ErrConfiguration
	}
	adapter, err := sane.New(sane.Options{Binary: c.ScannerTools.ScanimagePath, ParametersBinary: c.ScannerTools.ParametersPath, CAFile: c.ScannerTools.CAFile, ConfigDir: directory, WorkRoot: c.DocumentDir, DeviceID: device.ID, DeviceName: "airscan:e0:" + device.ID, LibraryDirs: c.ScannerTools.LibraryDirs, MaxBytes: c.Limits["SCANNER_JOB_MAX_BYTES"], MaxPixels: c.Limits["SCANNER_MAX_PAGE_PIXELS"], Timeout: c.Durations["SCANNER_SCAN_TIMEOUT_SECONDS"], StopGrace: c.Durations["SCANNER_PROCESS_STOP_GRACE_SECONDS"]})
	if err != nil || c.ScannerTools.ParametersPath == "" {
		return Device{}, escl.Device{}, ErrConfiguration
	}
	backend, err := Native(adapter)
	if err != nil {
		return Device{}, escl.Device{}, ErrConfiguration
	}
	return Device{ID: device.ID, Backend: &sessionBackend{session: session, backend: backend}}, escl.Device{ID: device.ID, Endpoint: device.Endpoint}, nil
}
