package authport

import "context"

// PasswordResult is an internal authentication result, not a browser response.
// CredentialVersion must be rechecked when issuing a session after a password
// reset; a completed password calculation alone is not a session grant.
type PasswordResult struct {
	Principal
	CredentialVersion int64
}

type Password interface {
	Authenticate(context.Context, string, string) (PasswordResult, error)
}
