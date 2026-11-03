package websession

import (
	"crypto/sha256"
	"encoding/base64"
)

type grantKey struct{}
type loginGrant struct {
	source, nonce, principal string
	rotated                  bool
	kind                     string
	credentialVersion        int64
}

func hashedToken(raw string) string {
	digest := sha256.Sum256([]byte(raw))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}
