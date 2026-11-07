//go:build linux

// Package scannerdriver resolves registered SANE work and fresh idle evidence.
// It never claims jobs, initializes idle leases or reconciles a previous boot.
package scannerdriver

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/scanner"
	"scanner-kiosk/internal/scanner/sane"
)

var ErrInvalid = errors.New("invalid scanner driver configuration or work")
var ErrUnavailable = errors.New("registered scanner unavailable")
var ErrClosed = errors.New("scanner driver closed")

// Inspection retains owned scratch and reliable local-stop evidence on failure.
// A nil result is unknown; an implementation must explicitly prove no start.
type Inspection interface {
	StoppedAtMS() int64
	Close() error
}
type Backend interface {
	DeviceID() string
	InspectSelection(context.Context, sane.Selection) (sane.Capabilities, Inspection, error)
	InspectParameters(context.Context, sane.Selection) (sane.Parameters, Inspection, error)
	scanner.Capturer
}
type IdleObserver interface {
	ObserveIdle(context.Context, string, int64) (int64, error)
}

// IdentityVerifier compares an advertised physical identity with a job binding.
// It provides no exclusive-writer fence or permission to initialize readiness.
type IdentityVerifier interface {
	VerifyIdentity(context.Context, string, string) error
}
type Device struct {
	ID      string
	Backend Backend
}
type Options struct {
	BootID              string
	Devices             []Device
	Observer            IdleObserver
	MaxBytes, MaxPixels int64
	IdentityVerifier    IdentityVerifier
}

// deviceState owns native serialization and unresolved evidence for one device.
// Published devices retain their gates for the worker lifetime; gates never protect IO on
// a different scanner. Job-use fencing remains shared across the whole driver.
type deviceState struct {
	backend        Backend
	gate           chan struct{}
	pending        []Inspection
	captureUnknown bool
	physical       string
}
type Driver struct {
	boot                string
	registryMu          sync.RWMutex
	devices             map[string]*deviceState
	deviceIDs           []string
	observer            IdleObserver
	identity            IdentityVerifier
	maxBytes, maxPixels int64
	closeGate           chan struct{}
	closed              atomic.Bool
	useMu               sync.Mutex
	used                map[string]int64
}

