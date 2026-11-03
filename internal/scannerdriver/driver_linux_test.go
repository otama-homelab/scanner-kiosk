//go:build linux

package scannerdriver

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/scanner"
	"scanner-kiosk/internal/scanner/sane"
)

const deviceID = "scanner_driver_0001"
const bootID = "worker_driver_00001"

type inspection struct {
	stopped int64
	err     error
	closes  int
}

func (i *inspection) StoppedAtMS() int64 { return i.stopped }
func (i *inspection) Close() error       { i.closes++; return i.err }

type backend struct {
	started, unblock                     chan struct{}
	selection, parameters                *inspection
	selectionErr, parameterErr           error
	params                               sane.Parameters
	caps                                 sane.Capabilities
	selections, parameterCalls, captures int
	captured                             scanner.Plan
	capture                              scanner.Attempt
}

func (*backend) DeviceID() string { return deviceID }
func (b *backend) InspectSelection(context.Context, sane.Selection) (sane.Capabilities, Inspection, error) {
	b.selections++
	if b.selection == nil {
		return b.caps, nil, b.selectionErr
	}
	return b.caps, b.selection, b.selectionErr
}
func (b *backend) InspectParameters(context.Context, sane.Selection) (sane.Parameters, Inspection, error) {
	b.parameterCalls++
	if b.parameters == nil {
		return b.params, nil, b.parameterErr
	}
	return b.params, b.parameters, b.parameterErr
}
func (b *backend) Capture(ctx context.Context, p scanner.Plan) (scanner.Attempt, error) {
	b.captures++
	b.captured = p
	if b.started != nil {
		close(b.started)
		select {
		case <-b.unblock:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return b.capture, nil
}

type observer struct {
	calls int
	err   error
	stale bool
}

func (o *observer) ObserveIdle(context.Context, string, int64) (int64, error) {
	o.calls++
	if o.stale {
		return 1, nil
	}
	return time.Now().UnixMilli(), o.err
}
func fixture(t *testing.T) (*Driver, *backend, *observer, documents.ScanWork) {
	t.Helper()
	native := &backend{selection: &inspection{stopped: 1}, parameters: &inspection{stopped: 1}, params: sane.Parameters{ProtocolVersion: 1, Size: "a4", Color: "gray", DPI: 300, Width: 80, Height: 100, BytesPerLine: 80, Depth: 8, Channels: 1, LastFrame: true, SANEVersion: 1 << 24}, caps: sane.Capabilities{Modes: []string{"gray"}, DPI: []int{300}, Left: sane.Range{Min: 0, Max: 300}, Top: sane.Range{Min: 0, Max: 300}, Width: sane.Range{Min: 0, Max: 300}, Height: sane.Range{Min: 0, Max: 300}}, capture: &noStart{at: time.Now().UnixMilli()}}
	idle := &observer{}
	d, err := New(Options{BootID: bootID, Devices: []Device{{ID: deviceID, Backend: native}}, Observer: idle, MaxBytes: 1 << 20, MaxPixels: 1000000})
	if err != nil {
		t.Fatal(err)
	}
	work := documents.ScanWork{Job: documents.Job{ID: "job_driver_0000001", DocumentID: "document_driver_01", Kind: "scan", Revision: 1, DeadlineAtMS: time.Now().Add(time.Minute).UnixMilli()}, Snapshot: documents.ScanSnapshot{Version: 1, DeviceID: deviceID, WorkerBootID: bootID, LeaseEpoch: 1, Capture: documents.Capture{Size: "a4", Color: "gray", DPI: 300}}, ReserveBytes: 1 << 20}
	return d, native, idle, work
}
func TestRegisteredDriverResolvesExactPlanAndPreventsReexecution(t *testing.T) {
	d, native, _, work := fixture(t)
	defer d.Close(context.Background())
	capturer, plan, err := d.Resolve(context.Background(), work)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Width != 80 || plan.Height != 100 || plan.DeviceID != deviceID || plan.ByteBudget != 1<<20 || native.selection.closes != 1 || native.parameters.closes != 1 {
		t.Fatal("unresolved or retained inspection", plan)
	}
	if _, err = capturer.Capture(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if _, err = capturer.Capture(context.Background(), plan); !errors.Is(err, scanner.ErrUnresolved) || native.captures != 1 {
		t.Fatal("native capture replayed", err)
	}
	if _, _, err = d.Resolve(context.Background(), work); !errors.Is(err, scanner.ErrUnresolved) || native.selections != 1 {
		t.Fatal("resolution replayed claimed job", err)
	}
}
func TestDriverRejectsWorkBeforeNativeInspection(t *testing.T) {
	for _, mode := range []string{"boot", "device", "kind", "deadline", "budget", "capture"} {
		t.Run(mode, func(t *testing.T) {
			d, native, _, work := fixture(t)
			defer d.Close(context.Background())
			switch mode {
			case "boot":
				work.Snapshot.WorkerBootID = "worker_replaced_001"
			case "device":
				work.Snapshot.DeviceID = "scanner_unknown_01"
			case "kind":
				work.Job.Kind = "render"
			case "deadline":
				work.Job.DeadlineAtMS = 1
			case "budget":
				work.ReserveBytes = 10
			case "capture":
				work.Snapshot.Capture.DPI = 1200
			}
			if _, _, err := d.Resolve(context.Background(), work); err == nil || native.selections != 0 {
				t.Fatal("invalid work reached backend", err)
			}
		})
	}
}
func TestUnresolvedInspectionBlocksCaptureAndIdle(t *testing.T) {
	for _, mode := range []string{"nil", "unknown-stop", "cleanup"} {
		t.Run(mode, func(t *testing.T) {
			d, native, idle, work := fixture(t)
			switch mode {
			case "nil":
				native.selection = nil
			case "unknown-stop":
				native.selection.stopped = 0
			case "cleanup":
				native.selection.err = errors.New("cleanup failed")
			}
			if _, _, err := d.Resolve(context.Background(), work); !errors.Is(err, scanner.ErrUnresolved) {
				t.Fatal("unknown work treated as stopped", err)
			}
			if native.parameterCalls != 0 || native.captures != 0 {
				t.Fatal("resolution continued after uncertainty")
			}
			if _, err := d.ObserveIdle(context.Background(), deviceID, 1); !errors.Is(err, scanner.ErrUnresolved) || idle.calls != 0 {
				t.Fatal("idle used with unresolved local work", err)
			}
			if err := d.Close(context.Background()); !errors.Is(err, scanner.ErrUnresolved) {
				t.Fatal("unresolved inspection discarded", err)
			}
			if mode == "unknown-stop" && native.selection.closes != 0 {
				t.Fatal("unknown live inspection was cleaned")
			}
			if mode == "cleanup" {
				native.selection.err = nil
				if err := d.Close(context.Background()); err != nil {
					t.Fatal("confirmed cleanup could not be retried", err)
				}
			}
		})
	}
}
func TestStoppedInspectionErrorIsCleanedWithoutCapture(t *testing.T) {
	d, native, _, work := fixture(t)
	defer d.Close(context.Background())
	native.selectionErr = sane.ErrCapabilities
	if _, _, err := d.Resolve(context.Background(), work); !errors.Is(err, sane.ErrCapabilities) || errors.Is(err, scanner.ErrUnresolved) || native.selection.closes != 1 || native.captures != 0 {
		t.Fatal("ordinary rejection lost stop/cleanup", err)
	}
}
func TestDriverChecksNativeParametersAndLocksCapturePlan(t *testing.T) {
	for _, mode := range []string{"pixels", "selection", "plan"} {
		t.Run(mode, func(t *testing.T) {
			d, native, _, work := fixture(t)
			defer d.Close(context.Background())
			if mode == "pixels" {
				native.params.Width = 1000000
			}
			if mode == "selection" {
				native.params.DPI = 600
			}
			capturer, plan, err := d.Resolve(context.Background(), work)
			if mode != "plan" {
				if err == nil || native.captures != 0 {
					t.Fatal("invalid parameters accepted", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			plan.DPI = 600
			attempt, err := capturer.Capture(context.Background(), plan)
			if err == nil || native.captures != 0 || attempt == nil || attempt.StoppedAtMS() <= 0 {
				t.Fatal("changed plan reached native capture", err)
			}
		})
	}
}
func TestDriverRequiresFreshRegisteredIdleAndHonorsClose(t *testing.T) {
	d, _, idle, work := fixture(t)
	if _, err := d.ObserveIdle(context.Background(), "scanner_unknown_01", 1); err == nil || idle.calls != 0 {
		t.Fatal("unknown device reached observer")
	}
	idle.stale = true
	if _, err := d.ObserveIdle(context.Background(), deviceID, 1); err == nil {
		t.Fatal("cached idle accepted")
	}
	idle.stale = false
	if _, err := d.ObserveIdle(context.Background(), deviceID, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.Resolve(context.Background(), work); !errors.Is(err, ErrClosed) {
		t.Fatal("closed driver resolved", err)
	}
}

func TestUnknownCaptureBlocksFurtherDriverOperations(t *testing.T) {
	d, native, idle, work := fixture(t)
	native.capture = nil
	capturer, plan, err := d.Resolve(context.Background(), work)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = capturer.Capture(context.Background(), plan); !errors.Is(err, scanner.ErrUnresolved) {
		t.Fatal("unknown capture reported success", err)
	}
	if _, err = d.ObserveIdle(context.Background(), deviceID, 1); !errors.Is(err, scanner.ErrUnresolved) || idle.calls != 0 {
		t.Fatal("unknown capture used idle", err)
	}
	if err = d.Close(context.Background()); !errors.Is(err, scanner.ErrUnresolved) {
		t.Fatal("unknown capture discarded", err)
	}
}

func TestDuplicateCaptureHasNoFalseStopProofAndCloseCanWait(t *testing.T) {
	d, native, _, work := fixture(t)
	native.started = make(chan struct{})
	native.unblock = make(chan struct{})
	capturer, plan, err := d.Resolve(context.Background(), work)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := capturer.Capture(context.Background(), plan); done <- err }()
	select {
	case <-native.started:
	case <-time.After(time.Second):
		t.Fatal("capture did not start")
	}
	attempt, err := capturer.Capture(context.Background(), plan)
	if attempt != nil || !errors.Is(err, scanner.ErrUnresolved) {
		t.Fatal("duplicate capture invented stop proof", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err = d.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("close did not honor active native work", err)
	}
	close(native.unblock)
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("capture did not finish")
	}
	if native.captures != 1 {
		t.Fatal("duplicate native acquisition")
	}
	if err = d.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestConcreteNativeRefusalProvesNoStart(t *testing.T) {
	for _, mode := range []string{"invalid", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			config, root := t.TempDir(), t.TempDir()
			if err := os.Chmod(config, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(root, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(config, "dll.conf"), []byte("test\n"), 0600); err != nil {
				t.Fatal(err)
			}
			adapter, err := sane.New(sane.Options{Binary: "/bin/false", ConfigDir: config, WorkRoot: root, DeviceID: deviceID, DeviceName: "test:0", AllowTestBackend: true, MaxBytes: 1 << 20, MaxPixels: 1000000, Timeout: time.Second, StopGrace: time.Millisecond})
			if err != nil {
				t.Fatal(err)
			}
			backend, err := Native(adapter)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			expected := sane.ErrInvalid
			if mode == "cancelled" {
				cancel()
				expected = context.Canceled
			}
			attempt, err := backend.Capture(ctx, scanner.Plan{})
			if !errors.Is(err, expected) || attempt == nil || attempt.StoppedAtMS() <= 0 {
				t.Fatal("definitive preflight refusal lost no-start proof", err)
			}
			if err = attempt.Close(); err != nil {
				t.Fatal(err)
			}
			if files, err := os.ReadDir(root); err != nil || len(files) != 0 {
				t.Fatal("preflight refusal allocated native output", err)
			}
		})
	}
}
