//go:build linux

package imageexport

import (
	"context"
	"scanner-kiosk/internal/artifactscope"
)

func GenerateScoped(ctx context.Context, scope *artifactscope.Scope, id string, page Page, format string, options Options) (string, error) {
	if scope == nil {
		return "", ErrInvalid
	}
	input, err := scope.OpenInput(ctx, id)
	if err != nil {
		return "", err
	}
	raw, readErr := ReadBounded(ctx, input, options.MaxInputBytes)
	closeErr := input.Close()
	if readErr != nil {
		return "", readErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	output, err := Build(ctx, raw, page, format, options)
	if err != nil {
		return "", err
	}
	writer, err := scope.CreateArtifact(ctx, format)
	if err != nil {
		return "", err
	}
	_, writeErr := writer.Write(output)
	closeErr = writer.Close()
	if writeErr != nil {
		return "", writeErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	return writer.Reference(), nil
}