func validID(value string) bool {
	if len(value) < 16 || len(value) > 96 {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-') {
			return false
		}
	}
	return true
}
func New(options Options) (*Driver, error) {
	if !validID(options.BootID) || options.Observer == nil || len(options.Devices) > 32 || options.MaxBytes < 256<<10 || options.MaxBytes > 1<<40 || options.MaxPixels < 1 || options.MaxPixels > 1<<60 {
		return nil, ErrInvalid
	}
	d := &Driver{boot: options.BootID, devices: map[string]*deviceState{}, observer: options.Observer, identity: options.IdentityVerifier, maxBytes: options.MaxBytes, maxPixels: options.MaxPixels, closeGate: make(chan struct{}, 1), used: map[string]int64{}}
	for _, device := range options.Devices {
		if !validID(device.ID) || device.Backend == nil || device.Backend.DeviceID() != device.ID || d.devices[device.ID] != nil {
			return nil, ErrInvalid
		}
		state := &deviceState{backend: device.Backend, gate: make(chan struct{}, 1)}
		state.gate <- struct{}{}
		d.devices[device.ID] = state
		d.deviceIDs = append(d.deviceIDs, device.ID)
	}
	d.closeGate <- struct{}{}
	return d, nil
}
func (d *deviceState) acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-d.gate:
		return nil
	}
}
func (d *deviceState) release() { d.gate <- struct{}{} }
func (d *Driver) available(state *deviceState) error {
	if len(state.pending) > 0 || state.captureUnknown {
		return scanner.ErrUnresolved
	}
	if d.closed.Load() {
		return ErrClosed
	}
	return nil
}
func (d *deviceState) finishInspection(inspection Inspection, err error) error {
	if inspection == nil {
		d.captureUnknown = true
		return errors.Join(scanner.ErrUnresolved, err)
	}
	if inspection.StoppedAtMS() <= 0 {
		d.pending = append(d.pending, inspection)
		return errors.Join(scanner.ErrUnresolved, err)
	}
	if closeErr := inspection.Close(); closeErr != nil {
		d.pending = append(d.pending, inspection)
		return errors.Join(scanner.ErrUnresolved, err, closeErr)
	}
	return err
}
func (d *Driver) Resolve(ctx context.Context, work documents.ScanWork) (scanner.Capturer, scanner.Plan, error) {
	empty := scanner.Plan{}
	if !validID(work.Job.ID) || !validID(work.Job.DocumentID) || work.Job.Kind != "scan" || work.Job.Revision < 1 || work.Snapshot.Version != 1 || work.Snapshot.WorkerBootID != d.boot || work.Snapshot.LeaseEpoch < 1 || work.Job.DeadlineAtMS <= time.Now().UnixMilli() || work.ReserveBytes < 256<<10 || work.ReserveBytes > 1<<60 {
		return nil, empty, ErrInvalid
	}
	selection := sane.Selection{Size: work.Snapshot.Capture.Size, Color: work.Snapshot.Capture.Color, DPI: work.Snapshot.Capture.DPI}
	if (work.Snapshot.PhysicalID != "" || work.Snapshot.HandoffID != "") && (!validID(work.Snapshot.PhysicalID) || !validID(work.Snapshot.HandoffID)) {
		return nil, empty, ErrInvalid
	}
	if _, _, err := selection.AreaMM(); err != nil {
		return nil, empty, ErrInvalid
	}
	execution, cancel := context.WithDeadline(ctx, time.UnixMilli(work.Job.DeadlineAtMS))
	defer cancel()
	state := d.device(work.Snapshot.DeviceID)
	if state == nil {
		return nil, empty, ErrUnavailable
	}
	if err := state.acquire(execution); err != nil {
		return nil, empty, err
	}
	defer state.release()
	if err := d.available(state); err != nil {
		return nil, empty, err
	}
	backend := state.backend
	if err := d.useJob(work.Job.ID, work.Job.DeadlineAtMS); err != nil {
		return nil, empty, err
	}
	if state.physical != "" && state.physical != work.Snapshot.PhysicalID {
		state.captureUnknown = true
		return nil, empty, errors.Join(scanner.ErrUnresolved, ErrUnavailable)
	}
	state.physical = work.Snapshot.PhysicalID
	if err := d.checkIdentity(execution, state, work.Snapshot.DeviceID); err != nil {
		return nil, empty, err
	}
	profile, err := d.inspectCapture(execution, state, selection)
	if err != nil {
		return nil, empty, err
	}

	plan := scanner.Plan{DeviceID: work.Snapshot.DeviceID, Size: selection.Size, Color: selection.Color, DPI: selection.DPI, Width: profile.Width, Height: profile.Height, ByteBudget: min(work.ReserveBytes, d.maxBytes)}
	return &capturer{driver: d, state: state, backend: backend, plan: plan, deadline: work.Job.DeadlineAtMS}, plan, nil
}
func (d *Driver) ObserveIdle(ctx context.Context, id string, after int64) (int64, error) {
	if d.device(id) == nil || after <= 0 || after > time.Now().UnixMilli() {
		return 0, ErrInvalid
	}
	state := d.device(id)
	if err := state.acquire(ctx); err != nil {
		return 0, err
	}
	defer state.release()
	if err := d.available(state); err != nil {
		return 0, err
	}
	if err := d.checkIdentity(ctx, state, id); err != nil {
		return 0, err
	}
	began := time.Now().UnixMilli()
	observed, err := d.observer.ObserveIdle(ctx, id, after)
	if err != nil {
		return 0, err
	}
	if observed < began || observed < after || observed > time.Now().UnixMilli() {
		return 0, ErrUnavailable
	}
	if err := d.checkIdentity(ctx, state, id); err != nil {
		return 0, err
	}
	return observed, nil
}

// Called only under the device gate, including immediately before capture and
// around idle observation. A lost binding must retain leases/reservations and
// cannot be cleared by a later Idle response from replacement hardware.
func (d *Driver) checkIdentity(ctx context.Context, state *deviceState, id string) error {
	if state.physical == "" {
		return nil
	} // legacy trusted internal admission only
	if d.identity == nil {
		state.captureUnknown = true
		return errors.Join(scanner.ErrUnresolved, ErrUnavailable)
	}
	if err := d.identity.VerifyIdentity(ctx, id, state.physical); err != nil {
		if errors.Is(err, scanner.ErrUnresolved) {
			state.captureUnknown = true
		}
		return err
	}
	return nil
}

// Close never forces unknown processes away or releases durable job resources.
// It may retry cleanup only for retained inspections whose stop is known.
func (d *Driver) Close(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-d.closeGate:
	}
	defer func() { d.closeGate <- struct{}{} }()
	// Refuse new operations on every device before joining any one device.
	d.registryMu.Lock()
	d.closed.Store(true)
	d.registryMu.Unlock()
	var result error
	for _, id := range d.IDs() {
		state := d.device(id)
		if err := state.acquire(ctx); err != nil {
			result = errors.Join(result, err)
			continue
		}
		remaining := []Inspection{}
		for _, inspection := range state.pending {
			if inspection.StoppedAtMS() <= 0 || inspection.Close() != nil {
				remaining = append(remaining, inspection)
			}
		}
		state.pending = remaining
		if len(state.pending) > 0 || state.captureUnknown {
			result = errors.Join(result, scanner.ErrUnresolved)
		}
		state.release()
	}
	return result
}

func (d *Driver) useJob(job string, deadline int64) error {
	d.useMu.Lock()
	defer d.useMu.Unlock()
	now := time.Now().UnixMilli()
	for id, until := range d.used {
		if until <= now {
			delete(d.used, id)
		}
	}
	if _, exists := d.used[job]; exists {
		return scanner.ErrUnresolved
	}
	if len(d.used) >= 4096 {
		return ErrUnavailable
	}
	d.used[job] = deadline
	return nil
}

