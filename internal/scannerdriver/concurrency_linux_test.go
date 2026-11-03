//go:build linux

package scannerdriver

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/scanner"
)

type namedBackend struct {
	*backend
	id string
}

func (b *namedBackend) DeviceID() string { return b.id }

type parallelObserver struct{ calls atomic.Int64 }

func (o *parallelObserver) ObserveIdle(context.Context, string, int64) (int64, error) {
	o.calls.Add(1)
	return time.Now().UnixMilli(), nil
}

func twoDevices(t *testing.T) (*Driver, *namedBackend, *namedBackend, documents.ScanWork) {
	t.Helper()
	_, a, _, work := fixture(t)
	_, b, _, _ := fixture(t)
	first := &namedBackend{a, deviceID}
	second := &namedBackend{b, "scanner_driver_0002"}
	d, err := New(Options{BootID: bootID, Devices: []Device{{first.id, first}, {second.id, second}}, Observer: &parallelObserver{}, MaxBytes: 1 << 20, MaxPixels: 1000000})
	if err != nil {
		t.Fatal(err)
	}
	return d, first, second, work
}

func TestDifferentDevicesCaptureConcurrentlyAndSameDeviceWaits(t *testing.T) {
	d, a, b, work := twoDevices(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	release := make(chan struct{})
	a.started, a.unblock = make(chan struct{}), release
	b.started, b.unblock = make(chan struct{}), release
	ca, pa, err := d.Resolve(ctx, work)
	if err != nil {
		t.Fatal(err)
	}
	other := work
	other.Job.ID = "job_driver_parallel_02"
	other.Snapshot.DeviceID = b.id
	cb, pb, err := d.Resolve(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	go func() { _, err := ca.Capture(ctx, pa); results <- err }()
	go func() { _, err := cb.Capture(ctx, pb); results <- err }()
	// Both native operations must enter before either is allowed to finish.
	for _, started := range []chan struct{}{a.started, b.started} {
		select {
		case <-started:
		case <-ctx.Done():
			close(release)
			t.Fatal("different device blocked behind capture", ctx.Err())
		}
	}
	wait, stop := context.WithTimeout(ctx, 20*time.Millisecond)
	_, err = d.InspectCapture(wait, a.id, scanner.Capture{Size: "a4", Color: "gray", DPI: 300})
	stop()
	if !errors.Is(err, context.DeadlineExceeded) {
		close(release)
		t.Fatal("same device inspection overlapped native capture", err)
	}
	wait, stop = context.WithTimeout(ctx, 20*time.Millisecond)
	_, err = d.ObserveIdle(wait, b.id, time.Now().UnixMilli())
	stop()
	if !errors.Is(err, context.DeadlineExceeded) {
		close(release)
		t.Fatal("same device status overlapped native capture", err)
	}
	close(release)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if a.captures != 1 || b.captures != 1 || a.selections != 1 {
		t.Fatal("capture or cancelled inspection replayed")
	}
	if err := d.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := d.InspectCapture(ctx, b.id, scanner.Capture{Size: "a4", Color: "gray", DPI: 300}); !errors.Is(err, ErrClosed) {
		t.Fatal("operation after close", err)
	}
}

func TestDeviceUncertaintyDoesNotBlockOtherDeviceButCloseRetainsEvidence(t *testing.T) {
	d, a, b, work := twoDevices(t)
	a.selection.stopped = 0
	if _, _, err := d.Resolve(context.Background(), work); !errors.Is(err, scanner.ErrUnresolved) {
		t.Fatal(err)
	}
	other := work
	other.Job.ID = "job_driver_parallel_02"
	other.Snapshot.DeviceID = b.id
	capturer, plan, err := d.Resolve(context.Background(), other)
	if err != nil {
		t.Fatal("healthy device blocked", err)
	}
	if _, err = capturer.Capture(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if err = d.Close(context.Background()); !errors.Is(err, scanner.ErrUnresolved) {
		t.Fatal("lost unknown stop", err)
	}
	if a.selection.closes != 0 || b.captures != 1 {
		t.Fatal("unsafe cleanup or missed independent capture")
	}
	if _, _, err = d.Resolve(context.Background(), other); !errors.Is(err, ErrClosed) {
		t.Fatal("healthy device not closed after earlier uncertainty", err)
	}
}

func TestJobUseFenceIsSharedAcrossDevices(t *testing.T) {
	d, _, b, work := twoDevices(t)
	defer d.Close(context.Background())
	if _, _, err := d.Resolve(context.Background(), work); err != nil {
		t.Fatal(err)
	}
	work.Snapshot.DeviceID = b.id
	if _, _, err := d.Resolve(context.Background(), work); !errors.Is(err, scanner.ErrUnresolved) || b.selections != 0 {
		t.Fatal("job replay moved to another device", err)
	}
}

func TestCloseJoinsEveryDeviceAndRefusesNewOperations(t *testing.T) {
	d, a, b, work := twoDevices(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a.started, a.unblock = make(chan struct{}), make(chan struct{})
	capture, plan, err := d.Resolve(ctx, work)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := capture.Capture(ctx, plan); done <- err }()
	select {
	case <-a.started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	closing := make(chan error, 1)
	go func() { closing <- d.Close(ctx) }()
	for !d.closed.Load() {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if _, err = d.InspectCapture(ctx, b.id, scanner.Capture{Size: "a4", Color: "gray", DPI: 300}); !errors.Is(err, ErrClosed) || b.selections != 0 {
		t.Fatal("new operation entered during close", err)
	}
	select {
	case err := <-closing:
		t.Fatal("close returned before native join", err)
	default:
	}
	close(a.unblock)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-closing; err != nil {
		t.Fatal(err)
	}
}
