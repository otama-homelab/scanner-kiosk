//go:build linux

package thumbnail

import (
	"bytes"
	"context"
	"errors"
	"io"

	"scanner-kiosk/internal/artifactscope"
)

// GenerateScoped consumes only a coordinator-authorized input and creates a
// budgeted private JPEG. Caller must Seal, transactionally publish the derivative
// binding and Release; otherwise Close removes outputs and preserves originals.
// It does not schedule work, release device leases or publish DB records.
func GenerateScoped(ctx context.Context, scope *artifactscope.Scope, id string, options Options) (string, error) {
	if scope == nil || options.Input.MaxBytes < 1 || options.Input.MaxBytes > 1<<30 {
		return "", ErrInvalid
	}
	input, err := scope.OpenInput(ctx, id)
	if err != nil {
		return "", err
	}
	raw := &writer{ctx: ctx, limit: options.Input.MaxBytes}
	_, readErr := io.Copy(raw, io.LimitReader(input, options.Input.MaxBytes+1))
	closeErr := input.Close()
	if err = errors.Join(readErr, closeErr); err != nil {
		return "", err
	}
	result, err := Build(ctx, bytes.NewReader(raw.data.Bytes()), options)
	if err != nil {
		return "", err
	}
	output, err := scope.CreateArtifact(ctx, artifactscope.JPEG)
	if err != nil {
		return "", err
	}
	_, writeErr := output.Write(result.Data)
	closeErr = output.Close()
	if err = errors.Join(writeErr, closeErr); err != nil {
		return "", err
	}
	return output.Reference(), nil
}