type capturer struct {
	driver   *Driver
	state    *deviceState
	backend  Backend
	plan     scanner.Plan
	deadline int64
	spent    atomic.Bool
}

func (c *capturer) Capture(ctx context.Context, plan scanner.Plan) (scanner.Attempt, error) {
	refused := func(err error) (scanner.Attempt, error) { return &noStart{at: time.Now().UnixMilli()}, err }
	if !c.spent.CompareAndSwap(false, true) {
		return nil, errors.Join(scanner.ErrUnresolved, ErrInvalid)
	}
	if plan != c.plan {
		return refused(ErrInvalid)
	}
	execution, cancel := context.WithDeadline(ctx, time.UnixMilli(c.deadline))
	defer cancel()
	if err := c.state.acquire(execution); err != nil {
		return refused(err)
	}
	defer c.state.release()
	if err := c.driver.available(c.state); err != nil {
		return refused(err)
	}
	if err := c.driver.checkIdentity(execution, c.state, c.plan.DeviceID); err != nil {
		if errors.Is(err, scanner.ErrUnresolved) {
			return nil, err
		}
		return refused(err)
	}
	attempt, err := c.backend.Capture(execution, plan)
	if attempt == nil || attempt.StoppedAtMS() <= 0 {
		c.state.captureUnknown = true
		return attempt, errors.Join(scanner.ErrUnresolved, err)
	}
	return attempt, err
}

// noStart is evidence only for a refusal before invoking a native operation.
type noStart struct{ at int64 }

func (a *noStart) StoppedAtMS() int64                { return a.at }
func (*noStart) Close() error                        { return nil }
func (*noStart) Release() error                      { return ErrUnavailable }
func (*noStart) Artifact() (scanner.Artifact, error) { return scanner.Artifact{}, ErrUnavailable }

// InspectCapture verifies a complete registered setting without claiming work,
// allocating a capture attempt, observing physical idle or creating readiness.
func (d *Driver) InspectCapture(ctx context.Context, id string, capture scanner.Capture) (scanner.Profile, error) {
	if !validID(id) {
		return scanner.Profile{}, ErrInvalid
	}
	selection := sane.Selection{Size: capture.Size, Color: capture.Color, DPI: capture.DPI}
	if _, _, err := selection.AreaMM(); err != nil {
		return scanner.Profile{}, ErrInvalid
	}
	state := d.device(id)
	if state == nil {
		return scanner.Profile{}, ErrUnavailable
	}
	if err := state.acquire(ctx); err != nil {
		return scanner.Profile{}, err
	}
	defer state.release()
	if err := d.available(state); err != nil {
		return scanner.Profile{}, err
	}
	return d.inspectCapture(ctx, state, selection)
}
func (d *Driver) inspectCapture(ctx context.Context, state *deviceState, selection sane.Selection) (scanner.Profile, error) {
	backend := state.backend
	caps, inspection, err := backend.InspectSelection(ctx, selection)
	if err = state.finishInspection(inspection, err); err != nil {
		return scanner.Profile{}, err
	}
	width, height, _ := selection.AreaMM()
	mode, dpi := false, false
	for _, value := range caps.Modes {
		mode = mode || value == selection.Color
	}
	for _, value := range caps.DPI {
		dpi = dpi || value == selection.DPI
	}
	if !mode || !dpi || !caps.Left.Contains(0) || !caps.Top.Contains(0) || !caps.Width.Contains(width) || !caps.Height.Contains(height) {
		return scanner.Profile{}, ErrUnavailable
	}
	if err = ctx.Err(); err != nil {
		return scanner.Profile{}, err
	}
	params, inspection, err := backend.InspectParameters(ctx, selection)
	if err = state.finishInspection(inspection, err); err != nil {
		return scanner.Profile{}, err
	}
	if err = params.Validate(selection, d.maxPixels); err != nil {
		return scanner.Profile{}, err
	}
	if err = ctx.Err(); err != nil {
		return scanner.Profile{}, err
	}
	return scanner.Profile{Capture: scanner.Capture{Size: selection.Size, Color: selection.Color, DPI: selection.DPI}, Width: params.Width, Height: params.Height, CheckedAtMS: time.Now().UnixMilli()}, nil
}

func (d *Driver) device(id string) *deviceState {
	d.registryMu.RLock()
	defer d.registryMu.RUnlock()
	return d.devices[id]
}
func (d *Driver) IDs() []string {
	d.registryMu.RLock()
	defer d.registryMu.RUnlock()
	return append([]string(nil), d.deviceIDs...)
}
func (d *Driver) register(device Device, physical string) error {
	d.registryMu.Lock()
	defer d.registryMu.Unlock()
	if d.closed.Load() || !validID(device.ID) || device.Backend == nil || device.Backend.DeviceID() != device.ID || d.devices[device.ID] != nil || len(d.devices) >= 32 {
		return ErrInvalid
	}
	state := &deviceState{backend: device.Backend, gate: make(chan struct{}, 1), physical: physical}
	state.gate <- struct{}{}
	d.devices[device.ID] = state
	d.deviceIDs = append(d.deviceIDs, device.ID)
	return nil
}
